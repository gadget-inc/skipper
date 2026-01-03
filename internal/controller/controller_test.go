package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/function"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetReadyInstances(t *testing.T) {
	type testState struct {
		fn             function.Function
		fakeKubernetes *fake.Clientset
		instances      []*function.Instance
	}

	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "one",
			setup: func(t *testing.T, state *testState) {
				err := state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1)
			},
		},
		{
			name: "many",
			setup: func(t *testing.T, state *testState) {
				err := state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				assert.NilError(t, err)

				err = state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 2)
			},
		},
		{
			name: "deleted",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			name: "failed",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Status.Phase = v1.PodFailed
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			name: "no pod IP",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Status.PodIP = ""
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			name: "unready",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			name: "different metadata",
			setup: func(t *testing.T, state *testState) {
				// create a pod with different metadata than state.fn
				fn := state.fn
				fn.Metadata = "different"
				err := state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}

			tc.setup(t, state)

			ctrl := New(testConfig(), nil, state.fakeKubernetes, nil)
			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			state.instances, err = ctrl.getReadyInstances(state.fn)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, state)
		})
	}
}

func TestControllerInformer(t *testing.T) {
	type testState struct {
		ctrlPod        *v1.Pod
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
	}

	assertCtrlPodInRing := func(t *testing.T, state *testState) {
		t.Helper()
		ringIps := state.ctrl.ring.List()
		assert.Assert(t, len(ringIps) == 1)
		assert.Assert(t, fixture.ControllerIP == ringIps[0])
	}

	assertCtrlPodNotInRing := func(t *testing.T, state *testState) {
		t.Helper()
		ringIps := state.ctrl.ring.List()
		assert.Assert(t, len(ringIps) == 0)
	}

	testCases := []struct {
		name   string
		setup  func(*testing.T, *testState)
		change func(*testing.T, *testState)
		check  func(*testing.T, *testState)
	}{
		{
			name: "pod exists",
			setup: func(t *testing.T, state *testState) {
				state.ctrlPod = fixture.NewControllerPod()
			},
			check: func(t *testing.T, state *testState) {
				assertCtrlPodInRing(t, state)
			},
		},
		{
			name: "pod added",
			setup: func(t *testing.T, state *testState) {
				// intentionally leave ctrlPod nil
			},
			change: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewControllerPod())
			},
			check: func(t *testing.T, state *testState) {
				assertCtrlPodInRing(t, state)
			},
		},
		{
			name: "pod deleted",
			setup: func(t *testing.T, state *testState) {
				state.ctrlPod = fixture.NewControllerPod()
			},
			change: func(t *testing.T, state *testState) {
				assertCtrlPodInRing(t, state)
				state.fakeKubernetes.Tracker().Delete(v1.SchemeGroupVersion.WithResource("pods"), state.ctrlPod.Namespace, state.ctrlPod.Name)
			},
			check: func(t *testing.T, state *testState) {
				assertCtrlPodNotInRing(t, state)
			},
		},
		{
			name: "pod updated with condition unready",
			setup: func(t *testing.T, state *testState) {
				state.ctrlPod = fixture.NewControllerPod()
			},
			change: func(t *testing.T, state *testState) {
				assertCtrlPodInRing(t, state)
				state.ctrlPod.Status.Conditions = []v1.PodCondition{
					{
						Type:   v1.PodReady,
						Status: v1.ConditionFalse,
					},
				}
				state.fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), state.ctrlPod, state.ctrlPod.Namespace)
			},
			check: func(t *testing.T, state *testState) {
				assertCtrlPodNotInRing(t, state)
			},
		},
		{
			name: "pod updated with phase succeeded",
			setup: func(t *testing.T, state *testState) {
				state.ctrlPod = fixture.NewControllerPod()
			},
			change: func(t *testing.T, state *testState) {
				assertCtrlPodInRing(t, state)
				state.ctrlPod.Status.Phase = v1.PodSucceeded
				state.fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), state.ctrlPod, state.ctrlPod.Namespace)
			},
			check: func(t *testing.T, state *testState) {
				assertCtrlPodNotInRing(t, state)
			},
		},
		{
			name: "pod updated with phase failed",
			setup: func(t *testing.T, state *testState) {
				state.ctrlPod = fixture.NewControllerPod()
			},
			change: func(t *testing.T, state *testState) {
				assertCtrlPodInRing(t, state)
				state.ctrlPod.Status.Phase = v1.PodFailed
				state.fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), state.ctrlPod, state.ctrlPod.Namespace)
			},
			check: func(t *testing.T, state *testState) {
				assertCtrlPodNotInRing(t, state)
			},
		},
		{
			name: "pod updated with phase unknown",
			setup: func(t *testing.T, state *testState) {
				state.ctrlPod = fixture.NewControllerPod()
			},
			change: func(t *testing.T, state *testState) {
				assertCtrlPodInRing(t, state)
				state.ctrlPod.Status.Phase = v1.PodUnknown
				state.fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), state.ctrlPod, state.ctrlPod.Namespace)
			},
			check: func(t *testing.T, state *testState) {
				assertCtrlPodNotInRing(t, state)
			},
		},
		{
			name: "pod updated with no ip",
			setup: func(t *testing.T, state *testState) {
				state.ctrlPod = fixture.NewControllerPod()
			},
			change: func(t *testing.T, state *testState) {
				assertCtrlPodInRing(t, state)
				state.ctrlPod.Status.PodIP = ""
				state.fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), state.ctrlPod, state.ctrlPod.Namespace)
			},
			check: func(t *testing.T, state *testState) {
				assertCtrlPodNotInRing(t, state)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fakeKubernetes: fake.NewClientset(),
			}

			tc.setup(t, state)

			if state.ctrlPod != nil {
				state.fakeKubernetes.Tracker().Add(state.ctrlPod)
			}

			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)
			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			if tc.change != nil {
				tc.change(t, state)
				time.Sleep(100 * time.Millisecond)
			}

			tc.check(t, state)
		})
	}
}
