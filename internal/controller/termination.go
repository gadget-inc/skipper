package controller

import (
	"context"
	"strconv"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/timer"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// terminationPollInterval is how often awaitTermination re-reads a failed
// oneshot pod. The kubelet notices a dead container on its next relist
// (every 1s) and then posts the pod status, so the reason usually shows up
// within a few polls.
const terminationPollInterval = 250 * time.Millisecond

// awaitTermination reads the pod behind inst until Kubernetes says why it
// stopped, or until InstanceTerminationWait elapses. It reads from the API
// server rather than the informer cache because deletePod evicts the cache
// entry, which would make a pod skipper just deleted look like it never
// existed. The returned termination has an unspecified reason when nothing
// turned up in time.
func (ctrl *Controller) awaitTermination(ctx context.Context, inst *skipper.Instance) *skipper.InstanceTermination {
	ctx, cancel := context.WithTimeout(ctx, ctrl.config.InstanceTerminationWait)
	defer cancel()

	pods := ctrl.kubernetes.CoreV1().Pods(inst.GetFunction().GetNamespace())
	termination, _ := timer.Poll(ctx, terminationPollInterval, func(ctx context.Context) (*skipper.InstanceTermination, error) {
		pod, err := pods.Get(ctx, inst.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return skipper.InstanceTermination_builder{
				Reason:  skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED.Enum(),
				Message: new("already deleted when the controller checked"),
			}.Build(), nil
		}
		if err != nil {
			log.Warn(ctx, "failed to get pod to describe its termination", key.Error.Slog(err))
			if apierrors.IsForbidden(err) {
				return nil, err // retrying won't grant the permission
			}
			return nil, nil // keep polling until the deadline
		}
		return describeTermination(pod, inst.GetAssignedAt().AsTime()), nil
	})
	if termination == nil {
		return &skipper.InstanceTermination{}
	}
	return termination
}

// describeTermination says why pod stopped serving the instance assigned at
// assignedAt, or returns nil if the pod shows no sign of stopping yet.
//
// An OOM kill wins over everything else because it is a root cause. A pod
// being deleted or evicted wins over a container exit because deleting a
// pod is what makes its containers exit. Container terminations that
// finished before assignedAt are ignored: those are restarts from before
// the pod took this request.
func describeTermination(pod *v1.Pod, assignedAt time.Time) *skipper.InstanceTermination {
	var exited *skipper.InstanceTermination
	for _, status := range pod.Status.ContainerStatuses {
		// Deployment pods restart their containers in place, so a container
		// killed seconds ago is usually already running again, with the kill
		// recorded in LastTerminationState.
		terminated := status.State.Terminated
		if terminated == nil {
			terminated = status.LastTerminationState.Terminated
		}
		if terminated == nil || (!terminated.FinishedAt.IsZero() && terminated.FinishedAt.Time.Before(assignedAt)) {
			continue
		}

		termination := &skipper.InstanceTermination{}
		termination.SetContainer(status.Name)
		termination.SetExitCode(terminated.ExitCode)
		if terminated.Reason == "OOMKilled" {
			termination.SetReason(skipper.TerminationReason_TERMINATION_REASON_OOM_KILLED)
			if limit := containerMemoryLimit(pod, status.Name); limit > 0 {
				termination.SetMemoryLimitBytes(limit)
			}
			return termination
		}
		if exited == nil {
			termination.SetReason(skipper.TerminationReason_TERMINATION_REASON_CONTAINER_EXITED)
			if terminated.Message != "" {
				termination.SetMessage(terminated.Message)
			}
			exited = termination
		}
	}

	// Evictions (API, kubelet node pressure, preemption, taint manager) set
	// the DisruptionTarget condition; the kubelet's own evictions don't set
	// a deletion timestamp.
	disruption := podCondition(pod, v1.DisruptionTarget)
	if pod.DeletionTimestamp != nil || disruption != nil {
		termination := &skipper.InstanceTermination{}
		termination.SetReason(skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED)
		if by := pod.Annotations[key.TerminatedBy.Label]; by != "" {
			termination.SetTerminatedBy(by)
		} else if disruption != nil {
			termination.SetTerminatedBy(disruption.Reason)
			if disruption.Message != "" {
				termination.SetMessage(disruption.Message)
			}
		}
		return termination
	}

	return exited
}

// markTerminatedBy records on the pod who is about to delete it, so that
// whichever controller handles the router's ReleaseInstance can name the
// cause. Failure only costs the name, so it is logged and not returned.
func (ctrl *Controller) markTerminatedBy(ctx context.Context, inst *skipper.Instance, terminatedBy string) {
	patches := []byte(`[{ "op": "add", "path": "` + key.TerminatedBy.PatchAnnotation + `", "value": ` + strconv.Quote(terminatedBy) + ` }]`)
	_, err := ctrl.patchPod(ctx, inst.GetFunction().GetNamespace(), inst.GetName(), types.JSONPatchType, patches, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil && !apierrors.IsNotFound(err) {
		log.Warn(ctx, "failed to record who terminated the pod", key.Error.Slog(err), skipper.InstanceKey.Slog(inst))
	}
}

func containerMemoryLimit(pod *v1.Pod, name string) int64 {
	for _, container := range pod.Spec.Containers {
		if container.Name == name {
			return container.Resources.Limits.Memory().Value()
		}
	}
	return 0
}

func podCondition(pod *v1.Pod, conditionType v1.PodConditionType) *v1.PodCondition {
	for i := range pod.Status.Conditions {
		if pod.Status.Conditions[i].Type == conditionType && pod.Status.Conditions[i].Status == v1.ConditionTrue {
			return &pod.Status.Conditions[i]
		}
	}
	return nil
}
