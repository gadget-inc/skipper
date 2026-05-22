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
	"github.com/cespare/xxhash/v2"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/go-json-experiment/json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/protobuf/types/known/timestamppb"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

// podRetryPickBackoff bounds the CPU cost of looping back through
// getUnassignedPod when tryAssignPod returns retryPick=true. The next call
// returns instantly when candidates remain, so without a wait the loop spins
// for the duration of the in-flight SSA (intra-controller reservation case)
// or until the lister catches up to the winning peer (cross-controller race).
const podRetryPickBackoff = 10 * time.Millisecond

// podHashKey adapts a pod name to hashring.Hasher so the existing ring (used
// for function-to-controller routing) can also partition unassigned pods to
// controllers. Reusing xxhash.Sum64String matches Function.Hash so the two
// partitions live in the same 64-bit space.
type podHashKey string

func (k podHashKey) Hash() uint64 {
	return xxhash.Sum64String(string(k))
}

// podOwnedByMe returns true when this controller is responsible for the given
// pod under the current ring topology. On an empty ring -- which happens both
// at boot before the controller-pod informer first fires AND in the
// cohort-transition window where removeFromRing has just emptied the ring
// before any peer is re-added -- the controller owns everything so assignment
// stays reachable. With at least one ring member, ownership is the standard
// consistent-hash lookup against the controller's PodIP.
func (ctrl *Controller) podOwnedByMe(podName string) bool {
	if len(ctrl.ring.List()) == 0 {
		return true
	}
	return ctrl.ring.Get(podHashKey(podName)) == ctrl.config.PodIP
}

var (
	hasTenantSelector         = labels.NewSelector().Add(*unwrap(labels.NewRequirement(key.Tenant.Label, selection.Exists, nil)))
	doesNotHaveTenantSelector = labels.NewSelector().Add(*unwrap(labels.NewRequirement(key.Tenant.Label, selection.DoesNotExist, nil)))
	otelHTTPClient            = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
)

// assignPod claims an unassigned pod from this controller's owned partition
// (per podOwnedByMe) and binds it to fn. Concurrent callers inside the same
// controller serialize through podReservations on the pod key; the SSA apply
// runs without ApplyOptions.Force, so a cross-controller race during a
// ring-rebalance window surfaces as a field-manager Conflict that
// tryAssignPod converts to retryPick=true (picking another pod instead of
// stealing ownership, which would break the partition guarantee). When a
// reservation is already held or the SSA reports a Conflict, the caller
// loops back to getUnassignedPod for a different candidate after a short
// jittered backoff; the outer for-loop discards the reservation between
// iterations.
func (ctrl *Controller) assignPod(ctx context.Context, fn *skipper.Function) (*skipper.Instance, error) {
	ctx, span := telemetry.Trace(ctx, "controller.assign_pod")
	defer span.End()

	assignmentsTotal.WithLabelValues(fn.GetDeployment()).Inc()

	for {
		unassignedPod, err := ctrl.getUnassignedPod(ctx, fn)
		if err != nil {
			return nil, fmt.Errorf("failed to get unassigned pod: %w", err)
		}

		instance, retryPick, err := ctrl.tryAssignPod(ctx, fn, unassignedPod)
		if retryPick {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(podRetryPickBackoff + time.Duration(rand.Int63n(int64(podRetryPickBackoff)))):
			}
			continue
		}
		return instance, err
	}
}

// tryAssignPod runs one reservation-guarded assign attempt against the picked
// pod. retryPick==true means another goroutine in this controller already
// holds the reservation, OR the SSA apply lost a cross-controller race for
// field-manager ownership (apiserver Conflict); either way the caller loops
// back to getUnassignedPod for a different candidate. retryPick==false means
// the attempt either succeeded (instance non-nil, err nil) or failed
// terminally (err set, deletePod was invoked to release the pod K8s-side
// before the reservation released).
func (ctrl *Controller) tryAssignPod(ctx context.Context, fn *skipper.Function, unassignedPod *v1.Pod) (instance *skipper.Instance, retryPick bool, err error) {
	reservationKey := unassignedPod.Namespace + "/" + unassignedPod.Name
	if _, claimed := ctrl.podReservations.LoadOrStore(reservationKey, struct{}{}); claimed {
		log.Trace(ctx, "pod reservation held, retrying", key.Pod.Slog(unassignedPod))
		return nil, true, nil
	}
	// Reservation deferred first so it releases LAST (LIFO) -- after the
	// deletePod cleanup below has had its chance to run on the failure path.
	// A peer goroutine that loops back through getUnassignedPod will not see
	// this pod as a candidate again until both the K8s state and the in-
	// memory reservation are clean.
	defer ctrl.podReservations.Delete(reservationKey)

	port, err := ctrl.portFromPod(unassignedPod)
	if err != nil {
		return nil, false, err
	}

	if len(unassignedPod.OwnerReferences) == 0 || unassignedPod.OwnerReferences[0].Kind != "ReplicaSet" {
		return nil, false, fmt.Errorf("unassigned pod %s/%s missing ReplicaSet owner reference", unassignedPod.Namespace, unassignedPod.Name)
	}
	replicaSetName := unassignedPod.OwnerReferences[0].Name

	fnJSON, err := json.Marshal(fn)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal function: %w", err)
	}

	assignedAt := time.Now().UTC().Format(time.RFC3339)
	apply := corev1ac.Pod(unassignedPod.Name, unassignedPod.Namespace).
		WithLabels(map[string]string{key.Tenant.Label: fn.GetTenant()}).
		WithAnnotations(map[string]string{
			skipper.FunctionKey.Label: string(fnJSON),
			key.ReplicaSet.Label:      replicaSetName,
			key.AssignedAt.Label:      assignedAt,
		})

	var assignedPod *v1.Pod
	assignedPod, err = ctrl.applyPod(ctx, apply, metav1.ApplyOptions{FieldManager: ctrl.ssaFieldManager})
	if err != nil {
		if apierrors.IsConflict(err) {
			// A peer's field manager already owns this pod's tenant
			// (cross-controller race during a ring-rebalance window).
			// ApplyOptions.Force is intentionally unset so the partition
			// guarantee holds; retrying the same apply with the same
			// manager is futile. Surface as retryPick so assignPod loops
			// back through getUnassignedPod, where the lister will see
			// the winning peer's tenant label and exclude the contested
			// pod from this controller's unassigned pool.
			log.Trace(ctx, "SSA conflict, peer claimed pod", key.Pod.Slog(unassignedPod), key.Error.Slog(err))
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("failed to apply pod: %w", err)
	}

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

	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, assignURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(key.Token.Header, token.V2Sign(ctrl.config.PasetoPrivateKey.V2AsymmetricSecretKey))
	fn.SetHeader(req) // TODO: put the function in the token instead

	log.Info(ctx, "assigning pod", key.Pod.Slog(assignedPod))
	res, err := otelHTTPClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to send assign request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("assign request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
		return nil, false, err
	}

	// Populate ready-at on a deep copy of the assigned pod so instanceFromPod
	// sees it. The apiserver patch runs asynchronously below; readers that
	// matter for routing (scaleWithoutLock's direct append, oneshot's
	// single-use path) consume this in-memory value, while other readers
	// (informer cache, stuck cleanup, HPA CPU) tolerate a brief delay. We
	// deep-copy because applyPod already inserted assignedPod into the
	// informer indexer and informer background goroutines iterate its
	// annotations.
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
	return instance, false, err
}

func (ctrl *Controller) getUnassignedPod(ctx context.Context, fn *skipper.Function) (*v1.Pod, error) {
	ctx, span := telemetry.Trace(ctx, "controller.get_unassigned_pod")
	defer span.End()

	waitingForUnassignedPods.WithLabelValues(fn.GetDeployment()).Inc()
	defer waitingForUnassignedPods.WithLabelValues(fn.GetDeployment()).Dec()

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

func (ctrl *Controller) getUnassignedPods(fn *skipper.Function) ([]*v1.Pod, error) {
	equalDeploymentName, err := labels.NewRequirement(key.Deployment.Label, selection.Equals, []string{fn.GetDeployment()})
	if err != nil {
		return nil, err
	}

	pods, err := ctrl.listPods(fn.GetNamespace(), doesNotHaveTenantSelector.Add(*equalDeploymentName))
	if err != nil {
		return nil, err
	}

	// filter out pods that are unready
	pods = slices.DeleteFunc(pods, func(pod *v1.Pod) bool { return !isPodReady(pod) })

	// Partition by ring ownership so multiple controllers don't race on the
	// same K8s pod. owned is a fresh slice so the caller can't see partial
	// state when the filter leaves us with zero candidates.
	owned := slices.Clone(pods)
	owned = slices.DeleteFunc(owned, func(pod *v1.Pod) bool { return !ctrl.podOwnedByMe(pod.Name) })
	if len(owned) > 0 {
		return owned, nil
	}
	// Empty-partition fallback: when ring topology and the available pool
	// disagree (e.g., cohort transition window, deployment pool smaller than
	// the controller fleet, or an unlucky hash distribution), return the
	// unfiltered pool so the function-responsible controller can still make
	// progress. SSA's field-manager conflict detection and the per-pod
	// in-process reservation in assignPod keep this safe under cross-
	// controller races. Clone so the caller can mutate the result without
	// reaching the lister's slice.
	return slices.Clone(pods), nil
}

func (ctrl *Controller) getReadyInstances(ctx context.Context, fn *skipper.Function) ([]*skipper.Instance, error) {
	instances, err := ctrl.getInstances(ctx, fn)
	if err != nil {
		return nil, err
	}

	// filter out instances that are unready
	return slices.DeleteFunc(instances, func(instance *skipper.Instance) bool { return !instance.HasReadyAt() }), nil
}

func (ctrl *Controller) getInstances(ctx context.Context, fn *skipper.Function) ([]*skipper.Instance, error) {
	return ctrl.collectInstances(ctx, fn, true)
}

// listInstances is a read-only variant of getInstances that does NOT
// delete invalid pods. Use from observability paths (web UI, status
// RPCs) where the call must not have side effects.
func (ctrl *Controller) listInstances(ctx context.Context, fn *skipper.Function) ([]*skipper.Instance, error) {
	return ctrl.collectInstances(ctx, fn, false)
}

func (ctrl *Controller) collectInstances(ctx context.Context, fn *skipper.Function, cleanup bool) ([]*skipper.Instance, error) {
	namespaceLister, found := ctrl.namespaceListers[fn.GetNamespace()]
	if !found {
		return nil, fmt.Errorf("managed pod lister not started for namespace %s", fn.GetNamespace())
	}

	hashKey := strconv.FormatUint(fn.Hash(), 10)
	objs, err := namespaceLister.podIndexer.ByIndex(functionHashIndex, hashKey)
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

// applyPod runs a server-side-apply patch and mirrors patchPod's informer-
// cache update so readers see the new tenant/function/replicaSet/assignedAt
// labels and annotations before the watch event arrives.
func (ctrl *Controller) applyPod(ctx context.Context, apply *corev1ac.PodApplyConfiguration, opts metav1.ApplyOptions) (*v1.Pod, error) {
	if apply.Namespace == nil || apply.Name == nil {
		return nil, fmt.Errorf("applyPod requires namespace and name on the apply configuration")
	}
	pod, err := ctrl.kubernetes.CoreV1().Pods(*apply.Namespace).Apply(ctx, apply, opts)
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

func (ctrl *Controller) functionFromPod(pod *v1.Pod) (*skipper.Function, error) {
	if pod == nil {
		return nil, errors.New("pod is nil")
	}

	fnJSON, ok := pod.Annotations[skipper.FunctionKey.Label]
	if !ok {
		return nil, errors.New("missing function annotation")
	}

	if fn, ok := ctrl.functionCache.Load(fnJSON); ok {
		return fn, nil
	}

	fn := &skipper.Function{}
	if err := json.Unmarshal([]byte(fnJSON), fn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal function from pod annotation: %w", err)
	}
	if err := fn.Validate(); err != nil {
		return nil, fmt.Errorf("invalid function in pod annotation: %w", err)
	}

	ctrl.functionCache.Store(fnJSON, fn)
	return fn, nil
}

func (ctrl *Controller) instanceFromPod(pod *v1.Pod) (*skipper.Instance, error) {
	fn, err := ctrl.functionFromPod(pod)
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
	instance.SetFunction(fn)
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
