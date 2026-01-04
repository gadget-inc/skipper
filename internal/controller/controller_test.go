package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

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
