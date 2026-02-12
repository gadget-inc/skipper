package controller

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestControllerInformer(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

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

func TestWatchErrorHandler(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		err  error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
		},
		{
			name: "status reason expired",
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Reason: metav1.StatusReasonExpired,
				},
			},
		},
		{
			name: "other error",
			err:  errors.New("some other error"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
			handler := ctrl.watchErrorHandler(t.Context())

			// should not panic for any error type
			handler(nil, tc.err)
		})
	}
}

func TestGetControllerClient(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		check func(*testing.T, *Controller, *atomic.Int32)
	}{
		{
			name: "creates new client for new IP",
			check: func(t *testing.T, ctrl *Controller, callCount *atomic.Int32) {
				ctrl.getControllerClient("127.0.0.1")
				assert.Assert(t, callCount.Load() == 1)
			},
		},
		{
			name: "returns cached client for same IP",
			check: func(t *testing.T, ctrl *Controller, callCount *atomic.Int32) {
				client1 := ctrl.getControllerClient("127.0.0.1")
				client2 := ctrl.getControllerClient("127.0.0.1")

				// newClientFunc should only be called once
				assert.Assert(t, callCount.Load() == 1)
				// both calls should return the same client instance
				assert.Assert(t, client1 == client2)
			},
		},
		{
			name: "creates different clients for different IPs",
			check: func(t *testing.T, ctrl *Controller, callCount *atomic.Int32) {
				client1 := ctrl.getControllerClient("127.0.0.1")
				client2 := ctrl.getControllerClient("127.0.0.2")

				// newClientFunc should be called twice
				assert.Assert(t, callCount.Load() == 2)
				// clients should be different instances
				assert.Assert(t, client1 != client2)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			callCount := &atomic.Int32{}
			ctrl := New(testConfig(), func(host string) Client {
				callCount.Add(1)
				return fixture.NewMockControllerClient(t)
			}, fake.NewClientset(), nil)

			tc.check(t, ctrl, callCount)
		})
	}
}

// closeTrackingClient wraps a Client and tracks Close() calls
type closeTrackingClient struct {
	Client
	closeCalls *atomic.Int32
}

func (c *closeTrackingClient) Close() error {
	c.closeCalls.Add(1)
	return c.Client.Close()
}

func TestControllerClose(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		clientIPs  []string
		wantClosed int32
	}{
		{
			name:       "closes all clients",
			clientIPs:  []string{"127.0.0.1", "127.0.0.2", "127.0.0.3"},
			wantClosed: 3,
		},
		{
			name:       "handles no clients",
			clientIPs:  []string{},
			wantClosed: 0,
		},
		{
			name:       "closes single client",
			clientIPs:  []string{"127.0.0.1"},
			wantClosed: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			closeCalls := &atomic.Int32{}
			ctrl := New(testConfig(), func(host string) Client {
				return &closeTrackingClient{
					Client:     fixture.NewMockControllerClient(t),
					closeCalls: closeCalls,
				}
			}, fake.NewClientset(), nil)

			// Create clients for each IP
			for _, ip := range tc.clientIPs {
				ctrl.getControllerClient(ip)
			}

			ctrl.Close()

			assert.Assert(t, closeCalls.Load() == tc.wantClosed,
				"expected %d Close() calls, got %d", tc.wantClosed, closeCalls.Load())
		})
	}
}
