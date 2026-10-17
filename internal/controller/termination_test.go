package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// terminationAssignedAt and terminationKilledAt mirror a production OOM
// kill: the build was assigned a pod, then killed 52 seconds later.
var (
	terminationAssignedAt = time.Date(2026, 10, 15, 14, 24, 0, 0, time.UTC)
	terminationKilledAt   = metav1.NewTime(terminationAssignedAt.Add(52 * time.Second))
)

// newTerminationTestPod returns a running single-container pod with a 6Gi
// memory limit, modified by mutate.
func newTerminationTestPod(mutate func(pod *v1.Pod)) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "operation-runner-abc", Namespace: "fn"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Name: "runner",
				Resources: v1.ResourceRequirements{
					Limits: v1.ResourceList{v1.ResourceMemory: resource.MustParse("6Gi")},
				},
			}},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			ContainerStatuses: []v1.ContainerStatus{{
				Name:  "runner",
				State: v1.ContainerState{Running: &v1.ContainerStateRunning{}},
			}},
		},
	}
	mutate(pod)
	return pod
}

func TestDescribeTermination(t *testing.T) {
	t.Parallel()

	oomKilled := &v1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: terminationKilledAt}
	wantOOM := skipper.InstanceTermination_builder{
		Reason:           skipper.TerminationReason_TERMINATION_REASON_OOM_KILLED.Enum(),
		Container:        new("runner"),
		ExitCode:         new(int32(137)),
		MemoryLimitBytes: new(int64(6 << 30)),
	}.Build()

	testCases := []struct {
		name   string
		mutate func(pod *v1.Pod)
		want   *skipper.InstanceTermination // nil means "no sign of stopping yet"
	}{
		{
			name:   "running pod",
			mutate: func(pod *v1.Pod) {},
			want:   nil,
		},
		{
			name: "OOM kill after the container restarted in place",
			mutate: func(pod *v1.Pod) {
				pod.Status.ContainerStatuses[0].RestartCount = 1
				pod.Status.ContainerStatuses[0].LastTerminationState.Terminated = oomKilled
			},
			want: wantOOM,
		},
		{
			name: "OOM kill before the container restarted",
			mutate: func(pod *v1.Pod) {
				pod.Status.ContainerStatuses[0].State = v1.ContainerState{Terminated: oomKilled}
			},
			want: wantOOM,
		},
		{
			name: "OOM kill from before the pod was assigned",
			mutate: func(pod *v1.Pod) {
				pod.Status.ContainerStatuses[0].LastTerminationState.Terminated = &v1.ContainerStateTerminated{
					Reason: "OOMKilled", ExitCode: 137, FinishedAt: metav1.NewTime(terminationAssignedAt.Add(-time.Minute)),
				}
			},
			want: nil,
		},
		{
			name: "container exited",
			mutate: func(pod *v1.Pod) {
				pod.Status.ContainerStatuses[0].State = v1.ContainerState{Terminated: &v1.ContainerStateTerminated{
					Reason: "Error", ExitCode: 1, Message: "segmentation fault", FinishedAt: terminationKilledAt,
				}}
			},
			want: skipper.InstanceTermination_builder{
				Reason:    skipper.TerminationReason_TERMINATION_REASON_CONTAINER_EXITED.Enum(),
				Container: new("runner"),
				ExitCode:  new(int32(1)),
				Message:   new("segmentation fault"),
			}.Build(),
		},
		{
			name: "pod deleted by skipper after a heartbeat timeout",
			mutate: func(pod *v1.Pod) {
				pod.DeletionTimestamp = &terminationKilledAt
				pod.Annotations = map[string]string{key.TerminatedBy.Label: skipper.TerminatedByHeartbeatTimeout}
			},
			want: skipper.InstanceTermination_builder{
				Reason:       skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED.Enum(),
				TerminatedBy: new(skipper.TerminatedByHeartbeatTimeout),
			}.Build(),
		},
		{
			name: "pod deleted by someone else",
			mutate: func(pod *v1.Pod) {
				pod.DeletionTimestamp = &terminationKilledAt
			},
			want: skipper.InstanceTermination_builder{
				Reason: skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED.Enum(),
			}.Build(),
		},
		{
			name: "pod evicted through the eviction API",
			mutate: func(pod *v1.Pod) {
				pod.DeletionTimestamp = &terminationKilledAt
				pod.Status.Conditions = []v1.PodCondition{{
					Type: v1.DisruptionTarget, Status: v1.ConditionTrue,
					Reason: "EvictionByEvictionAPI", Message: "Eviction API: evicting",
				}}
			},
			want: skipper.InstanceTermination_builder{
				Reason:       skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED.Enum(),
				TerminatedBy: new("EvictionByEvictionAPI"),
				Message:      new("Eviction API: evicting"),
			}.Build(),
		},
		{
			// The kubelet fails the pod instead of deleting it, so there is no
			// deletion timestamp, and the container's exit is a consequence.
			name: "pod evicted by the kubelet under node memory pressure",
			mutate: func(pod *v1.Pod) {
				pod.Status.Phase = v1.PodFailed
				pod.Status.Conditions = []v1.PodCondition{{
					Type: v1.DisruptionTarget, Status: v1.ConditionTrue,
					Reason: v1.PodReasonTerminationByKubelet, Message: "The node was low on resource: memory.",
				}}
				pod.Status.ContainerStatuses[0].State = v1.ContainerState{Terminated: &v1.ContainerStateTerminated{
					Reason: "Error", ExitCode: 137, FinishedAt: terminationKilledAt,
				}}
			},
			want: skipper.InstanceTermination_builder{
				Reason:       skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED.Enum(),
				TerminatedBy: new(v1.PodReasonTerminationByKubelet),
				Message:      new("The node was low on resource: memory."),
			}.Build(),
		},
		{
			name: "OOM kill wins over a later deletion",
			mutate: func(pod *v1.Pod) {
				pod.DeletionTimestamp = &terminationKilledAt
				pod.Annotations = map[string]string{key.TerminatedBy.Label: skipper.TerminatedByHeartbeatTimeout}
				pod.Status.ContainerStatuses[0].LastTerminationState.Terminated = oomKilled
			},
			want: wantOOM,
		},
		{
			name: "disruption condition that is not true",
			mutate: func(pod *v1.Pod) {
				pod.Status.Conditions = []v1.PodCondition{{Type: v1.DisruptionTarget, Status: v1.ConditionFalse}}
			},
			want: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := describeTermination(newTerminationTestPod(tc.mutate), terminationAssignedAt)
			if tc.want == nil {
				assert.Assert(t, got == nil, "expected no termination, got %v", got)
				return
			}
			assert.Assert(t, proto.Equal(got, tc.want), "got %v, want %v", got, tc.want)
		})
	}
}

func TestAwaitTermination(t *testing.T) {
	t.Parallel()

	newInstance := func(pod *v1.Pod) *skipper.Instance {
		return skipper.Instance_builder{
			Function:   skipper.Function_builder{Namespace: new(pod.Namespace)}.Build(),
			Name:       new(pod.Name),
			AssignedAt: timestamppb.New(terminationAssignedAt),
		}.Build()
	}

	t.Run("waits for the kubelet to post the reason", func(t *testing.T) {
		t.Parallel()

		pod := newTerminationTestPod(func(*v1.Pod) {})
		client := fake.NewClientset(pod)
		ctrl := New(testConfig(), nil, client, nil)

		// The router sees EOF before the kubelet notices the dead container.
		go func() {
			time.Sleep(300 * time.Millisecond)
			killed := pod.DeepCopy()
			killed.Status.ContainerStatuses[0].LastTerminationState.Terminated = &v1.ContainerStateTerminated{
				Reason: "OOMKilled", ExitCode: 137, FinishedAt: terminationKilledAt,
			}
			_, err := client.CoreV1().Pods(pod.Namespace).UpdateStatus(context.Background(), killed, metav1.UpdateOptions{})
			assert.Check(t, err)
		}()

		termination := ctrl.awaitTermination(t.Context(), newInstance(pod))
		assert.Equal(t, termination.GetReason(), skipper.TerminationReason_TERMINATION_REASON_OOM_KILLED)
		assert.Equal(t, termination.GetMemoryLimitBytes(), int64(6<<30))
	})

	t.Run("pod already gone", func(t *testing.T) {
		t.Parallel()

		pod := newTerminationTestPod(func(*v1.Pod) {})
		ctrl := New(testConfig(), nil, fake.NewClientset(), nil)

		termination := ctrl.awaitTermination(t.Context(), newInstance(pod))
		assert.Equal(t, termination.GetReason(), skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED)
		assert.Equal(t, termination.GetMessage(), "already deleted when the controller checked")
	})

	t.Run("gives up after the wait", func(t *testing.T) {
		t.Parallel()

		pod := newTerminationTestPod(func(*v1.Pod) {})
		cfg := testConfig()
		cfg.InstanceTerminationWait = 300 * time.Millisecond
		ctrl := New(cfg, nil, fake.NewClientset(pod), nil)

		start := time.Now()
		termination := ctrl.awaitTermination(t.Context(), newInstance(pod))
		elapsed := time.Since(start)

		assert.Equal(t, termination.GetReason(), skipper.TerminationReason_TERMINATION_REASON_UNSPECIFIED)
		assert.Assert(t, elapsed >= cfg.InstanceTerminationWait, "returned after %s, before the %s wait", elapsed, cfg.InstanceTerminationWait)
		assert.Assert(t, elapsed < 2*time.Second, "took %s, the wait should bound it", elapsed)
	})
}

// TestReleaseInstanceReportsTermination verifies that a ReleaseInstance
// request with report_termination returns the OOM kill, records it in the
// event log, and still deletes the pod.
func TestReleaseInstanceReportsTermination(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fn.SetOneshot(true)

	pod := fixture.NewAssignedPod(t, fn, nil)
	pod.Spec.Containers[0].Resources.Limits = v1.ResourceList{v1.ResourceMemory: resource.MustParse("6Gi")}
	pod.Status.ContainerStatuses = []v1.ContainerStatus{{
		Name:                 "main",
		RestartCount:         1,
		State:                v1.ContainerState{Running: &v1.ContainerStateRunning{}},
		LastTerminationState: v1.ContainerState{Terminated: &v1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: metav1.Now()}},
	}}

	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod(), pod)
	ctrl := New(testConfig(), nil, fakeKubernetes, nil)
	assert.NilError(t, ctrl.startInformers(ctx))

	inst := skipper.Instance_builder{
		Function: fn,
		Name:     new(pod.Name),
		// assigned_at is second-precision, like the pod annotation
		AssignedAt: timestamppb.New(time.Now().Add(-time.Minute).Truncate(time.Second)),
	}.Build()

	resp, err := NewServer(ctrl).ReleaseInstance(ctx, skipper.ReleaseInstanceRequest_builder{
		Instance:          inst,
		ReportTermination: new(true),
	}.Build())
	assert.NilError(t, err)

	want := skipper.InstanceTermination_builder{
		Reason:           skipper.TerminationReason_TERMINATION_REASON_OOM_KILLED.Enum(),
		Container:        new("main"),
		ExitCode:         new(int32(137)),
		MemoryLimitBytes: new(int64(6 << 30)),
	}.Build()
	assert.Assert(t, proto.Equal(resp.GetTermination(), want), "got %v, want %v", resp.GetTermination(), want)

	_, err = fakeKubernetes.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	assert.Assert(t, apierrors.IsNotFound(err), "released pod should be deleted, got err %v", err)

	events := ctrl.events.snapshot()
	assert.Assert(t, len(events) == 1, "expected 1 event, got %d", len(events))
	assert.Equal(t, events[0].GetType(), skipper.EventType_EVENT_TYPE_POD_DELETED)
	assert.Equal(t, events[0].GetSeverity(), skipper.EventSeverity_EVENT_SEVERITY_WARN)
	assert.Equal(t, events[0].GetMessage(), "released failed pod "+pod.Name+`: container "main" ran out of memory and was killed at its 6Gi limit (exit code 137)`)
}
