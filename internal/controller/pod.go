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
	"strconv"
	"strings"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/go-json-experiment/json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/protobuf/types/known/timestamppb"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
)

var (
	hasTenantSelector         = labels.NewSelector().Add(*unwrap(labels.NewRequirement(key.Tenant.Label, selection.Exists, nil)))
	doesNotHaveTenantSelector = labels.NewSelector().Add(*unwrap(labels.NewRequirement(key.Tenant.Label, selection.DoesNotExist, nil)))
	otelHTTPClient            = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
)

func (ctrl *Controller) assignPod(ctx context.Context, fn *skipper.Assignment) (instance *skipper.Instance, err error) {
	ctx, span := telemetry.Trace(ctx, "controller.assign_pod")
	defer span.End()

	deployment := fn.GetDeployment()
	assignmentsTotal.WithLabelValues(deployment, deployment).Inc()

GET_UNASSIGNED_POD:
	var unassignedPod *v1.Pod
	unassignedPod, err = ctrl.getUnassignedPod(ctx, fn)
	if err != nil {
		return nil, fmt.Errorf("failed to get unassigned pod: %w", err)
	}

	var port string
	port, err = ctrl.portFromPod(unassignedPod)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal assignment: %w", err)
	}
	bodyJSON := strconv.Quote(string(body))

	// ensure the pod is part of a replica set and isn't already assigned (operations 1-5)
	// then copy the replica set name and label/annotate the pod (operations 6-10).
	// Both legacy "skipper/function" and new "skipper/assignment" annotations are
	// dual-written so a controller running pre-Phase-1 code continues to recognize
	// the pod via its expected annotation.
	patches := []byte(`[
		{ "op": "test", "path": "/metadata/ownerReferences/0/kind", "value": "ReplicaSet" },
		{ "op": "test", "path": "` + key.Tenant.PatchLabel + `", "value": null },
		{ "op": "test", "path": "` + skipper.LegacyFunctionKey.PatchAnnotation + `", "value": null },
		{ "op": "test", "path": "` + skipper.AssignmentKey.PatchAnnotation + `", "value": null },
		{ "op": "test", "path": "` + key.AssignedAt.PatchAnnotation + `", "value": null },
		{ "op": "copy", "path": "` + key.ReplicaSet.PatchAnnotation + `", "from": "/metadata/ownerReferences/0/name" },
		{ "op": "add", "path": "` + key.Tenant.PatchLabel + `", "value": "` + fn.GetTenant() + `" },
		{ "op": "add", "path": "` + skipper.LegacyFunctionKey.PatchAnnotation + `", "value": ` + bodyJSON + ` },
		{ "op": "add", "path": "` + skipper.AssignmentKey.PatchAnnotation + `", "value": ` + bodyJSON + ` },
		{ "op": "add", "path": "` + key.AssignedAt.PatchAnnotation + `", "value": "` + time.Now().UTC().Format(time.RFC3339) + `" }
	]`)

	var assignedPod *v1.Pod
	assignedPod, err = ctrl.patchPod(ctx, unassignedPod.Namespace, unassignedPod.Name, types.JSONPatchType, patches, metav1.PatchOptions{FieldManager: key.Controller.Label})
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
			if deleteErr := ctrl.deletePod(ctx, unassignedPod.Namespace, unassignedPod.Name, metav1.DeleteOptions{}); deleteErr != nil {
				log.Error(ctx, "failed to delete pod after failed assign request", key.Error.Slog(deleteErr), key.Pod.Slog(unassignedPod))
			}
		}
	}()

	assignURL := "http://" + net.JoinHostPort(assignedPod.Status.PodIP, port) + ctrl.config.FunctionAssignPath
	assignCtx, cancel := context.WithTimeout(ctx, ctrl.config.FunctionAssignTimeout)
	defer cancel()

	now := time.Now()
	token := paseto.NewToken()
	token.SetSubject(fn.GetTenant())
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

	// Populate ready-at on a deep copy of the assigned pod so instanceFromPod
	// sees it. The apiserver patch runs asynchronously below — readers that
	// matter for routing (scaleWithoutLock's direct append, oneshot's
	// single-use path) consume this in-memory value; other readers (informer
	// cache, stuck cleanup, HPA CPU) tolerate a brief delay. We deep-copy
	// because patchPod already inserted assignedPod into the informer
	// indexer, and informer background goroutines iterate its annotations.
	assignedPod = assignedPod.DeepCopy()
	if assignedPod.Annotations == nil {
		assignedPod.Annotations = map[string]string{}
	}
	readyAtStr := now.UTC().Format(time.RFC3339)
	assignedPod.Annotations[key.ReadyAt.Label] = readyAtStr

	go func() {
		asyncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ctrl.config.FunctionAssignTimeout)
		defer cancel()

		patches := []byte(`[{ "op": "add", "path": "` + key.ReadyAt.PatchAnnotation + `", "value": "` + readyAtStr + `" }]`)
		if err := ctrl.patchPodWithRetry(asyncCtx, assignedPod.Namespace, assignedPod.Name, patches); err != nil {
			log.Error(asyncCtx, "failed to persist ready-at annotation", key.Error.Slog(err), key.Pod.Slog(assignedPod))
		}
	}()

	instance, err = ctrl.instanceFromPod(assignedPod)
	return
}

func (ctrl *Controller) getUnassignedPod(ctx context.Context, fn *skipper.Assignment) (*v1.Pod, error) {
	ctx, span := telemetry.Trace(ctx, "controller.get_unassigned_pod")
	defer span.End()

	deployment := fn.GetDeployment()
	waitingForUnassignedPods.WithLabelValues(deployment, deployment).Inc()
	defer waitingForUnassignedPods.WithLabelValues(deployment, deployment).Dec()

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

func (ctrl *Controller) getUnassignedPods(fn *skipper.Assignment) ([]*v1.Pod, error) {
	equalDeploymentName, err := labels.NewRequirement(key.Deployment.Label, selection.Equals, []string{fn.GetDeployment()})
	if err != nil {
		return nil, err
	}

	pods, err := ctrl.listPods(fn.GetNamespace(), doesNotHaveTenantSelector.Add(*equalDeploymentName))
	if err != nil {
		return nil, err
	}

	// filter out pods that are unready
	return slices.DeleteFunc(pods, func(pod *v1.Pod) bool { return !isPodReady(pod) }), nil
}

func (ctrl *Controller) getReadyInstances(ctx context.Context, fn *skipper.Assignment) ([]*skipper.Instance, error) {
	instances, err := ctrl.getInstances(ctx, fn)
	if err != nil {
		return nil, err
	}

	// filter out instances that are unready
	return slices.DeleteFunc(instances, func(instance *skipper.Instance) bool { return !instance.HasReadyAt() }), nil
}

func (ctrl *Controller) getInstances(ctx context.Context, fn *skipper.Assignment) ([]*skipper.Instance, error) {
	return ctrl.collectInstances(ctx, fn, true)
}

// listInstances is a read-only variant of getInstances that does NOT
// delete invalid pods. Use from observability paths (web UI, status
// RPCs) where the call must not have side effects.
func (ctrl *Controller) listInstances(ctx context.Context, fn *skipper.Assignment) ([]*skipper.Instance, error) {
	return ctrl.collectInstances(ctx, fn, false)
}

func (ctrl *Controller) collectInstances(ctx context.Context, fn *skipper.Assignment, cleanup bool) ([]*skipper.Instance, error) {
	namespaceLister, found := ctrl.namespaceListers[fn.GetNamespace()]
	if !found {
		return nil, fmt.Errorf("managed pod lister not started for namespace %s", fn.GetNamespace())
	}

	hashKey := strconv.FormatUint(fn.Hash(), 10)
	objs, err := namespaceLister.podIndexer.ByIndex(assignmentHashIndex, hashKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get pods by function hash: %w", err)
	}

	instances := make([]*skipper.Instance, 0, len(objs))
	for _, obj := range objs {
		pod := obj.(*v1.Pod)
		if !isPodRunning(pod) {
			continue
		}

		instance, err := ctrl.instanceFromPod(pod)
		if err != nil {
			if !cleanup {
				continue
			}
			// Pod failed validation (e.g., malformed timestamp annotation, missing replica set annotation).
			// Delete it and continue processing other pods instead of blocking all scaling for this skipper.
			log.Warn(ctx, "failed to get instance from pod, deleting invalid pod", key.Error.Slog(err), key.Pod.Slog(pod))
			err = ctrl.deletePod(ctx, pod.Namespace, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Error(ctx, "failed to delete invalid pod", key.Error.Slog(err), key.Pod.Slog(pod))
			}
			continue
		}

		instances = append(instances, instance)
	}

	return instances, nil
}

func (ctrl *Controller) listPods(namespace string, selector labels.Selector) ([]*v1.Pod, error) {
	podListerEntry, found := ctrl.namespaceListers[namespace]
	if !found {
		return nil, fmt.Errorf("managed pod lister not started for namespace %s", namespace)
	}

	listedPods, err := podListerEntry.podLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// filter out pods that are not running
	return slices.DeleteFunc(listedPods, func(pod *v1.Pod) bool { return !isPodRunning(pod) }), nil
}

func (ctrl *Controller) updatePodCache(ctx context.Context, pod *v1.Pod) {
	namespaceLister, found := ctrl.namespaceListers[pod.Namespace]
	if !found {
		log.Warn(ctx, "managed pod lister not started for namespace", key.Namespace.Slog(pod.Namespace))
		return
	}

	err := namespaceLister.podIndexer.Update(pod)
	if err != nil {
		log.Warn(ctx, "failed to update pod cache", key.Error.Slog(err), key.Pod.Slog(pod))
	}
}

// patchPod patches a pod via the Kubernetes API and updates the informer cache.
func (ctrl *Controller) patchPod(ctx context.Context, namespace, name string, patchType types.PatchType, data []byte, opts metav1.PatchOptions) (*v1.Pod, error) {
	pod, err := ctrl.kubernetes.CoreV1().Pods(namespace).Patch(ctx, name, patchType, data, opts)
	if err != nil {
		return nil, err
	}

	ctrl.updatePodCache(ctx, pod)
	return pod, nil
}

// patchPodWithRetry applies a JSON patch with exponential backoff. It stops
// early if the pod is gone or ctx is cancelled. Callers should use this for
// best-effort background patches where a transient apiserver error shouldn't
// drop the update.
func (ctrl *Controller) patchPodWithRetry(ctx context.Context, namespace, name string, patches []byte) error {
	const maxAttempts = 5
	backoff := 50 * time.Millisecond
	opts := metav1.PatchOptions{FieldManager: key.Controller.Label}

	var lastErr error
	for attempt := range maxAttempts {
		_, lastErr = ctrl.patchPod(ctx, namespace, name, types.JSONPatchType, patches, opts)
		if lastErr == nil {
			return nil
		}
		if apierrors.IsNotFound(lastErr) {
			// pod is gone (e.g., oneshot release) — nothing to persist to
			return nil
		}
		if attempt == maxAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return lastErr
}

// deletePod deletes a pod via the Kubernetes API and removes it from the informer cache.
func (ctrl *Controller) deletePod(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	err := ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, name, opts)
	if err != nil {
		return err
	}

	namespaceLister, found := ctrl.namespaceListers[namespace]
	if !found {
		log.Warn(ctx, "managed pod lister not started for namespace", key.Namespace.Slog(namespace))
		return nil
	}

	// Get the pod from the cache to delete it
	podObj, exists, err := namespaceLister.podIndexer.GetByKey(namespace + "/" + name)
	if err != nil {
		log.Warn(ctx, "failed to get pod from cache for deletion", key.Error.Slog(err), key.Namespace.Slog(namespace), slog.String("pod", name))
		return nil
	}

	if exists {
		if pod, ok := podObj.(*v1.Pod); ok {
			ctrl.portCache.Delete(pod.UID)
		}

		err = namespaceLister.podIndexer.Delete(podObj)
		if err != nil {
			if pod, ok := podObj.(*v1.Pod); ok {
				log.Warn(ctx, "failed to delete pod from cache", key.Error.Slog(err), key.Pod.Slog(pod))
			} else {
				log.Warn(ctx, "failed to delete pod from cache", key.Error.Slog(err), key.Namespace.Slog(namespace), slog.String("pod", name))
			}
		}
	}

	return nil
}

func (ctrl *Controller) refreshMetrics(ctx context.Context) {
	for _, namespace := range ctrl.config.FunctionNamespaces {
		var continueToken string
		for {
			metrics, err := ctrl.kubernetesMetrics.
				MetricsV1beta1().
				PodMetricses(namespace).
				List(ctx, metav1.ListOptions{
					LabelSelector: key.Tenant.Label,
					Limit:         100,
					Continue:      continueToken,
				})
			if err != nil {
				log.Error(ctx, "failed to get pod metrics", key.Error.Slog(err), key.Namespace.Slog(namespace))
				break
			}

			for _, metric := range metrics.Items {
				ctrl.podMetrics.Store(namespace+"/"+metric.Name, metric)
			}

			if metrics.Continue == "" {
				break
			}
			continueToken = metrics.Continue
		}
	}

	// garbage collect metrics for pods that no longer exist
	for key := range ctrl.podMetrics.AllRelaxed() {
		namespace, podName, _ := strings.Cut(key, "/")
		if lister, ok := ctrl.namespaceListers[namespace]; ok {
			if _, err := lister.podLister.Pods(namespace).Get(podName); err != nil {
				ctrl.podMetrics.Delete(key)
			}
		}
	}
}

func (ctrl *Controller) assignmentFromPod(pod *v1.Pod) (*skipper.Assignment, error) {
	if pod == nil {
		return nil, errors.New("pod is nil")
	}

	// Prefer the canonical "skipper/assignment" annotation; fall back to the
	// legacy "skipper/function" annotation written by older controller binaries.
	body, ok := pod.Annotations[skipper.AssignmentKey.Label]
	if !ok {
		body, ok = pod.Annotations[skipper.LegacyFunctionKey.Label]
	}
	if !ok {
		return nil, errors.New("missing assignment annotation")
	}

	if a, ok := ctrl.assignmentCache.Load(body); ok {
		return a, nil
	}

	a := &skipper.Assignment{}
	if err := json.Unmarshal([]byte(body), a); err != nil {
		return nil, fmt.Errorf("failed to unmarshal assignment from pod annotation: %w", err)
	}
	if err := a.Validate(); err != nil {
		return nil, fmt.Errorf("invalid assignment in pod annotation: %w", err)
	}

	ctrl.assignmentCache.Store(body, a)
	return a, nil
}

func (ctrl *Controller) instanceFromPod(pod *v1.Pod) (*skipper.Instance, error) {
	fn, err := ctrl.assignmentFromPod(pod)
	if err != nil {
		return nil, err
	}

	replicaSet := pod.Annotations[key.ReplicaSet.Label]
	if replicaSet == "" {
		return nil, errors.New("missing replica set annotation")
	}

	assignedAt, err := time.Parse(time.RFC3339, pod.Annotations[key.AssignedAt.Label])
	if err != nil {
		return nil, fmt.Errorf("failed to parse assigned at annotation: %w", err)
	}

	port, err := ctrl.portFromPod(pod)
	if err != nil {
		return nil, err
	}

	var cpuUsageMilli, memoryUsageMib uint32
	if podMetric, ok := ctrl.podMetrics.Load(pod.Namespace + "/" + pod.Name); ok {
		for _, container := range podMetric.Containers {
			if container.Usage.Cpu() != nil {
				cpuUsageMilli += uint32(container.Usage.Cpu().MilliValue())
			}
			if container.Usage.Memory() != nil {
				memoryUsageMib += uint32(container.Usage.Memory().Value() / 1024 / 1024) // convert to MiB
			}
		}
	}

	instance := &skipper.Instance{}
	instance.SetAssignment(fn)
	instance.SetName(pod.Name)
	instance.SetReplicaSet(replicaSet)
	instance.SetAssignedAt(timestamppb.New(assignedAt))
	instance.SetAddr(net.JoinHostPort(pod.Status.PodIP, port))
	instance.SetCpuUsageMilli(cpuUsageMilli)
	instance.SetMemoryUsageMib(memoryUsageMib)

	if readyAtStr, ok := pod.Annotations[key.ReadyAt.Label]; ok && isPodReady(pod) {
		readyAt, err := time.Parse(time.RFC3339, readyAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ready at annotation: %w", err)
		}
		instance.SetReadyAt(timestamppb.New(readyAt))
	}

	return instance, nil
}

func (ctrl *Controller) portFromPod(pod *v1.Pod) (string, error) {
	if pod.UID != "" {
		if cached, ok := ctrl.portCache.Load(pod.UID); ok {
			return cached, nil
		}
	}

	port := pod.Annotations[key.Port.Label]
	if port == "" {
		// no port annotation, grab the first port from the first container
		if len(pod.Spec.Containers) > 0 && len(pod.Spec.Containers[0].Ports) > 0 {
			port = strconv.Itoa(int(pod.Spec.Containers[0].Ports[0].ContainerPort))
		}
	} else {
		// assume the port annotation is a named port and try to find
		// the actual port. if we don't find a matching named port,
		// we'll use the port annotation as the actual port
		for _, container := range pod.Spec.Containers {
			for _, containerPort := range container.Ports {
				if containerPort.Name == port {
					port = strconv.Itoa(int(containerPort.ContainerPort))
					break
				}
			}
		}
	}
	if port == "" {
		return "", fmt.Errorf("failed to get port for pod %s", pod.Name)
	}

	if pod.UID != "" {
		ctrl.portCache.Store(pod.UID, port)
	}
	return port, nil
}
