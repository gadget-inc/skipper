package controller

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/telemetry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Supervisor struct {
	mu                  sync.Mutex
	fn                  *function.Function
	ctrl                *Controller
	routerHeartbeats    map[string]*function.Heartbeat
	stabilizationWindow []Recommendation
}

func (s *Supervisor) heartbeat(routerIP string, heartbeat *function.Heartbeat) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.routerHeartbeats == nil {
		s.routerHeartbeats = make(map[string]*function.Heartbeat)
	}

	if s.routerHeartbeats[routerIP] == nil || heartbeat.Timestamp.After(s.routerHeartbeats[routerIP].Timestamp) {
		s.routerHeartbeats[routerIP] = heartbeat
	}

	// garbage collect expired router heartbeats
	for routerIP, heartbeat := range s.routerHeartbeats {
		if time.Since(heartbeat.Timestamp) > FlagHeartbeatTimeout.Value() {
			delete(s.routerHeartbeats, routerIP)
		}
	}
}

func (s *Supervisor) converge(ctx context.Context, instances []*function.Instance) ([]*function.Instance, error) {
	ctx, span := telemetry.Trace(ctx, "controller.supervisor.converge")
	defer span.End()

	heartbeat := new(function.Heartbeat)
	heartbeat.Function = s.fn

	for _, instance := range instances {
		if heartbeat.Timestamp.Before(instance.AssignedAt) {
			heartbeat.Timestamp = instance.AssignedAt
		}
	}

	for _, routerHeartbeat := range s.routerHeartbeats {
		heartbeat.InFlightRequests += routerHeartbeat.InFlightRequests
		if heartbeat.Timestamp.Before(routerHeartbeat.Timestamp) {
			heartbeat.Timestamp = routerHeartbeat.Timestamp
		}
	}

	ctx = log.With(ctx, key.Heartbeat.Field(heartbeat))
	ctx = telemetry.WithPropagatedAttributes(ctx, key.Heartbeat.Attributes(heartbeat)...)

	scalingDecision := calculateDesiredInstances(ctx, heartbeat, instances)

	now := time.Now()
	heartbeatTimeout := now.Add(-FlagHeartbeatTimeout.Value())
	s.stabilizationWindow = append(s.stabilizationWindow, Recommendation{DesiredInstances: scalingDecision.DesiredInstances, Timestamp: now})
	s.stabilizationWindow = slices.DeleteFunc(s.stabilizationWindow, func(r Recommendation) bool { return r.Timestamp.Before(heartbeatTimeout) })

	currentInstances := len(instances)
	if scalingDecision.DesiredInstances < currentInstances {
		// we're scaling down
		if time.Since(s.ctrl.startedAt) < FlagHPADownscaleStabilization.Value() {
			// the controller hasn't been running long enough to
			// record recommendations or receive heartbeats,
			// so don't scale anything down yet
			log.Debug(ctx, "skipping scale down because controller hasn't been running long enough", slog.Time("started_at", s.ctrl.startedAt))
			return instances, nil
		}

		if scalingDecision.DesiredInstances == 0 {
			// we're scaling to 0, so remove ourself from the supervisors map when we're done
			defer s.ctrl.supervisors.Delete(s.fn.Hash())
		} else {
			// we're scaling down, but not to 0, so scale down to the max recommended instances within the stabilization window
			// if we're already lower than the max recommended instances, then use the current number of instances (i.e. don't scale up)
			maxRecommendation := slices.MaxFunc(s.stabilizationWindow, func(a, b Recommendation) int { return a.DesiredInstances - b.DesiredInstances })
			scalingDecision.DesiredInstances = min(currentInstances, maxRecommendation.DesiredInstances)
		}
	}

	// we already hold the lock, so don't call scale()
	return s._scaleWithoutLock(ctx, scalingDecision)
}

func (s *Supervisor) scale(ctx context.Context, scalingDecision ScalingDecision) ([]*function.Instance, error) {
	_, span := telemetry.Trace(ctx, "controller.supervisor.scale.lock")
	s.mu.Lock()
	defer s.mu.Unlock()
	span.End()
	return s._scaleWithoutLock(ctx, scalingDecision)
}

func (s *Supervisor) _scaleWithoutLock(ctx context.Context, decision ScalingDecision) ([]*function.Instance, error) {
	ctx, span := telemetry.Trace(ctx, "controller.supervisor.scale")
	defer span.End()

	instances, err := s.ctrl.getInstances(s.fn)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

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
		key.ScalingDecision.Field(decision),
		key.ReadyInstances.Field(len(readyInstances)),
		key.UnreadyInstances.Field(len(unreadyInstances)),
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
		return readyInstances, nil
	}

	responsibleIP := s.ctrl.ring.Get(s.fn)
	if responsibleIP != FlagPodIP.Value() {
		log.Debug(ctx, "forwarding scale request to responsible controller", key.ResponsibleIP.Field(responsibleIP))
		return s.ctrl.getControllerClient(responsibleIP).Scale(ctx, s.fn, decision.DesiredInstances, decision.Reason)
	}

	if decision.DesiredInstances > len(readyInstances) {
		// we need to scale up
		if len(readyInstances)+len(unreadyInstances) >= s.fn.Scale.MaxInstances+1 {
			// we have too many instances in total, so we can't scale up
			log.Info(ctx, "skipping scale up because function has too many instances")
			return readyInstances, nil
		}

		log.Info(ctx, "scaling function up")
		scaleUpsTotal.WithLabelValues(s.fn.Deployment).Add(float64(decision.DesiredInstances - len(readyInstances)))

		for range decision.DesiredInstances - len(readyInstances) {
			instance, err := s.ctrl.assignPod(ctx, s.fn)
			if err != nil {
				return nil, fmt.Errorf("failed to assign pod: %w", err)
			}
			readyInstances = append(readyInstances, instance)
		}
	} else {
		// we either need to scale down or we're already at the desired number of instances but have extra unready instances
		log.Info(ctx, "scaling function down")
		scaleDownsTotal.WithLabelValues(s.fn.Deployment).Add(float64(len(readyInstances) + len(unreadyInstances) - decision.DesiredInstances))

		// delete all unready instances
		for _, unreadyInstance := range unreadyInstances {
			err := s.ctrl.kubernetes.CoreV1().Pods(unreadyInstance.Function.Namespace).Delete(ctx, unreadyInstance.Name, metav1.DeleteOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to delete pod: %w", err)
			}
		}

		// sort ready instances by assigned at in descending order (newest first)
		slices.SortFunc(readyInstances, func(a, b *function.Instance) int { return b.AssignedAt.Compare(a.AssignedAt) })

		// iterate over ready instances in reverse order, deleting the oldest ones first
		for i := len(readyInstances) - 1; i >= decision.DesiredInstances; i-- {
			instance := readyInstances[i]
			err := s.ctrl.kubernetes.CoreV1().Pods(instance.Function.Namespace).Delete(ctx, instance.Name, metav1.DeleteOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to delete pod: %w", err)
			}
			readyInstances = readyInstances[:i]
		}
	}

	return readyInstances, nil
}
