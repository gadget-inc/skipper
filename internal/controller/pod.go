package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/go-json-experiment/json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

var (
	hasTenantSelector         = labels.NewSelector().Add(*unwrap(labels.NewRequirement(key.Tenant.Label, selection.Exists, nil)))
	doesNotHaveTenantSelector = labels.NewSelector().Add(*unwrap(labels.NewRequirement(key.Tenant.Label, selection.DoesNotExist, nil)))
	otelHTTPClient            = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
)

func (ctrl *Controller) convergeNamespace(ctx context.Context, namespace string) error {
	ctx, span := telemetry.TraceRoot(ctx, "controller.converge_namespace", trace.WithAttributes(key.Namespace.Otel(namespace)...))
	defer span.End()

	pods, err := ctrl.listPods(namespace, hasTenantSelector)
	if err != nil {
		return fmt.Errorf("failed to get assigned pods: %w", err)
	}

	// TODO: paginate
	metrics, err := ctrl.kubernetesMetrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{LabelSelector: key.Tenant.Label})
	if err != nil {
		log.Error(ctx, "failed to get pod metrics", key.Error.Slog(err))
	}

	podNameToMetric := make(map[string]metricsv1beta1.PodMetrics, len(metrics.Items))
	for _, metric := range metrics.Items {
		podNameToMetric[metric.Name] = metric
	}

	type fnInstances struct {
		fn        *function.Function
		instances []*function.Instance
	}

	hashInstances := make(map[function.Hash]*fnInstances)
	terminatedStaleInstances := make(map[string]int32)

	for _, pod := range pods {
		instance, err := instanceFromPod(pod)
		if err != nil {
			log.Warn(ctx, "failed to get instance from pod", key.Error.Slog(err), key.Pod.Slog(pod))
			err = ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Error(ctx, "failed to terminate pod", key.Error.Slog(err), key.Pod.Slog(pod))
			}
			continue
		}

		ctx := log.With(ctx, key.Instance.Slog(instance))

		responsibleIP := ctrl.ring.Get(instance)
		if responsibleIP != ctrl.config.PodIP {
			log.Trace(ctx, "skipping scaling for function, not assigned to this controller", key.ResponsibleIP.Slog(responsibleIP))
			continue
		}

		if _, ok := hashInstances[instance.Hash()]; !ok {
			hashInstances[instance.Hash()] = &fnInstances{fn: instance.Function} // ensure the function is in the map so that we loop over all the functions in the next step
		}

		// FIXME: this is true if the instance was assigned 3 minutes ago and it fails its readiness probe
		if instance.ReadyAt.IsZero() && time.Since(instance.AssignedAt) > ctrl.config.FunctionAssignTimeout*2 {
			log.Warn(ctx, "terminating instance stuck in assigned state")
			err = ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Error(ctx, "failed to terminate instance stuck in assigned state", key.Error.Slog(err))
			}
			continue
		}

		if podMetric, exists := podNameToMetric[pod.Name]; exists {
			for _, container := range podMetric.Containers {
				if container.Usage.Cpu() != nil {
					instance.CPUUsageMilli += int(container.Usage.Cpu().MilliValue())
				}
				if container.Usage.Memory() != nil {
					instance.MemoryUsageMiB += int(container.Usage.Memory().Value() / 1024 / 1024) // convert to MiB
				}
			}
		}

		replicaSet, err := ctrl.namespaceListers[namespace].replicaSetLister.ReplicaSets(namespace).Get(instance.ReplicaSet)
		if err != nil {
			log.Error(ctx, "failed to get replica set for instance", key.Error.Slog(err))
			continue
		}

		if replicaSet.Status.Replicas > 0 {
			// instance is running on a replica set that has replicas, so it isn't stale
			hashInstances[instance.Hash()].instances = append(hashInstances[instance.Hash()].instances, instance)
			continue
		}

		// this is a stale instance, find a replica set that has replicas
		replicaSets, err := ctrl.namespaceListers[namespace].replicaSetLister.List(labels.SelectorFromSet(labels.Set{key.Deployment.Label: instance.Deployment}))
		if err != nil {
			log.Error(ctx, "failed to list replica sets", key.Error.Slog(err))
			continue
		}

		var activeReplicaSet *appsv1.ReplicaSet
		for _, replicaSet := range replicaSets {
			if replicaSet.Status.Replicas > 0 {
				activeReplicaSet = replicaSet
				break
			}
		}

		ctx = log.With(ctx, key.K8sReplicaSet.Slog(activeReplicaSet))

		if activeReplicaSet != nil {
			minAvailableReplicas := max(1, int32(float32(activeReplicaSet.Status.Replicas)/ctrl.config.AvailableReplicaDivisor))
			availableReplicas := activeReplicaSet.Status.AvailableReplicas - terminatedStaleInstances[activeReplicaSet.Name]
			if availableReplicas < minAvailableReplicas {
				log.Info(ctx, "replica set does not have enough available replicas to terminate stale instance",
					slog.Int("terminated_stale_instances", int(terminatedStaleInstances[activeReplicaSet.Name])),
					slog.Int("min_available_replicas", int(minAvailableReplicas)),
					slog.Int("available_replicas", int(availableReplicas)),
				)
				continue
			}
			terminatedStaleInstances[activeReplicaSet.Name]++
		}

		ctrl.supervisor(instance.Function).mu.Lock()
		log.Info(ctx, "terminating stale instance")
		err = ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, instance.Name, metav1.DeleteOptions{})
		if err != nil {
			log.Error(ctx, "failed to terminate stale instance", key.Error.Slog(err))
		}
		ctrl.supervisor(instance.Function).mu.Unlock()
	}

	var wg sync.WaitGroup
	for _, fnInstances := range hashInstances {
		wg.Go(func() {
			if _, err := ctrl.supervisor(fnInstances.fn).converge(ctx, fnInstances.instances); err != nil {
				log.Error(ctx, "failed to scale function to desired instances", key.Error.Slog(err))
			}
		})
	}
	wg.Wait()

	return nil
}

func (ctrl *Controller) assignPod(ctx context.Context, fn *function.Function) (instance *function.Instance, err error) {
	ctx, span := telemetry.Trace(ctx, "controller.assign_pod")
	defer span.End()

	assignmentsTotal.WithLabelValues(fn.Deployment).Inc()

GET_UNASSIGNED_POD:
	var unassignedPod *v1.Pod
	unassignedPod, err = ctrl.getUnassignedPod(ctx, fn)
	if err != nil {
		return nil, fmt.Errorf("failed to get unassigned pod: %w", err)
	}

	var port string
	port, err = portFromPod(unassignedPod)
	if err != nil {
		return nil, err
	}

	var fnJSON []byte
	fnJSON, err = json.Marshal(fn)
	if err == nil {
		fnJSON, err = json.Marshal(string(fnJSON)) // escape the json so it can be used in the json patch
	}
	if err != nil {
		return nil, fmt.Errorf("failed to marshal function: %w", err)
	}

	// ensure the pod is part of a replica set and isn't already assigned to a function (operations 1-4)
	// then copy the replica set name and label/annotate the pod with the function (operations 5-8)
	patches := []byte(`[
		{ "op": "test", "path": "/metadata/ownerReferences/0/kind", "value": "ReplicaSet" },
		{ "op": "test", "path": "` + key.Tenant.PatchLabel + `", "value": null },
		{ "op": "test", "path": "` + key.Function.PatchAnnotation + `", "value": null },
		{ "op": "test", "path": "` + key.AssignedAt.PatchAnnotation + `", "value": null },
		{ "op": "copy", "path": "` + key.ReplicaSet.PatchAnnotation + `", "from": "/metadata/ownerReferences/0/name" },
		{ "op": "add", "path": "` + key.Tenant.PatchLabel + `", "value": "` + fn.Tenant + `" },
		{ "op": "add", "path": "` + key.Function.PatchAnnotation + `", "value": ` + string(fnJSON) + ` },
		{ "op": "add", "path": "` + key.AssignedAt.PatchAnnotation + `", "value": "` + time.Now().UTC().Format(time.RFC3339) + `" }
	]`)

	var assignedPod *v1.Pod
	assignedPod, err = ctrl.kubernetes.CoreV1().Pods(unassignedPod.Namespace).Patch(ctx, unassignedPod.Name, types.JSONPatchType, patches, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		if apierrors.IsInvalid(err) || errors.Is(err, jsonpatch.ErrTestFailed) {
			log.Warn(ctx, "failed to patch pod, retrying", key.Error.Slog(err), key.Pod.Slog(unassignedPod))
			// there are many reasons this can fail, but one hard to debug one is that the pod doesn't have any annotations
			// see: https://stackoverflow.com/a/57480206, https://datatracker.ietf.org/doc/html/rfc6902#appendix-A.12
			goto GET_UNASSIGNED_POD
		}
		return nil, fmt.Errorf("failed to patch pod: %w", err)
	}

	// delete the unassigned pod if the assign request fails
	defer func() {
		if err != nil {
			if deleteErr := ctrl.kubernetes.CoreV1().Pods(unassignedPod.Namespace).Delete(ctx, unassignedPod.Name, metav1.DeleteOptions{}); deleteErr != nil {
				log.Error(ctx, "failed to delete pod after failed assign request", key.Error.Slog(deleteErr), key.Pod.Slog(unassignedPod))
			}
		}
	}()

	ctrl.updatePodCache(ctx, assignedPod)

	assignURL := "http://" + net.JoinHostPort(assignedPod.Status.PodIP, port) + ctrl.config.FunctionAssignPath
	assignCtx, cancel := context.WithTimeout(ctx, ctrl.config.FunctionAssignTimeout)
	defer cancel()

	now := time.Now()
	token := paseto.NewToken()
	token.SetSubject(fn.Tenant)
	token.SetIssuedAt(now)
	token.SetNotBefore(now)
	token.SetExpiration(now.Add(7 * 24 * time.Hour))

	var req *http.Request
	req, err = http.NewRequestWithContext(assignCtx, http.MethodPost, assignURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(key.Token.Header, token.V2Sign(ctrl.config.PasetoPrivateKey.V2AsymmetricSecretKey))
	fn.SetHeader(req) // TODO: put the function in the token instead

	log.Info(ctx, "assigning pod", key.Pod.Slog(assignedPod))
	var res *http.Response
	res, err = otelHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send assign request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("assign request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
		return nil, err
	}

	// annotate the pod as ready
	patches = []byte(`[{ "op": "add", "path": "` + key.ReadyAt.PatchAnnotation + `", "value": "` + now.UTC().Format(time.RFC3339) + `" }]`)
	assignedPod, err = ctrl.kubernetes.CoreV1().Pods(assignedPod.Namespace).Patch(ctx, assignedPod.Name, types.JSONPatchType, patches, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		return nil, fmt.Errorf("failed to patch pod as ready: %w", err)
	}

	ctrl.updatePodCache(ctx, assignedPod)

	instance, err = instanceFromPod(assignedPod)
	return
}

func (ctrl *Controller) getUnassignedPod(ctx context.Context, fn *function.Function) (*v1.Pod, error) {
	ctx, span := telemetry.Trace(ctx, "controller.get_unassigned_pod")
	defer span.End()

	waitingForUnassignedPods.WithLabelValues(fn.Deployment).Inc()
	defer waitingForUnassignedPods.WithLabelValues(fn.Deployment).Dec()

	return timer.Poll(ctx, 250*time.Millisecond, func(ctx context.Context) (*v1.Pod, error) {
		unassignedPods, err := ctrl.getUnassignedPods(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to list unassigned pods: %w", err)
		}
		if len(unassignedPods) == 0 {
			log.Trace(ctx, "no unassigned pods")
			return nil, nil
		}
		return unassignedPods[rand.Intn(len(unassignedPods))], nil
	})
}

func (ctrl *Controller) getUnassignedPods(fn *function.Function) ([]*v1.Pod, error) {
	equalDeploymentName, err := labels.NewRequirement(key.Deployment.Label, selection.Equals, []string{fn.Deployment})
	if err != nil {
		return nil, err
	}

	pods, err := ctrl.listPods(fn.Namespace, doesNotHaveTenantSelector.Add(*equalDeploymentName))
	if err != nil {
		return nil, err
	}

	// filter out pods that are unready
	return slices.DeleteFunc(pods, func(pod *v1.Pod) bool { return !isPodReady(pod) }), nil
}
