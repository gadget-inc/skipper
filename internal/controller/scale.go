package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/telemetry"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/goccy/go-json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

func (ctrl *Controller) scaleFunctions(ctx context.Context, namespace string) error {
	ctx, span := telemetry.StartRoot(ctx, "controller.scale_functions")
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

		if !ctrl.isResponsibleForFunction(instance.Function) {
			log.Trace(ctx, "skipping scaling for function, not assigned to this controller", key.Function.Field(instance.Function))
			continue
		}

		if _, ok := fnInstances[instance.Function]; !ok {
			fnInstances[instance.Function] = nil // ensure the function is in the map so that we loop over all the functions in the next step
		}

		if instance.ReadyAt.IsZero() && time.Since(instance.AssignedAt) > function.FlagAssignTimeout.Value()*2 {
			log.Warn(ctx, "terminating instance stuck in assigned state", key.Instance.Field(instance))
			err = ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Error(ctx, "failed to terminate instance stuck in assigned state", key.Error.Field(err), key.Pod.Field(pod))
			}
			continue
		}

		if podMetric, exists := podNameToMetric[pod.Name]; exists {
			for _, container := range podMetric.Containers {
				if container.Usage.Cpu() != nil {
					cpuUsage := container.Usage.Cpu().MilliValue()
					instance.CPUUsage += int(cpuUsage)
				}
				if container.Usage.Memory() != nil {
					memUsage := container.Usage.Memory().Value()
					instance.MemoryUsage += int(memUsage)
				}
			}
		}

		replicaSet, err := ctrl.namespaceListers[namespace].replicaSetLister.ReplicaSets(namespace).Get(instance.ReplicaSet)
		if err != nil {
			log.Error(ctx, "failed to get replica set for instance", key.Error.Field(err), key.Instance.Field(instance))
			continue
		}

		if replicaSet.Status.Replicas > 0 {
			// instance is running on the latest replica set
			fnInstances[instance.Function] = append(fnInstances[instance.Function], instance)
			continue
		}

		// this is a stale instance, find the active replica set
		replicaSets, err := ctrl.namespaceListers[namespace].replicaSetLister.List(labels.SelectorFromSet(labels.Set{key.Deployment.Label: instance.Deployment}))
		if err != nil {
			log.Error(ctx, "failed to list replica sets", key.Error.Field(err), key.Instance.Field(instance))
			continue
		}

		var activeReplicaSet *appsv1.ReplicaSet
		for _, replicaSet := range replicaSets {
			if replicaSet.Status.Replicas > 0 {
				activeReplicaSet = replicaSet
				break
			}
		}

		if activeReplicaSet != nil && activeReplicaSet.Status.AvailableReplicas < max(1, activeReplicaSet.Status.Replicas/2) {
			log.Info(ctx, "replica set does not have enough available replicas to terminate stale instance", key.Instance.Field(instance), key.ReplicaSet.Field(activeReplicaSet))
			continue
		}

		scaleMu, _ := ctrl.scaleMu.LoadOrCompute(instance.Function, func() *sync.Mutex { return new(sync.Mutex) })
		scaleMu.Lock()
		log.Info(ctx, "terminating stale instance", key.Instance.Field(instance))
		err = ctrl.kubernetes.CoreV1().Pods(namespace).Delete(ctx, instance.Name, metav1.DeleteOptions{})
		if err != nil {
			log.Error(ctx, "failed to terminate stale instance", key.Error.Field(err), key.Instance.Field(instance))
		}
		scaleMu.Unlock()
	}

	var wg sync.WaitGroup

	now := time.Now()
	for fn, instances := range fnInstances {
		wg.Add(1)

		go func() {
			ctx, span := telemetry.StartRoot(ctx, "controller.scale_functions.scale_function", trace.WithAttributes(key.Function.Attributes(fn)...))
			defer span.End()
			defer wg.Done()

			routerHeartbeats, _ := ctrl.routerHeartbeats.Load(fn)
			heartbeat := routerHeartbeats.Combined()
			for _, instance := range instances {
				if instance.AssignedAt.After(heartbeat.Timestamp) {
					heartbeat.Timestamp = instance.AssignedAt
				}
			}

			if time.Since(heartbeat.Timestamp) >= FlagHeartbeatTimeout.Value() {
				log.Info(ctx, "scaling function to 0 due to heartbeat timeout", key.Heartbeat.Field(heartbeat))
				_, err := ctrl.scaleFunction(ctx, fn, 0)
				if err != nil {
					log.Error(ctx, "failed to scale function to 0", key.Error.Field(err), key.Function.Field(fn))
				}
				ctrl.scaleMu.Delete(fn)
				ctrl.routerHeartbeats.Delete(fn)
				ctrl.stabilizationWindows.Delete(fn)
				return
			}

			desiredInstances := calculateDesiredInstances(ctx, heartbeat, instances, now)

			stabilizationWindow, _ := ctrl.stabilizationWindows.LoadOrCompute(fn, func() *StabilizationWindow { return new(StabilizationWindow) })
			stabilizationWindow.RecordRecommendation(desiredInstances, now)

			currentInstances := len(instances)
			if desiredInstances < currentInstances {
				desiredInstances = min(currentInstances, stabilizationWindow.GetMaxRecommendation()) // scale down to the max recommendation within the stabilization window
			}

			if desiredInstances == 0 {
				desiredInstances = 1 // only scale to 0 from a heartbeat timeout
			}

			_, err = ctrl.scaleFunction(ctx, fn, desiredInstances)
			if err != nil {
				log.Error(ctx, "failed to scale function to desired instances", key.Error.Field(err), key.Function.Field(fn), key.CurrentInstances.Field(currentInstances), key.DesiredInstances.Field(desiredInstances))
			}
		}()
	}

	wg.Wait()

	return nil
}

func (ctrl *Controller) scaleFunction(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
	ctx, span := telemetry.Start(ctx, "controller.scale_function")
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

	controllerIP := ctrl.ring.Get(fn.RingKey())
	if controllerIP != FlagPodIP.Value() {
		log.Debug(ctx, "forwarding scale request", key.Function.Field(fn), key.ControllerIP.Field(controllerIP))
		return ctrl.getControllerClient(controllerIP).Scale(ctx, fn, desiredInstances)
	}

	log.Info(ctx, "scaling function",
		key.Function.Field(fn),
		key.CurrentInstances.Field(currentInstances),
		key.DesiredInstances.Field(desiredInstances),
	)

	if desiredInstances > currentInstances {
		for range desiredInstances - currentInstances {
			instance, err := ctrl.assignPodToFunction(ctx, fn)
			if err != nil {
				return nil, fmt.Errorf("failed to assign pod: %w", err)
			}
			instances = append(instances, instance)
			log.Trace(ctx, "assigned pod", key.Instance.Field(instance))
		}
	} else {
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
			log.Trace(ctx, "deleted pod", key.Instance.Field(instance))
		}
	}

	return instances, nil
}

func (ctrl *Controller) assignPodToFunction(ctx context.Context, fn function.Function) (*function.Instance, error) {
	ctx, span := telemetry.Start(ctx, "controller.assign_pod_to_function", trace.WithAttributes(key.Function.Attributes(fn)...))
	defer span.End()

	pod, err := timer.PollUntil(ctx, "controller.get_unassigned_pod", 250*time.Millisecond, func(ctx context.Context) (*v1.Pod, error) {
		span := trace.SpanFromContext(ctx)
		span.SetAttributes(key.Function.Attributes(fn)...)

		availablePods, err := ctrl.getAvailablePodsForFunction(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to list available pods: %w", err)
		}
		if len(availablePods) == 0 {
			log.Warn(ctx, "no available pods", key.Function.Field(fn))
			return nil, nil
		}
		return availablePods[rand.Intn(len(availablePods))], nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to poll for available pod: %w", err)
	}

	fnBytes, err := json.Marshal(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal function: %w", err)
	}

	assignPatches := []patchOperation{
		{Op: "test", Path: "/metadata/ownerReferences/0/kind", Value: "ReplicaSet"},                     // ensure the pod is assigned to a replica set
		{Op: "copy", From: "/metadata/ownerReferences/0/name", Path: key.ReplicaSet.PatchAnnotation},    // copy its name so we know which replica set the pod belonged to before it was assigned
		{Op: "add", Path: key.Tenant.PatchLabel, Value: fn.Tenant},                                      // label the pod with the tenant it belongs to
		{Op: "add", Path: key.Function.PatchAnnotation, Value: string(fnBytes)},                         // annotate the pod with the function it is assigned to
		{Op: "add", Path: key.AssignedAt.PatchAnnotation, Value: time.Now().UTC().Format(time.RFC3339)}, // annotate the pod with the time it was assigned
	}

	patchBody, err := json.Marshal(assignPatches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal assign patch: %w", err)
	}

	pod, err = ctrl.kubernetes.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patchBody, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		// there are many reasons this can fail, but one hard to debug
		// one is that the pod doesn't have any annotations causing the
		// json patch to fail: https://github.com/kubernetes-sigs/kustomize/issues/2986#issuecomment-692891118
		return nil, fmt.Errorf("failed to patch pod: %w", err)
	}

	ctrl.updatePodCache(ctx, pod)

	port, err := portFromPod(pod)
	if err != nil {
		return nil, err
	}

	assignURL := "http://" + net.JoinHostPort(pod.Status.PodIP, port) + function.FlagAssignPath.Value()
	assignCtx, cancel := context.WithTimeout(ctx, function.FlagAssignTimeout.Value())
	defer cancel()

	token := paseto.NewToken()
	token.SetSubject(fn.Tenant)
	token.SetIssuedAt(time.Now())
	token.SetNotBefore(time.Now())
	token.SetExpiration(time.Now().Add(7 * 24 * time.Hour))

	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, assignURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(key.Token.Header, token.V2Sign(FlagPasetoPrivateKey.Value()))
	fn.SetHeader(req) // TODO: put the function in the token instead

	log.Info(ctx, "assigning pod", key.Pod.Field(pod), key.Function.Field(fn))
	res, err := otelhttp.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send assign request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("assign request failed: status=%d body=%s", res.StatusCode, getResponseBody(res))
	}

	setReadyPatches := []patchOperation{
		{Op: "add", Path: key.ReadyAt.PatchAnnotation, Value: time.Now().UTC().Format(time.RFC3339)},
	}

	patchBody, err = json.Marshal(setReadyPatches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal set ready patch: %w", err)
	}

	pod, err = ctrl.kubernetes.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patchBody, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		return nil, fmt.Errorf("failed to patch status: %w", err)
	}

	ctrl.updatePodCache(ctx, pod)

	return instanceFromPod(pod)
}

func (ctrl *Controller) getAvailablePodsForFunction(fn function.Function) ([]*v1.Pod, error) {
	equalDeploymentName, err := labels.NewRequirement(key.Deployment.Label, selection.Equals, []string{fn.Deployment})
	if err != nil {
		return nil, err
	}

	pods, err := ctrl.listPods(fn.Namespace, doesNotHaveTenantSelector.Add(*equalDeploymentName))
	if err != nil {
		return nil, err
	}

	var availablePods []*v1.Pod
	for _, pod := range pods {
		if pod.Status.PodIP != "" {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
					availablePods = append(availablePods, pod)
				}
			}
		}
	}

	return availablePods, nil
}

type patchOperation struct {
	Op    string `json:"op"`
	From  string `json:"from,omitempty"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty"`
}

type Metric string

const (
	MetricCPU    Metric = "cpu"
	MetricMemory Metric = "memory"
)

// Recommendation represents a scaling recommendation at a point in time
type Recommendation struct {
	Instances int
	Timestamp time.Time
}

// StabilizationWindow represents a window of scaling recommendations
type StabilizationWindow struct {
	Recommendations []Recommendation
}

// RecordRecommendation adds a new recommendation and prunes old ones
func (sw *StabilizationWindow) RecordRecommendation(desiredInstances int, timestamp time.Time) {
	sw.Recommendations = append(sw.Recommendations, Recommendation{
		Instances: desiredInstances,
		Timestamp: timestamp,
	})

	// Remove old recommendations
	cutoff := timestamp.Add(-FlagHPADownscaleStabilization.Value())
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
	var maxInstances int
	for _, rec := range sw.Recommendations {
		if rec.Instances > maxInstances {
			maxInstances = rec.Instances
		}
	}
	return maxInstances
}

// calculateDesiredInstancesForMetric computes desired instances based on a single metric
func calculateDesiredInstancesForMetric(_ context.Context, metric Metric, instances []*function.Instance, timestamp time.Time) int {
	currentInstances := len(instances)
	var instancesWithMetrics []*function.Instance
	var instancesWithoutMetrics []*function.Instance

	for _, instance := range instances {
		var usage int
		switch metric {
		case MetricCPU:
			usage = instance.CPUUsage
		case MetricMemory:
			usage = instance.MemoryUsage
		default:
			return currentInstances
		}

		if metric == MetricCPU && (instance.ReadyAt.IsZero() || timestamp.Sub(instance.ReadyAt) <= FlagHPAInitialReadinessDelay.Value()) {
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
			totalUsage += instance.CPUUsage
		case MetricMemory:
			targetUsage = instance.Scale.TargetMemoryUsageMiB
			totalUsage += instance.MemoryUsage / 1024 / 1024 // convert memory usage from bytes to MiB
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
			// we wanted to scale down, so we assume that instances without metrics are consuming 100% of target usage
			adjustedTotalUsage += len(instancesWithoutMetrics) * targetUsage
		} else {
			// we wanted to scale up, so we assume that instances without metrics are consuming 0% of target usage
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
func calculateDesiredInstances(ctx context.Context, heartbeat function.Heartbeat, instances []*function.Instance, timestamp time.Time) int {
	maxDesiredInstances := 0

	for _, metric := range []Metric{MetricCPU, MetricMemory} {
		desiredInstances := calculateDesiredInstancesForMetric(ctx, metric, instances, timestamp)
		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
		}
	}

	if heartbeat.Function.Scale.TargetRequestsPerInstance > 0 {
		desiredInstances := int(math.Ceil(float64(heartbeat.Requests) / float64(heartbeat.Function.Scale.TargetRequestsPerInstance)))
		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
		}
	}

	desiredInstances := min(max(maxDesiredInstances, heartbeat.Function.Scale.MinInstances), heartbeat.Function.Scale.MaxInstances)

	instanceAttrs := make([]any, 0, len(instances))
	for i, instance := range instances {
		instanceAttrs = append(instanceAttrs, slog.Group(strconv.Itoa(i),
			slog.String("name", instance.Name),
			slog.String("addr", instance.Addr),
			slog.String("replica_set", instance.ReplicaSet),
			slog.Time("assigned_at", instance.AssignedAt),
			slog.Time("ready_at", instance.ReadyAt),
			slog.Int("cpu_usage", instance.CPUUsage),
			slog.Int("memory_usage", instance.MemoryUsage),
		))
	}
	log.Info(ctx, "calculated desired instances", key.DesiredInstances.Field(desiredInstances), key.Heartbeat.Field(heartbeat), slog.Group("instances", instanceAttrs...))

	return desiredInstances
}

type RouterHeartbeats map[string]function.Heartbeat

func (r RouterHeartbeats) Combined() function.Heartbeat {
	var combined function.Heartbeat
	for _, heartbeat := range r {
		combined.Requests += heartbeat.Requests
		if combined.Timestamp.Before(heartbeat.Timestamp) {
			combined.Timestamp = heartbeat.Timestamp
		}
	}
	return combined
}
