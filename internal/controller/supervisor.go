package controller

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"slices"
	"sync"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Supervisor manages scaling decisions for a single skipper. It tracks
// heartbeats from routers, maintains a stabilization window for
// scale-down decisions, and coordinates with the controller to adjust
// instance counts. Each Supervisor runs its own independent converge
// loop so that functions don't block each other.
type Supervisor struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	once                sync.Once
	mu                  sync.Mutex
	fn                  *skipper.Function
	ctrl                *Controller
	routerHeartbeats    *xsync.Map[string, *skipper.Heartbeat]
	stabilizationWindow []Recommendation
}

// supervisor returns the supervisor for the given function, creating it if necessary.
// If the controller has been started (ctrl.ctx is set), the supervisor's loop will
// be started automatically.
func (ctrl *Controller) supervisor(fn *skipper.Function) *Supervisor {
	supervisor, _ := ctrl.supervisors.LoadOrCompute(fn.Hash(), func() (*Supervisor, bool) {
		return &Supervisor{fn: fn, ctrl: ctrl, routerHeartbeats: xsync.NewMap[string, *skipper.Heartbeat]()}, false
	})
	supervisor.start(ctrl.ctx)
	return supervisor
}

// discoverSupervisors discovers functions in the given namespace by listing
// assigned pods and ensures supervisors exist for each unique skipper.
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

	seenFunctions := make(map[skipper.FunctionHash]bool)

	for _, pod := range pods {
		if !isPodRunning(pod) {
			continue // Skip terminating or non-running pods
		}
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
func (s *Supervisor) heartbeat(routerIP string, heartbeat *skipper.Heartbeat) {
	// update the heartbeat for the router if it's newer than the existing one
	s.routerHeartbeats.Compute(routerIP, func(existing *skipper.Heartbeat, _ bool) (*skipper.Heartbeat, xsync.ComputeOp) {
		if existing == nil || heartbeat.GetTimestamp().AsTime().After(existing.GetTimestamp().AsTime()) {
			return heartbeat, xsync.UpdateOp
		}
		return existing, xsync.CancelOp
	})

	// garbage collect expired router heartbeats
	s.routerHeartbeats.Range(func(routerIP string, heartbeat *skipper.Heartbeat) bool {
		if time.Since(heartbeat.GetTimestamp().AsTime()) > s.ctrl.config.HeartbeatTimeout {
			s.routerHeartbeats.Delete(routerIP)
		}
		return true
	})
}

// combinedHeartbeat aggregates heartbeats from all routers for this
// function, summing in-flight requests and using the most recent
// timestamp from either router heartbeats or instance assignments.
func (s *Supervisor) combinedHeartbeat(instances []*skipper.Instance) *skipper.Heartbeat {
	heartbeat := &skipper.Heartbeat{}
	heartbeat.SetFunction(s.fn)

	var latestTimestamp time.Time
	var totalInFlightRequests uint32

	s.routerHeartbeats.Range(func(_ string, routerHeartbeat *skipper.Heartbeat) bool {
		totalInFlightRequests += routerHeartbeat.GetInFlightRequests()
		routerTs := routerHeartbeat.GetTimestamp().AsTime()
		if latestTimestamp.Before(routerTs) {
			latestTimestamp = routerTs
		}
		return true
	})

	for _, instance := range instances {
		assignedAt := instance.GetAssignedAt().AsTime()
		if latestTimestamp.Before(assignedAt) {
			latestTimestamp = assignedAt
		}
	}

	heartbeat.SetInFlightRequests(totalInFlightRequests)
	if !latestTimestamp.IsZero() {
		heartbeat.SetTimestamp(timestamppb.New(latestTimestamp))
	}

	return heartbeat
}

// recordRecommendation adds a scaling recommendation to the stabilization window,
// prunes expired entries, and returns the maximum recommendation within the window.
func (s *Supervisor) recordRecommendation(desiredInstances uint32) Recommendation {
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
		return cmp.Compare(a.DesiredInstances, b.DesiredInstances)
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
	maxRecommendation := s.recordRecommendation(scalingDecision.GetDesiredInstances())

	currentInstances := uint32(len(instances))
	isScalingDown := scalingDecision.GetDesiredInstances() < currentInstances || scalingDecision.GetDesiredInstances() == 0

	if isScalingDown {
		// Use the maximum of HPADownscaleStabilization and HeartbeatTimeout as the protection period.
		// This ensures new controllers don't scale down until they've had enough time to:
		// 1. Record recommendations for the stabilization window (HPADownscaleStabilization)
		// 2. Receive heartbeats from routers (HeartbeatTimeout) - without this, functions with
		//    instances assigned longer than HeartbeatTimeout ago would immediately trigger
		//    heartbeat_timeout scale-to-zero when a new controller starts with empty heartbeat state
		protectionPeriod := max(s.ctrl.config.HPADownscaleStabilization, s.ctrl.config.HeartbeatTimeout)
		if time.Since(s.ctrl.startedAt) < protectionPeriod {
			log.Debug(ctx, "skipping scale down because controller hasn't been running long enough", slog.Time("started_at", s.ctrl.startedAt))
			return nil
		}

		if scalingDecision.GetDesiredInstances() == 0 {
			// we're scaling to 0, so stop when we're done scaling
			defer s.stop()
		} else {
			// we're scaling down, but not to 0, so scale down to the max recommended instances within the stabilization window
			// if we're already lower than the max recommended instances, then use the current number of instances (i.e. don't scale up)
			scalingDecision.SetDesiredInstances(min(currentInstances, maxRecommendation.DesiredInstances))
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
// skipper. For scale-down, it deletes unready instances first, then
// oldest ready instances.
func (s *Supervisor) scale(ctx context.Context, decision *skipper.ScaleDecision) ([]*skipper.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	responsibleIP := s.ctrl.ring.Get(s.fn)
	if responsibleIP != s.ctrl.config.PodIP {
		log.Debug(ctx, "forwarding scale request to responsible controller", key.ResponsibleIP.Slog(responsibleIP))
		return s.ctrl.getControllerClient(responsibleIP).Scale(ctx, s.fn, decision.GetDesiredInstances(), decision.GetReason())
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
func (s *Supervisor) scaleWithoutLock(ctx context.Context, instances []*skipper.Instance, decision *skipper.ScaleDecision) ([]*skipper.Instance, []*skipper.Instance, error) {
	ctx, span := telemetry.Trace(ctx, "controller.supervisor.scale")
	defer span.End()

	// split instances into ready and unready
	var unreadyInstances []*skipper.Instance
	readyInstances := slices.DeleteFunc(instances, func(instance *skipper.Instance) bool {
		if !instance.HasReadyAt() {
			unreadyInstances = append(unreadyInstances, instance)
			return true
		}
		return false
	})

	ctx = log.With(ctx,
		key.ScaleDecision.Slog(decision),
		key.ReadyInstances.Slog(len(readyInstances)),
		key.UnreadyInstances.Slog(len(unreadyInstances)),
	)

	if s.fn.GetScale().GetMaxInstances() > 1 && decision.GetUnclampedDesiredInstances() > decision.GetDesiredInstances() {
		// this function is allowed to scale beyond a single instance
		// and it wanted to scale up higher than its max instances, so
		// let's log that for observability
		log.Info(ctx, "scaling decision was clamped")
	}

	ready := uint32(len(readyInstances))
	unready := uint32(len(unreadyInstances))
	desired := decision.GetDesiredInstances()
	total := ready + unready

	if desired == ready && desired > unready {
		// we already have the desired number of ready instances and we
		// don't have extra unready instances, so there's nothing to do
		return readyInstances, unreadyInstances, nil
	}

	if desired > ready {
		// we need to scale up
		if total >= s.fn.GetScale().GetMaxInstances()+1 {
			// we have too many instances in total, so we can't scale up
			log.Info(ctx, "skipping scale up because function has too many instances")
			return readyInstances, unreadyInstances, nil
		}

		log.Info(ctx, "scaling function up")
		scaleUpsTotal.WithLabelValues(s.fn.GetDeployment()).Add(float64(desired - ready))

		for range desired - ready {
			instance, err := s.ctrl.assignPod(ctx, s.fn)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to assign pod: %w", err)
			}
			readyInstances = append(readyInstances, instance)
		}
	} else if decision.GetReason() == skipper.ScaleReason_SCALE_REASON_NO_READY_INSTANCES {
		// we were asked to scale up to 1 instance because there were no
		// ready instances at the time of the request, but now we have
		// at least 1 ready instance, so there's nothing to do
		return readyInstances, unreadyInstances, nil
	} else {
		// we either need to scale down or we're already at the desired number of instances but have extra unready instances
		log.Info(ctx, "scaling function down")
		scaleDownsTotal.WithLabelValues(s.fn.GetDeployment()).Add(float64(total - desired))

		// delete all unready instances
		for _, unreadyInstance := range unreadyInstances {
			err := s.ctrl.deletePod(ctx, unreadyInstance.GetFunction().GetNamespace(), unreadyInstance.GetName(), metav1.DeleteOptions{})
			if err != nil {
				return nil, nil, fmt.Errorf("failed to delete pod: %w", err)
			}
		}
		unreadyInstances = nil

		// sort ready instances by assigned at in descending order (newest first)
		slices.SortFunc(readyInstances, func(a, b *skipper.Instance) int {
			return b.GetAssignedAt().AsTime().Compare(a.GetAssignedAt().AsTime())
		})

		// delete oldest ready instances (they're at the end after sorting newest-first)
		for toDelete := ready - desired; toDelete > 0; toDelete-- {
			instance := readyInstances[len(readyInstances)-1]
			err := s.ctrl.deletePod(ctx, instance.GetFunction().GetNamespace(), instance.GetName(), metav1.DeleteOptions{})
			if err != nil {
				return nil, nil, fmt.Errorf("failed to delete pod: %w", err)
			}
			readyInstances = readyInstances[:len(readyInstances)-1]
		}
	}

	return readyInstances, unreadyInstances, nil
}

// cleanupStuckInstances terminates instances that are stuck in the
// assigned state (not ready) for longer than FunctionAssignTimeout*2.
// This is a cheap operation (just deletes) and should be called before
// scaling execution to remove broken pods from consideration.
func (s *Supervisor) cleanupStuckInstances(ctx context.Context, instances []*skipper.Instance) []*skipper.Instance {
	return slices.DeleteFunc(instances, func(instance *skipper.Instance) bool {
		if !instance.HasReadyAt() && time.Since(instance.GetAssignedAt().AsTime()) > s.ctrl.config.FunctionAssignTimeout*2 {
			ctx := log.With(ctx, key.Instance.Slog(instance))
			log.Warn(ctx, "terminating instance stuck in assigned state")
			err := s.ctrl.deletePod(ctx, instance.GetFunction().GetNamespace(), instance.GetName(), metav1.DeleteOptions{})
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
func (s *Supervisor) replaceStaleInstances(ctx context.Context, instances []*skipper.Instance, currentTotalInstances int) {
	namespaceLister, ok := s.ctrl.namespaceListers[s.fn.GetNamespace()]
	if !ok {
		log.Warn(ctx, "namespace lister not found for function namespace", key.Namespace.Slog(s.fn.GetNamespace()))
		return
	}

	for _, instance := range instances {
		replicaSet, err := namespaceLister.replicaSetLister.ReplicaSets(s.fn.GetNamespace()).Get(instance.GetReplicaSet())
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
		if currentTotalInstances >= int(s.fn.GetScale().GetMaxInstances())+1 {
			log.Info(ctx, "skipping stale instance replacement, already at maxInstances+1")
			continue
		}

		// Try to acquire semaphore to limit concurrent stale instance replacements.
		// Use TryAcquire to avoid blocking the supervisor's converge loop - stale
		// replacement is not time-critical and will be retried on subsequent iterations.
		if !s.ctrl.staleReplacementSem.TryAcquire(1) {
			log.Debug(ctx, "stale replacement semaphore at capacity, deferring")
			return
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
		err = s.ctrl.deletePod(ctx, instance.GetFunction().GetNamespace(), instance.GetName(), metav1.DeleteOptions{})
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
func (s *Supervisor) getReadyInstance(ctx context.Context, excludeNames []string) (*skipper.Instance, error) {
	instances, err := s.ctrl.getReadyInstances(ctx, s.fn)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	telemetry.SetAttributes(ctx, attribute.Bool("has_instances", len(instances) > 0))

	for len(instances) == 0 {
		decision := &skipper.ScaleDecision{}
		decision.SetDesiredInstances(1)
		decision.SetUnclampedDesiredInstances(1)
		decision.SetReason(skipper.ScaleReason_SCALE_REASON_NO_READY_INSTANCES)
		if instances, err = s.scale(ctx, decision); err != nil {
			return nil, fmt.Errorf("failed to scale function: %w", err)
		}
	}

	if len(instances) > int(s.fn.GetScale().GetMaxInstances()) {
		// sort instances by assigned at in descending order (newest first)
		slices.SortFunc(instances, func(a, b *skipper.Instance) int {
			return b.GetAssignedAt().AsTime().Compare(a.GetAssignedAt().AsTime())
		})
		// keep the newest instances up to the max instances allowed for the function
		instances = instances[:s.fn.GetScale().GetMaxInstances()]
	}

	if len(excludeNames) > 0 {
		filtered := slices.DeleteFunc(slices.Clone(instances), func(inst *skipper.Instance) bool {
			return slices.Contains(excludeNames, inst.GetName())
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
	DesiredInstances uint32
	Timestamp        time.Time
}

// calculateDesiredInstancesForMetric computes desired instances based on a single metric
func calculateDesiredInstancesForMetric(_ context.Context, cfg *Config, metric Metric, instances []*skipper.Instance) (int, float64) {
	currentInstances := len(instances)
	var instancesWithMetrics []*skipper.Instance
	var instancesWithoutMetrics []*skipper.Instance

	for _, instance := range instances {
		var usage uint32
		switch metric {
		case MetricCPU:
			usage = instance.GetCpuUsageMilli()
		case MetricMemory:
			usage = instance.GetMemoryUsageMib()
		default:
			return currentInstances, 0
		}

		if metric == MetricCPU && (!instance.HasReadyAt() || time.Since(instance.GetReadyAt().AsTime()) <= cfg.HPAInitialReadinessDelay) {
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

	var targetUsage uint32
	var totalUsage uint32
	for _, instance := range instancesWithMetrics {
		// accumulate total usage and keep track of target usage (they should all be identical)
		switch metric {
		case MetricCPU:
			targetUsage = instance.GetFunction().GetScale().GetTargetCpuUsageMilli()
			totalUsage += instance.GetCpuUsageMilli()
		case MetricMemory:
			targetUsage = instance.GetFunction().GetScale().GetTargetMemoryUsageMib()
			totalUsage += instance.GetMemoryUsageMib()
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
			adjustedTotalUsage += uint32(len(instancesWithoutMetrics)) * targetUsage
		} else { //nolint:staticcheck
			// we wanted to scale up, so we assume that instances without metrics are consuming 0% of the target usage
			// so no adjustment is needed
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
func calculateDesiredInstances(ctx context.Context, cfg *Config, heartbeat *skipper.Heartbeat, instances []*skipper.Instance) *skipper.ScaleDecision {
	if !heartbeat.HasTimestamp() || time.Since(heartbeat.GetTimestamp().AsTime()) >= cfg.HeartbeatTimeout {
		decision := &skipper.ScaleDecision{}
		decision.SetDesiredInstances(0)
		decision.SetUnclampedDesiredInstances(0)
		decision.SetReason(skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT)
		return decision
	}

	maxDesiredInstances := 1 // we only scale to 0 from a heartbeat timeout, so we start at 1
	var scaleReason skipper.ScaleReason
	var scaleMetrics []*skipper.ScaleMetric

	scale := heartbeat.GetFunction().GetScale()

	if scale.GetTargetInFlightRequests() > 0 {
		desiredInstances := int(math.Ceil(float64(heartbeat.GetInFlightRequests()) / float64(scale.GetTargetInFlightRequests())))
		averageUsage := float64(heartbeat.GetInFlightRequests()) / float64(len(instances))
		metric := &skipper.ScaleMetric{}
		metric.SetName("in_flight_requests")
		metric.SetValue(averageUsage)
		scaleMetrics = append(scaleMetrics, metric)
		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
			scaleReason = skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS
		}
	}

	if scale.GetTargetCpuUsageMilli() > 0 {
		desiredInstances, averageUsage := calculateDesiredInstancesForMetric(ctx, cfg, MetricCPU, instances)
		metric := &skipper.ScaleMetric{}
		metric.SetName("cpu")
		metric.SetValue(averageUsage)
		scaleMetrics = append(scaleMetrics, metric)
		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
			scaleReason = skipper.ScaleReason_SCALE_REASON_CPU
		}
	}

	if scale.GetTargetMemoryUsageMib() > 0 {
		desiredInstances, averageUsage := calculateDesiredInstancesForMetric(ctx, cfg, MetricMemory, instances)
		metric := &skipper.ScaleMetric{}
		metric.SetName("memory")
		metric.SetValue(averageUsage)
		scaleMetrics = append(scaleMetrics, metric)
		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
			scaleReason = skipper.ScaleReason_SCALE_REASON_MEMORY
		}
	}

	// Apply min/max clamping
	minInstances := scale.GetMinInstances()
	maxInstances := scale.GetMaxInstances()
	clampedValue := min(max(uint32(maxDesiredInstances), minInstances), maxInstances)

	decision := &skipper.ScaleDecision{}
	decision.SetDesiredInstances(clampedValue)
	decision.SetUnclampedDesiredInstances(uint32(maxDesiredInstances))
	decision.SetReason(scaleReason)
	decision.SetMetrics(scaleMetrics)
	return decision
}
