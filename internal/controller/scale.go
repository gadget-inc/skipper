package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	"github.com/goccy/go-json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

var (
	hasTenantSelector         = labels.NewSelector().Add(*unwrap(labels.NewRequirement(key.Tenant.Label, selection.Exists, nil)))
	doesNotHaveTenantSelector = labels.NewSelector().Add(*unwrap(labels.NewRequirement(key.Tenant.Label, selection.DoesNotExist, nil)))

	waitingForUnassignedPodCounter = unwrap(telemetry.Meter.Int64UpDownCounter("controller.waiting_for_unassigned_pods",
		metric.WithDescription("The number of functions that are waiting for an unassigned pod"),
		metric.WithUnit("{function}"),
	))

	heartbeatsCounter = unwrap(telemetry.Meter.Int64Counter("controller.heartbeats",
		metric.WithDescription("The number of heartbeats received by the controller"),
		metric.WithUnit("{heartbeat}"),
	))

	scaleUpCounter = unwrap(telemetry.Meter.Int64Counter("controller.scale_ups",
		metric.WithDescription("The number of times the controller has scaled a function up"),
		metric.WithUnit("{scale_up}"),
	))

	scaleDownCounter = unwrap(telemetry.Meter.Int64Counter("controller.scale_downs",
		metric.WithDescription("The number of times the controller has scaled a function down"),
		metric.WithUnit("{scale_down}"),
	))
)

func (ctrl *Controller) scaleNamespace(ctx context.Context, namespace string) error {
	ctx, span := telemetry.TraceRoot(ctx, "controller.scale_namespace", trace.WithAttributes(key.Namespace.Attribute(namespace)))
	defer span.End()

	pods, err := ctrl.listPods(namespace, hasTenantSelector)
	if err != nil {
		return fmt.Errorf("failed to get assigned pods: %w", err)
	}

	// TODO: paginate
	metrics, err := ctrl.kubernetesMetrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{LabelSelector: key.Tenant.Label})
	if err != nil {
		// TODO: make this recoverable
		return fmt.Errorf("failed to get metrics: %w", err)
	}

	podNameToMetric := make(map[string]metricsv1beta1.PodMetrics, len(metrics.Items))
	for _, metric := range metrics.Items {
		podNameToMetric[metric.Name] = metric
	}

	fnInstances := make(map[function.Function][]*function.Instance)

	for _, pod := range pods {
		instance, err := instanceFromPod(pod)
		if err != nil {
			log.Warn(ctx, "failed to get instance from pod", key.Error.Field(err), key.Pod.Field(pod))
			err = ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Error(ctx, "failed to terminate pod", key.Error.Field(err), key.Pod.Field(pod))
			}
			continue
		}

		ctx := log.With(ctx, key.Instance.Field(instance))

		responsibleIP := ctrl.ring.Get(instance)
		if responsibleIP != FlagPodIP.Value() {
			log.Trace(ctx, "skipping scaling for function, not assigned to this controller", key.ResponsibleIP.Field(responsibleIP))
			continue
		}

		if _, ok := fnInstances[instance.Function]; !ok {
			fnInstances[instance.Function] = nil // ensure the function is in the map so that we loop over all the functions in the next step
		}

		if instance.ReadyAt.IsZero() && time.Since(instance.AssignedAt) > function.FlagAssignTimeout.Value()*2 {
			log.Warn(ctx, "terminating instance stuck in assigned state")
			err = ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Error(ctx, "failed to terminate instance stuck in assigned state", key.Error.Field(err))
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
			log.Error(ctx, "failed to get replica set for instance", key.Error.Field(err))
			continue
		}

		if replicaSet.Status.Replicas > 0 {
			// instance is running on a replica set that has replicas, so it isn't stale
			fnInstances[instance.Function] = append(fnInstances[instance.Function], instance)
			continue
		}

		// this is a stale instance, find a replica set that has replicas
		replicaSets, err := ctrl.namespaceListers[namespace].replicaSetLister.List(labels.SelectorFromSet(labels.Set{key.Deployment.Label: instance.Deployment}))
		if err != nil {
			log.Error(ctx, "failed to list replica sets", key.Error.Field(err))
			continue
		}

		var activeReplicaSet *appsv1.ReplicaSet
		for _, replicaSet := range replicaSets {
			if replicaSet.Status.Replicas > 0 {
				activeReplicaSet = replicaSet
				break
			}
		}

		ctx = log.With(ctx, key.ReplicaSet.Field(activeReplicaSet))

		if activeReplicaSet != nil && activeReplicaSet.Status.AvailableReplicas < max(1, activeReplicaSet.Status.Replicas/FlagAvailableReplicaDivisor.Value()) {
			log.Info(ctx, "replica set does not have enough available replicas to terminate stale instance")
			continue
		}

		scaleMu, _ := ctrl.scaleMu.LoadOrCompute(instance.Function, func() *sync.Mutex { return new(sync.Mutex) })
		scaleMu.Lock()
		log.Info(ctx, "terminating stale instance")
		err = ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, instance.Name, metav1.DeleteOptions{})
		if err != nil {
			log.Error(ctx, "failed to terminate stale instance", key.Error.Field(err))
		}
		scaleMu.Unlock()
	}

	var wg sync.WaitGroup

	for fn, instances := range fnInstances {
		wg.Add(1)

		go func() {
			ctx, span := telemetry.TraceRoot(ctx, "controller.scale_function")
			defer span.End()
			defer wg.Done()

			var heartbeat function.Heartbeat
			if routerHeartbeats, ok := ctrl.routerHeartbeats.Load(fn); ok {
				heartbeat = routerHeartbeats.Combined() // use the combined heartbeat from all the routers
			} else {
				heartbeat.Function = fn // use the empty heartbeat and associate it with the function
			}

			for _, instance := range instances {
				if instance.AssignedAt.After(heartbeat.Timestamp) {
					heartbeat.Timestamp = instance.AssignedAt
				}
			}

			ctx = log.With(ctx, key.Heartbeat.Field(heartbeat))
			ctx = telemetry.WithPropagatedAttributes(ctx, key.Heartbeat.Attributes(heartbeat)...)

			desiredInstances := calculateDesiredInstances(ctx, heartbeat, instances)

			stabilizationWindow, _ := ctrl.stabilizationWindows.LoadOrCompute(fn, func() *StabilizationWindow { return new(StabilizationWindow) })
			stabilizationWindow.RecordRecommendation(desiredInstances)

			currentInstances := len(instances)
			if desiredInstances < currentInstances {
				// we're scaling down
				if desiredInstances == 0 {
					// we're scaling to 0, so forget about this function after we're done
					defer ctrl.scaleMu.Delete(fn)
					defer ctrl.routerHeartbeats.Delete(fn)
					defer ctrl.stabilizationWindows.Delete(fn)
				} else {
					// we're scaling down, but not to 0, so scale down to the max recommended instances within the stabilization window
					// if we're already lower than the max recommended instances, then use the current number of instances (i.e. don't scale up)
					desiredInstances = min(stabilizationWindow.GetMaxRecommendation(), currentInstances)
				}
			}

			_, err = ctrl.scale(ctx, fn, desiredInstances)
			if err != nil {
				log.Error(ctx, "failed to scale function to desired instances", key.Error.Field(err))
			}
		}()
	}

	wg.Wait()

	return nil
}

func (ctrl *Controller) scale(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
	ctx, span := telemetry.Trace(ctx, "controller.scale")
	defer span.End()

	scaleMu, _ := ctrl.scaleMu.LoadOrCompute(fn, func() *sync.Mutex { return new(sync.Mutex) })
	scaleMu.Lock()
	defer scaleMu.Unlock()

	instances, err := ctrl.getInstances(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	currentInstances := len(instances)
	if currentInstances == desiredInstances {
		return instances, nil
	}

	responsibleIP := ctrl.ring.Get(fn)
	if responsibleIP != FlagPodIP.Value() {
		log.Debug(ctx, "forwarding scale request to responsible controller", key.ResponsibleIP.Field(responsibleIP))
		return ctrl.getControllerClient(responsibleIP).Scale(ctx, fn, desiredInstances)
	}

	if desiredInstances > currentInstances {
		log.Info(ctx, "scaling function up", key.CurrentInstances.Field(currentInstances), key.DesiredInstances.Field(desiredInstances))
		scaleUpCounter.Add(ctx, 1, metric.WithAttributeSet(key.Function.AttributesSet(fn)))

		for range desiredInstances - currentInstances {
			instance, err := ctrl.assignPod(ctx, fn)
			if err != nil {
				return nil, fmt.Errorf("failed to assign pod: %w", err)
			}
			instances = append(instances, instance)
		}
	} else {
		log.Info(ctx, "scaling function down", key.CurrentInstances.Field(currentInstances), key.DesiredInstances.Field(desiredInstances))
		scaleDownCounter.Add(ctx, 1, metric.WithAttributeSet(key.Function.AttributesSet(fn)))

		// sort instances by assigned at in ascending order
		slices.SortFunc(instances, func(a, b *function.Instance) int { return a.AssignedAt.Compare(b.AssignedAt) })

		// iterate over instances in reverse order, deleting the oldest ones first
		for i := len(instances) - 1; i >= desiredInstances; i-- {
			instance := instances[i]
			err := ctrl.kubernetes.CoreV1().Pods(instance.Namespace).Delete(ctx, instance.Name, metav1.DeleteOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to delete pod: %w", err)
			}
			instances = instances[:i]
		}
	}

	return instances, nil
}

func (ctrl *Controller) assignPod(ctx context.Context, fn function.Function) (instance *function.Instance, err error) {
	ctx, span := telemetry.Trace(ctx, "controller.assign_pod")
	defer span.End()

GET_UNASSIGNED_POD:
	var pod *v1.Pod
	pod, err = ctrl.getUnassignedPod(ctx, fn)
	if err != nil {
		return nil, fmt.Errorf("failed to get unassigned pod: %w", err)
	}

	var port string
	port, err = portFromPod(pod)
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

	log.Info(ctx, "assigning pod", key.Pod.Field(pod))
	pod, err = ctrl.kubernetes.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patches, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		if errors.Is(err, jsonpatch.ErrTestFailed) {
			log.Warn(ctx, "failed to patch pod, retrying", key.Error.Field(err), key.Pod.Field(pod))
			goto GET_UNASSIGNED_POD
		}
		// there are many reasons this can fail, but one hard to debug
		// one is that the pod doesn't have any annotations, causing the
		// json patch to fail: https://datatracker.ietf.org/doc/html/rfc6902#appendix-A.12
		return nil, fmt.Errorf("failed to patch pod: %w", err)
	}

	// at this point, terminate the pod if we fail to finish assigning the function
	defer func() {
		if err != nil {
			if deleteErr := ctrl.kubernetes.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); deleteErr != nil {
				log.Error(ctx, "failed to delete pod after failed assign request", key.Error.Field(deleteErr), key.Pod.Field(pod))
			}
		}
	}()

	ctrl.updatePodCache(ctx, pod)

	assignURL := "http://" + net.JoinHostPort(pod.Status.PodIP, port) + function.FlagAssignPath.Value()
	assignCtx, cancel := context.WithTimeout(ctx, function.FlagAssignTimeout.Value())
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

	req.Header.Set(key.Token.Header, token.V2Sign(FlagPasetoPrivateKey.Value()))
	fn.SetHeader(req) // TODO: put the function in the token instead

	var res *http.Response
	res, err = otelhttp.DefaultClient.Do(req)
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
	pod, err = ctrl.kubernetes.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patches, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		return nil, fmt.Errorf("failed to patch pod as ready: %w", err)
	}

	ctrl.updatePodCache(ctx, pod)

	instance, err = instanceFromPod(pod)
	return
}

func (ctrl *Controller) getUnassignedPod(ctx context.Context, fn function.Function) (*v1.Pod, error) {
	ctx, span := telemetry.Trace(ctx, "controller.get_unassigned_pod")
	defer span.End()

	waitingForUnassignedPodCounter.Add(ctx, 1, metric.WithAttributeSet(key.Function.AttributesSet(fn)))
	defer waitingForUnassignedPodCounter.Add(ctx, -1, metric.WithAttributeSet(key.Function.AttributesSet(fn)))

	return timer.Poll(ctx, 250*time.Millisecond, func(ctx context.Context) (*v1.Pod, error) {
		unassignedPods, err := ctrl.getUnassignedPods(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to list unassigned pods: %w", err)
		}
		if len(unassignedPods) == 0 {
			log.Warn(ctx, "no unassigned pods")
			return nil, nil
		}
		return unassignedPods[rand.Intn(len(unassignedPods))], nil
	})
}

func (ctrl *Controller) getUnassignedPods(fn function.Function) ([]*v1.Pod, error) {
	equalDeploymentName, err := labels.NewRequirement(key.Deployment.Label, selection.Equals, []string{fn.Deployment})
	if err != nil {
		return nil, err
	}

	pods, err := ctrl.listPods(fn.Namespace, doesNotHaveTenantSelector.Add(*equalDeploymentName))
	if err != nil {
		return nil, err
	}

	var unassignedPods []*v1.Pod
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil || pod.Status.PodIP == "" {
			continue
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
				unassignedPods = append(unassignedPods, pod)
				break
			}
		}
	}

	return unassignedPods, nil
}

type Metric string

const (
	MetricCPU    Metric = "cpu"
	MetricMemory Metric = "memory"
)

// Recommendation represents a scaling recommendation at a point in time
type Recommendation struct {
	DesiredInstances int
	Timestamp        time.Time
}

// StabilizationWindow represents a window of scaling recommendations
type StabilizationWindow struct {
	Recommendations []Recommendation
}

// RecordRecommendation adds a new recommendation and prunes old ones
func (sw *StabilizationWindow) RecordRecommendation(desiredInstances int) {
	now := time.Now()
	sw.Recommendations = append(sw.Recommendations, Recommendation{
		DesiredInstances: desiredInstances,
		Timestamp:        now,
	})

	// remove old recommendations
	cutoff := now.Add(-FlagHPADownscaleStabilization.Value())
	var newRecommendations []Recommendation
	for _, rec := range sw.Recommendations {
		if rec.Timestamp.After(cutoff) {
			newRecommendations = append(newRecommendations, rec)
		}
	}
	sw.Recommendations = newRecommendations
}

// GetMaxRecommendation returns the maximum recommended instances in the window
func (sw *StabilizationWindow) GetMaxRecommendation() int {
	var maxDesiredInstances int
	for _, rec := range sw.Recommendations {
		if rec.DesiredInstances > maxDesiredInstances {
			maxDesiredInstances = rec.DesiredInstances
		}
	}
	return maxDesiredInstances
}

// calculateDesiredInstancesForMetric computes desired instances based on a single metric
func calculateDesiredInstancesForMetric(_ context.Context, metric Metric, instances []*function.Instance) int {
	currentInstances := len(instances)
	var instancesWithMetrics []*function.Instance
	var instancesWithoutMetrics []*function.Instance

	for _, instance := range instances {
		var usage int
		switch metric {
		case MetricCPU:
			usage = instance.CPUUsageMilli
		case MetricMemory:
			usage = instance.MemoryUsageMiB
		default:
			return currentInstances
		}

		if metric == MetricCPU && (instance.ReadyAt.IsZero() || time.Since(instance.ReadyAt) <= FlagHPAInitialReadinessDelay.Value()) {
			// ignore CPU metrics for pods that have been ready for less than the initial readiness delay
			instancesWithoutMetrics = append(instancesWithoutMetrics, instance)
			continue
		}

		if usage == 0 {
			instancesWithoutMetrics = append(instancesWithoutMetrics, instance)
		} else {
			instancesWithMetrics = append(instancesWithMetrics, instance)
		}
	}

	if len(instancesWithMetrics) == 0 {
		return currentInstances
	}

	var targetUsage int
	var totalUsage int
	for _, instance := range instancesWithMetrics {
		// accumulate total usage and keep track of target usage (they should all be identical)
		switch metric {
		case MetricCPU:
			targetUsage = instance.Scale.TargetCPUUsageMilli
			totalUsage += instance.CPUUsageMilli
		case MetricMemory:
			targetUsage = instance.Scale.TargetMemoryUsageMiB
			totalUsage += instance.MemoryUsageMiB
		}
	}

	if targetUsage == 0 {
		// target usage = 0 means don't scale on this metric
		return currentInstances
	}

	averageUsage := float64(totalUsage) / float64(len(instancesWithMetrics))
	usageRatio := averageUsage / float64(targetUsage)
	usageDiscrepancy := math.Abs(1.0 - usageRatio)
	desiredInstances := int(math.Ceil(float64(currentInstances) * usageRatio))

	if usageDiscrepancy <= FlagHPATolerance.Value()+1e-10 { // add a small epsilon to avoid floating point errors
		// the average usage is within tolerance of the target utilization, so we should not scale
		return currentInstances
	}

	if len(instancesWithoutMetrics) > 0 {
		adjustedTotalUsage := totalUsage
		if desiredInstances < currentInstances {
			// we wanted to scale down, so we assume that instances without metrics are consuming 100% of the target usage
			adjustedTotalUsage += len(instancesWithoutMetrics) * targetUsage
		} else {
			// we wanted to scale up, so we assume that instances without metrics are consuming 0% of the target usage
			adjustedTotalUsage += len(instancesWithoutMetrics) * 0
		}

		adjustedAverageUsage := float64(adjustedTotalUsage) / float64(currentInstances)
		adjustedUsageRatio := adjustedAverageUsage / float64(targetUsage)

		if (adjustedUsageRatio > 1.0 && usageRatio < 1.0) ||
			(adjustedUsageRatio < 1.0 && usageRatio > 1.0) ||
			math.Abs(1.0-adjustedUsageRatio) <= FlagHPATolerance.Value()+1e-10 {
			// the adjusted usage ratio is the opposite of the original
			// usage ratio, or the adjusted usage ratio is within
			// tolerance of the target utilization. either way, we
			// should noop and not scale
			return currentInstances
		}

		desiredInstances = int(math.Ceil(float64(currentInstances) * adjustedUsageRatio))
	}

	return desiredInstances
}

// calculateDesiredInstances computes desired instances based on multiple metrics
func calculateDesiredInstances(ctx context.Context, heartbeat function.Heartbeat, instances []*function.Instance) int {
	if time.Since(heartbeat.Timestamp) >= FlagHeartbeatTimeout.Value() {
		return 0
	}

	maxDesiredInstances := 1 // we only scale to 0 from a heartbeat timeout, so we start at 1

	if heartbeat.Function.Scale.TargetInFlightRequests > 0 {
		desiredInstances := int(math.Ceil(float64(heartbeat.InFlightRequests) / float64(heartbeat.Function.Scale.TargetInFlightRequests)))
		maxDesiredInstances = max(maxDesiredInstances, desiredInstances)
	}

	if heartbeat.Function.Scale.TargetCPUUsageMilli > 0 {
		desiredInstances := calculateDesiredInstancesForMetric(ctx, MetricCPU, instances)
		maxDesiredInstances = max(maxDesiredInstances, desiredInstances)
	}

	if heartbeat.Function.Scale.TargetMemoryUsageMiB > 0 {
		desiredInstances := calculateDesiredInstancesForMetric(ctx, MetricMemory, instances)
		maxDesiredInstances = max(maxDesiredInstances, desiredInstances)
	}

	return min(max(maxDesiredInstances, heartbeat.Function.Scale.MinInstances), heartbeat.Function.Scale.MaxInstances)
}

// RouterHeartbeats is a map of router IP to heartbeat
type RouterHeartbeats map[string]function.Heartbeat

// Combined returns a heartbeat that is the sum of all the heartbeats from all the routers
func (r RouterHeartbeats) Combined() function.Heartbeat {
	var combined function.Heartbeat
	for _, heartbeat := range r {
		combined.Function = heartbeat.Function
		combined.InFlightRequests += heartbeat.InFlightRequests
		if combined.Timestamp.Before(heartbeat.Timestamp) {
			combined.Timestamp = heartbeat.Timestamp
		}
	}
	return combined
}
