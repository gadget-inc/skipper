package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"slices"
	"sync"
	"time"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Supervisor manages scaling decisions for a single function. It tracks
// heartbeats from routers, maintains a stabilization window for
// scale-down decisions, and coordinates with the controller to adjust
// instance counts. Each Supervisor runs its own independent converge
// loop so that functions don't block each other.
type Supervisor struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	once                sync.Once
	mu                  sync.Mutex
	fn                  *function.Function
	ctrl                *Controller
	routerHeartbeats    *xsync.Map[string, *function.Heartbeat]
	stabilizationWindow []Recommendation
}

// supervisor returns the supervisor for the given function, creating it if necessary.
// If the controller has been started (ctrl.ctx is set), the supervisor's loop will
// be started automatically.
func (ctrl *Controller) supervisor(fn *function.Function) *Supervisor {
	supervisor, _ := ctrl.supervisors.LoadOrCompute(fn.Hash(), func() (*Supervisor, bool) {
		return &Supervisor{fn: fn, ctrl: ctrl, routerHeartbeats: xsync.NewMap[string, *function.Heartbeat]()}, false
	})
	supervisor.start(ctrl.ctx)
	return supervisor
}

// discoverSupervisors discovers functions in the given namespace by listing
// assigned pods and ensures supervisors exist for each unique function.
// Supervisors are created even if this controller is not responsible for the
// function, as they track scaling decisions that may be needed if responsibility
// changes. Invalid pods (e.g., with malformed function annotations) are deleted.
func (ctrl *Controller) discoverSupervisors(ctx context.Context, namespace string) error {
	ctx, span := telemetry.TraceRoot(ctx, "controller.discover_supervisors", trace.WithAttributes(key.Namespace.Otel(namespace)...))
	defer span.End()

	pods, err := ctrl.listPods(namespace, hasTenantSelector)
	if err != nil {
		return fmt.Errorf("failed to get assigned pods: %w", err)
	}

	seenFunctions := make(map[function.Hash]bool)

	for _, pod := range pods {
		fn, err := ctrl.functionFromPod(pod)
		if err != nil {
			log.Warn(ctx, "failed to get function from pod", key.Error.Slog(err), key.Pod.Slog(pod))
			err = ctrl.deletePod(ctx, pod.Namespace, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Error(ctx, "failed to terminate pod", key.Error.Slog(err), key.Pod.Slog(pod))
			}
			continue
		}

		if !seenFunctions[fn.Hash()] {
			seenFunctions[fn.Hash()] = true
			ctrl.supervisor(fn) // ensure supervisor exists; its loop handles scaling
		}
	}

	return nil
}

// start begins the supervisor's independent converge loop. It is safe
// to call multiple times; only the first call with a valid context will start the loop.
// If ctx is nil, this is a no-op and does not consume the sync.Once.
func (s *Supervisor) start(ctx context.Context) {
	if ctx == nil {
		return
	}
	s.once.Do(func() {
		s.ctx, s.cancel = context.WithCancel(ctx)
		go s.loop()
	})
}

// loop runs the converge loop at the configured interval until the
// supervisor's context is cancelled. When the loop exits (either from
// scale-to-0 or controller shutdown), it removes itself from the
// supervisors map.
func (s *Supervisor) loop() {
	defer s.ctrl.supervisors.Delete(s.fn.Hash())

	timer.Loop(s.ctx, s.ctrl.config.ScaleInterval, func(ctx context.Context) error {
		defer func() {
			if r := recover(); r != nil {
				log.Error(ctx, "panic in supervisor loop", key.Error.Slog(fmt.Errorf("%v", r)))
			}
		}()

		if err := s.converge(ctx); err != nil {
			log.Error(ctx, "failed to converge", key.Error.Slog(err))
		}

		return nil
	})
}

// stop cancels the supervisor's converge loop context, causing the
// loop to exit.
func (s *Supervisor) stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// heartbeat updates the heartbeat for a specific router if it's newer
// than the existing one, and garbage collects any expired router
// heartbeats.
func (s *Supervisor) heartbeat(routerIP string, heartbeat *function.Heartbeat) {
	// update the heartbeat for the router if it's newer than the existing one
	s.routerHeartbeats.Compute(routerIP, func(existing *function.Heartbeat, _ bool) (*function.Heartbeat, xsync.ComputeOp) {
		if existing == nil || heartbeat.Timestamp.After(existing.Timestamp) {
			return heartbeat, xsync.UpdateOp
		}
		return existing, xsync.CancelOp
	})

	// garbage collect expired router heartbeats
	s.routerHeartbeats.Range(func(routerIP string, heartbeat *function.Heartbeat) bool {
		if time.Since(heartbeat.Timestamp) > s.ctrl.config.HeartbeatTimeout {
			s.routerHeartbeats.Delete(routerIP)
		}
		return true
	})
}

// combinedHeartbeat aggregates heartbeats from all routers for this
// function, summing in-flight requests and using the most recent
// timestamp from either router heartbeats or instance assignments.
func (s *Supervisor) combinedHeartbeat(instances []*function.Instance) *function.Heartbeat {
	heartbeat := &function.Heartbeat{
		Function: s.fn,
	}

	s.routerHeartbeats.Range(func(_ string, routerHeartbeat *function.Heartbeat) bool {
		heartbeat.InFlightRequests += routerHeartbeat.InFlightRequests
		if heartbeat.Timestamp.Before(routerHeartbeat.Timestamp) {
			heartbeat.Timestamp = routerHeartbeat.Timestamp
		}
		return true
	})

	for _, instance := range instances {
		if heartbeat.Timestamp.Before(instance.AssignedAt) {
			heartbeat.Timestamp = instance.AssignedAt
		}
	}

	return heartbeat
}

// recordRecommendation adds a scaling recommendation to the stabilization window,
// prunes expired entries, and returns the maximum recommendation within the window.
func (s *Supervisor) recordRecommendation(desiredInstances int) Recommendation {
	now := time.Now()
	cutoff := now.Add(-s.ctrl.config.HPADownscaleStabilization)
	s.stabilizationWindow = append(s.stabilizationWindow, Recommendation{
		DesiredInstances: desiredInstances,
		Timestamp:        now,
	})
	s.stabilizationWindow = slices.DeleteFunc(s.stabilizationWindow, func(r Recommendation) bool {
		return r.Timestamp.Before(cutoff)
	})
	return slices.MaxFunc(s.stabilizationWindow, func(a, b Recommendation) int {
		return a.DesiredInstances - b.DesiredInstances
	})
}

// converge is the main scaling loop that calculates the desired number
// of instances based on the combined heartbeat and current instances,
// applies the stabilization window for scale-down decisions, and calls
// scale to reconcile the actual instance count.
//
// The order of operations is designed for efficiency:
//  1. Calculate scaling decision based on current instances
//  2. Check responsibility - return early if not responsible (still tracks recommendations)
//  3. Cleanup stuck instances (cheap - just deletes broken pods)
//  4. Execute scaling (scale up or down as needed)
//  5. Replace stale instances only when not scaling down
//
// During scale-down, stale instances are naturally deleted first since
// scale() deletes oldest instances first and stale instances are
// typically older (assigned before deployment).
func (s *Supervisor) converge(ctx context.Context) error {
	ctx, span := telemetry.Trace(ctx, "controller.supervisor.converge")
	defer span.End()

	s.mu.Lock()
	defer s.mu.Unlock()

	instances, err := s.ctrl.getInstances(ctx, s.fn)
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}

	// 1. Calculate scaling decision
	heartbeat := s.combinedHeartbeat(instances)
	ctx = telemetry.With(ctx, key.Function.Attr(s.fn), key.Heartbeat.Attr(heartbeat))

	scalingDecision := calculateDesiredInstances(ctx, s.ctrl.config, heartbeat, instances)
	maxRecommendation := s.recordRecommendation(scalingDecision.DesiredInstances)

	currentInstances := len(instances)
	isScalingDown := scalingDecision.DesiredInstances < currentInstances || scalingDecision.DesiredInstances == 0

	if isScalingDown {
		if time.Since(s.ctrl.startedAt) < s.ctrl.config.HPADownscaleStabilization {
			// the controller hasn't been running long enough to record
			// recommendations or receive heartbeats, so don't scale
			// anything down yet
			log.Debug(ctx, "skipping scale down because controller hasn't been running long enough", slog.Time("started_at", s.ctrl.startedAt))
			return nil
		}

		if scalingDecision.DesiredInstances == 0 {
			// we're scaling to 0, so stop when we're done scaling
			defer s.stop()
		} else {
			// we're scaling down, but not to 0, so scale down to the max recommended instances within the stabilization window
			// if we're already lower than the max recommended instances, then use the current number of instances (i.e. don't scale up)
			scalingDecision.DesiredInstances = min(currentInstances, maxRecommendation.DesiredInstances)
		}
	}

	// 2. Check responsibility
	responsibleIP := s.ctrl.ring.Get(s.fn)
	if responsibleIP != s.ctrl.config.PodIP {
		return nil
	}

	// 3. Cleanup stuck instances (cheap, run before scaling execution)
	instances = s.cleanupStuckInstances(ctx, instances)

	// 4. Execute scaling
	ready, unready, err := s.scaleWithoutLock(ctx, instances, scalingDecision)
	if err != nil {
		return err
	}

	// 5. Replace stale instances only when not scaling down.
	// During scale-down, stale instances are naturally deleted first
	// (oldest instances deleted first, and stale instances are typically older).
	// This avoids wasteful pod assignments when we're reducing capacity anyway.
	if !isScalingDown {
		s.replaceStaleInstances(ctx, ready, len(ready)+len(unready))
	}

	return nil
}

// scale executes the scaling decision by assigning new pods for
// scale-up or deleting pods for scale-down. It forwards requests to the
// responsible controller if this controller is not responsible for the
// function. For scale-down, it deletes unready instances first, then
// oldest ready instances.
func (s *Supervisor) scale(ctx context.Context, decision ScalingDecision) ([]*function.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	responsibleIP := s.ctrl.ring.Get(s.fn)
	if responsibleIP != s.ctrl.config.PodIP {
		log.Debug(ctx, "forwarding scale request to responsible controller", key.ResponsibleIP.Slog(responsibleIP))
		return s.ctrl.getControllerClient(responsibleIP).Scale(ctx, s.fn, decision.DesiredInstances, decision.Reason)
	}

	instances, err := s.ctrl.getInstances(ctx, s.fn)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	ready, _, err := s.scaleWithoutLock(ctx, instances, decision)
	return ready, err
}

// scaleWithoutLock is the internal implementation of scale that assumes
// the caller already holds s.mu. It executes the scaling decision
// without forwarding to another controller.
func (s *Supervisor) scaleWithoutLock(ctx context.Context, instances []*function.Instance, decision ScalingDecision) ([]*function.Instance, []*function.Instance, error) {
	ctx, span := telemetry.Trace(ctx, "controller.supervisor.scale")
	defer span.End()

	// split instances into ready and unready
	var unreadyInstances []*function.Instance
	readyInstances := slices.DeleteFunc(instances, func(instance *function.Instance) bool {
		if instance.ReadyAt.IsZero() {
			unreadyInstances = append(unreadyInstances, instance)
			return true
		}
		return false
	})

	ctx = log.With(ctx,
		key.ScalingDecision.Slog(decision),
		key.ReadyInstances.Slog(len(readyInstances)),
		key.UnreadyInstances.Slog(len(unreadyInstances)),
	)

	if s.fn.Scale.MaxInstances > 1 && decision.UnclampedDesiredInstances > decision.DesiredInstances {
		// this function is allowed to scale beyond a single instance
		// and it wanted to scale up higher than its max instances, so
		// let's log that for observability
		log.Info(ctx, "scaling decision was clamped")
	}

	if decision.DesiredInstances == len(readyInstances) && decision.DesiredInstances > len(unreadyInstances) {
		// we already have the desired number of ready instances and we
		// don't have extra unready instances, so there's nothing to do
		return readyInstances, unreadyInstances, nil
	}

	if decision.DesiredInstances > len(readyInstances) {
		// we need to scale up
		if len(readyInstances)+len(unreadyInstances) >= s.fn.Scale.MaxInstances+1 {
			// we have too many instances in total, so we can't scale up
			log.Info(ctx, "skipping scale up because function has too many instances")
			return readyInstances, unreadyInstances, nil
		}

		log.Info(ctx, "scaling function up")
		scaleUpsTotal.WithLabelValues(s.fn.Deployment).Add(float64(decision.DesiredInstances - len(readyInstances)))

		for range decision.DesiredInstances - len(readyInstances) {
			instance, err := s.ctrl.assignPod(ctx, s.fn)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to assign pod: %w", err)
			}
			readyInstances = append(readyInstances, instance)
		}
	} else if decision.Reason == ScalingReasonNoReadyInstances {
		// we were asked to scale up to 1 instance because there were no
		// ready instances at the time of the request, but now we have
		// at least 1 ready instance, so there's nothing to do
		return readyInstances, unreadyInstances, nil
	} else {
		// we either need to scale down or we're already at the desired number of instances but have extra unready instances
		log.Info(ctx, "scaling function down")
		scaleDownsTotal.WithLabelValues(s.fn.Deployment).Add(float64(len(readyInstances) + len(unreadyInstances) - decision.DesiredInstances))

		// delete all unready instances
		for _, unreadyInstance := range unreadyInstances {
			err := s.ctrl.deletePod(ctx, unreadyInstance.Namespace, unreadyInstance.Name, metav1.DeleteOptions{})
			if err != nil {
				return nil, nil, fmt.Errorf("failed to delete pod: %w", err)
			}
		}
		// all unready instances are deleted
		unreadyInstances = nil

		// sort ready instances by assigned at in descending order (newest first)
		slices.SortFunc(readyInstances, func(a, b *function.Instance) int { return b.AssignedAt.Compare(a.AssignedAt) })

		// iterate over ready instances in reverse order, deleting the oldest ones first
		for i := len(readyInstances) - 1; i >= decision.DesiredInstances; i-- {
			instance := readyInstances[i]
			err := s.ctrl.deletePod(ctx, instance.Namespace, instance.Name, metav1.DeleteOptions{})
			if err != nil {
				return nil, nil, fmt.Errorf("failed to delete pod: %w", err)
			}
			readyInstances = readyInstances[:i]
		}
	}

	return readyInstances, unreadyInstances, nil
}

// cleanupStuckInstances terminates instances that are stuck in the
// assigned state (not ready) for longer than FunctionAssignTimeout*2.
// This is a cheap operation (just deletes) and should be called before
// scaling execution to remove broken pods from consideration.
func (s *Supervisor) cleanupStuckInstances(ctx context.Context, instances []*function.Instance) []*function.Instance {
	return slices.DeleteFunc(instances, func(instance *function.Instance) bool {
		if instance.ReadyAt.IsZero() && time.Since(instance.AssignedAt) > s.ctrl.config.FunctionAssignTimeout*2 {
			ctx := log.With(ctx, key.Instance.Slog(instance))
			log.Warn(ctx, "terminating instance stuck in assigned state")
			err := s.ctrl.deletePod(ctx, instance.Namespace, instance.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Error(ctx, "failed to terminate instance stuck in assigned state", key.Error.Slog(err))
			}
			return true // Remove from slice
		}
		return false // Keep in slice
	})
}

// replaceStaleInstances detects instances running on replica sets
// that have been scaled to 0 (e.g., after a deployment update) and
// replaces them. For each stale instance, it first assigns a new pod
// from the active replica set (temporarily exceeding max instances if
// needed), then terminates the stale instance. This guarantees
// availability during deployment rollouts.
//
// This function should only be called when NOT scaling down, as
// scale-down naturally deletes oldest instances first (which are
// typically stale). Calling this during scale-down would create
// wasteful pod assignments.
//
// To prevent unbounded pod creation when deletePod fails repeatedly,
// this function will not assign new replacement pods if the total
// instance count reaches or exceeds maxInstances+1. In this case, the
// stale instance is kept and scale() will handle cleanup on subsequent
// iterations.
func (s *Supervisor) replaceStaleInstances(ctx context.Context, instances []*function.Instance, currentTotalInstances int) {
	namespaceLister, ok := s.ctrl.namespaceListers[s.fn.Namespace]
	if !ok {
		log.Warn(ctx, "namespace lister not found for function namespace", key.Namespace.Slog(s.fn.Namespace))
		return
	}

	for _, instance := range instances {
		replicaSet, err := namespaceLister.replicaSetLister.ReplicaSets(s.fn.Namespace).Get(instance.ReplicaSet)
		if err != nil {
			log.Warn(ctx, "failed to get replica set for instance", key.Error.Slog(err), key.Instance.Slog(instance))
			continue
		}

		if replicaSet.Status.Replicas > 0 {
			// Instance is on an active replica set, not stale
			continue
		}

		// Instance is stale - scale up first to ensure a replacement, then terminate
		ctx := log.With(ctx, key.Instance.Slog(instance))

		// Check if we're already at maxInstances+1 to prevent unbounded pod creation.
		// This can happen if a previous iteration assigned a replacement but failed
		// to delete the stale pod. In this case, keep the stale instance and let
		// scale() handle cleanup.
		if currentTotalInstances >= s.fn.Scale.MaxInstances+1 {
			log.Info(ctx, "skipping stale instance replacement, already at maxInstances+1")
			continue
		}

		// Acquire semaphore to limit concurrent stale instance replacements
		if err := s.ctrl.staleReplacementSem.Acquire(ctx, 1); err != nil {
			log.Warn(ctx, "failed to acquire stale replacement semaphore", key.Error.Slog(err))
			continue
		}

		log.Info(ctx, "replacing stale instance")

		// Assign a new pod from the active replica set (temporarily exceeding max instances).
		// Use a closure with defer to ensure the semaphore is released even if assignPod panics.
		var assignErr error
		func() {
			defer s.ctrl.staleReplacementSem.Release(1)
			_, assignErr = s.ctrl.assignPod(ctx, s.fn)
		}()
		if assignErr != nil {
			log.Error(ctx, "failed to assign replacement pod for stale instance", key.Error.Slog(assignErr))
			continue
		}

		// Increment the counter to account for the newly assigned pod. This ensures
		// we don't exceed maxInstances+1 when processing multiple stale instances.
		currentTotalInstances++

		// Terminate the stale instance now that a replacement has been assigned
		err = s.ctrl.deletePod(ctx, instance.Namespace, instance.Name, metav1.DeleteOptions{})
		if err != nil {
			log.Error(ctx, "failed to terminate stale instance", key.Error.Slog(err))
			// If deletion fails, currentTotalInstances remains incremented, preventing
			// further replacements until the stale pod is cleaned up.
		} else {
			// Delete succeeded, decrement counter to allow more replacements in this loop
			currentTotalInstances--
		}
	}
}

// getReadyInstance returns a ready instance for the function, scaling
// up if necessary. If excludeNames is provided, those instances are
// excluded from selection (unless all instances would be excluded).
func (s *Supervisor) getReadyInstance(ctx context.Context, excludeNames []string) (*function.Instance, error) {
	instances, err := s.ctrl.getReadyInstances(ctx, s.fn)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	telemetry.SetAttributes(ctx, attribute.Bool("has_instances", len(instances) > 0))

	for len(instances) == 0 {
		if instances, err = s.scale(ctx, ScalingDecision{
			DesiredInstances:          1,
			UnclampedDesiredInstances: 1,
			Reason:                    ScalingReasonNoReadyInstances,
		}); err != nil {
			return nil, fmt.Errorf("failed to scale function: %w", err)
		}
	}

	if len(instances) > s.fn.Scale.MaxInstances {
		// sort instances by assigned at in descending order (newest first)
		slices.SortFunc(instances, func(a, b *function.Instance) int { return b.AssignedAt.Compare(a.AssignedAt) })
		// keep the newest instances up to the max instances allowed for the function
		instances = instances[:s.fn.Scale.MaxInstances]
	}

	if len(excludeNames) > 0 {
		filtered := slices.DeleteFunc(slices.Clone(instances), func(inst *function.Instance) bool {
			return slices.Contains(excludeNames, inst.Name)
		})
		if len(filtered) > 0 {
			instances = filtered
		} else {
			log.Warn(ctx, "no instances available, all instances filtered out, reverting back to all instances")
		}
	}

	return instances[rand.Intn(len(instances))], nil
}

// Metric represents a metric type used for autoscaling decisions.
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

// calculateDesiredInstancesForMetric computes desired instances based on a single metric
func calculateDesiredInstancesForMetric(_ context.Context, cfg *Config, metric Metric, instances []*function.Instance) (int, float64) {
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
			return currentInstances, 0
		}

		if metric == MetricCPU && (instance.ReadyAt.IsZero() || time.Since(instance.ReadyAt) <= cfg.HPAInitialReadinessDelay) {
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
		return currentInstances, 0
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
		return currentInstances, 0
	}

	averageUsage := float64(totalUsage) / float64(len(instancesWithMetrics))
	usageRatio := averageUsage / float64(targetUsage)
	usageDiscrepancy := math.Abs(1.0 - usageRatio)
	desiredInstances := int(math.Ceil(float64(currentInstances) * usageRatio))

	if usageDiscrepancy <= cfg.HPATolerance+1e-10 { // add a small epsilon to avoid floating point errors
		// the average usage is within tolerance of the target utilization, so we should not scale
		return currentInstances, 0
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
			math.Abs(1.0-adjustedUsageRatio) <= cfg.HPATolerance+1e-10 {
			// the adjusted usage ratio is the opposite of the original
			// usage ratio, or the adjusted usage ratio is within
			// tolerance of the target utilization. either way, we
			// should noop and not scale
			return currentInstances, averageUsage
		}

		desiredInstances = int(math.Ceil(float64(currentInstances) * adjustedUsageRatio))
	}

	return desiredInstances, averageUsage
}

// calculateDesiredInstances computes desired instances based on multiple metrics
func calculateDesiredInstances(ctx context.Context, cfg *Config, heartbeat *function.Heartbeat, instances []*function.Instance) ScalingDecision {
	if time.Since(heartbeat.Timestamp) >= cfg.HeartbeatTimeout {
		return ScalingDecision{
			DesiredInstances:          0,
			UnclampedDesiredInstances: 0,
			Reason:                    ScalingReasonHeartbeatTimeout,
		}
	}

	maxDesiredInstances := 1 // we only scale to 0 from a heartbeat timeout, so we start at 1
	var scalingReason ScalingReason
	var scalingMetrics []ScalingMetric

	if heartbeat.Function.Scale.TargetInFlightRequests > 0 {
		desiredInstances := int(math.Ceil(float64(heartbeat.InFlightRequests) / float64(heartbeat.Function.Scale.TargetInFlightRequests)))
		averageUsage := float64(heartbeat.InFlightRequests) / float64(len(instances))
		scalingMetrics = append(scalingMetrics, ScalingMetric{Name: "in_flight_requests", Value: averageUsage})
		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
			scalingReason = ScalingReasonInFlightRequests
		}
	}

	if heartbeat.Function.Scale.TargetCPUUsageMilli > 0 {
		desiredInstances, averageUsage := calculateDesiredInstancesForMetric(ctx, cfg, MetricCPU, instances)
		scalingMetrics = append(scalingMetrics, ScalingMetric{Name: "cpu", Value: averageUsage})
		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
			scalingReason = ScalingReasonCPU
		}
	}

	if heartbeat.Function.Scale.TargetMemoryUsageMiB > 0 {
		desiredInstances, averageUsage := calculateDesiredInstancesForMetric(ctx, cfg, MetricMemory, instances)
		scalingMetrics = append(scalingMetrics, ScalingMetric{Name: "memory", Value: averageUsage})
		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
			scalingReason = ScalingReasonMemory
		}
	}

	// Apply min/max clamping
	minInstances := heartbeat.Function.Scale.MinInstances
	maxInstances := heartbeat.Function.Scale.MaxInstances
	clampedValue := min(max(maxDesiredInstances, minInstances), maxInstances)

	return ScalingDecision{
		DesiredInstances:          clampedValue,
		UnclampedDesiredInstances: maxDesiredInstances,
		Reason:                    scalingReason,
		Metrics:                   scalingMetrics,
	}
}

// ScalingDecision contains the inputs and result of one scaling loop for one tenant
type ScalingDecision struct {
	DesiredInstances          int
	UnclampedDesiredInstances int
	Reason                    ScalingReason
	Metrics                   []ScalingMetric
}

var _ slog.LogValuer = ScalingDecision{}

// LogValue implements slog.LogValuer for structured logging.
func (sd ScalingDecision) LogValue() slog.Value {
	var metricAttrs []slog.Attr
	for _, metric := range sd.Metrics {
		metricAttrs = append(metricAttrs, slog.Float64(metric.Name, metric.Value))
	}

	return slog.GroupValue(
		key.DesiredInstances.Slog(sd.DesiredInstances),
		key.UnclampedDesiredInstances.Slog(sd.UnclampedDesiredInstances),
		key.Reason.Slog(sd.Reason),
		slog.GroupAttrs("metrics", metricAttrs...),
	)
}

// ScalingReason represents the reason for a scaling decision.
// TODO: make this a type definition instead of a type alias so its more typesafe
type ScalingReason = string

const (
	ScalingReasonCPU              ScalingReason = "cpu"
	ScalingReasonHeartbeatTimeout ScalingReason = "heartbeat_timeout"
	ScalingReasonInFlightRequests ScalingReason = "in_flight_requests"
	ScalingReasonMemory           ScalingReason = "memory"
	ScalingReasonNoReadyInstances ScalingReason = "no ready instances"
	ScalingReasonUnknown          ScalingReason = "unknown"
)

// isValidScalingReason returns true if the given string is a known scaling reason.
func isValidScalingReason(reason string) bool {
	switch reason {
	case ScalingReasonCPU,
		ScalingReasonHeartbeatTimeout,
		ScalingReasonInFlightRequests,
		ScalingReasonMemory,
		ScalingReasonNoReadyInstances,
		ScalingReasonUnknown:
		return true
	default:
		return false
	}
}

// ScalingMetric represents an unclamped metric value for a specific metric observed for scaling decisions
type ScalingMetric struct {
	Name  string
	Value float64
}
