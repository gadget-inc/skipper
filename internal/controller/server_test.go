package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	ctrl := New(nil, fake.NewClientset(), nil)
	ctrl.Handler().ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Body.Len() == 0)
}

func TestHandleInstance(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(*testing.T, *Controller, *fake.Clientset) *function.Function
		check func(*testing.T, *Controller, *fake.Clientset, *function.Function, *httptest.ResponseRecorder)
	}{
		{
			name: "smoke",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) *function.Function {
				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				return fn
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, fn *function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.GetFunction().Equal(fn))
				assert.Assert(t, !instance.GetReadyAt().AsTime().IsZero())
			},
		},
		{
			name: "unassigned with unassigned pod",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) *function.Function {
				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				return fn
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, fn *function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.GetFunction().Equal(fn))
				assert.Assert(t, !instance.GetReadyAt().AsTime().IsZero())
			},
		},
		{
			name: "unassigned with eventual unassigned pod",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) *function.Function {
				fn := fixture.NewFunction()
				go func() {
					time.Sleep(500 * time.Millisecond)
					fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				}()
				return fn
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, fn *function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.GetFunction().Equal(fn))
				assert.Assert(t, !instance.GetReadyAt().AsTime().IsZero())
			},
		},
		{
			name: "instances > max",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) *function.Function {
				fn := fixture.NewFunction()
				fn.GetScale().SetMaxInstances(1) // ensure we can only have one instance

				// add max instances
				for range fn.GetScale().GetMaxInstances() {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add another instance with an earlier assigned at
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Name = "earliest-assigned-at"
				pod.Annotations[key.AssignedAt.Label] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(pod)

				return fn
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, fn *function.Function, rw *httptest.ResponseRecorder) {
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.GetFunction().Equal(fn))
				assert.Assert(t, !instance.GetReadyAt().AsTime().IsZero())

				// ensure we didn't receive the earliest assigned at instance
				assert.Assert(t, instance.GetName() != "earliest-assigned-at")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			ctrl := New(nil, fakeKubernetes, nil)
			fn := tc.setup(t, ctrl, fakeKubernetes)

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/instance", nil)
			fn.SetHeader(req)
			rw := httptest.NewRecorder()
			ctrl.Handler().ServeHTTP(rw, req)

			tc.check(t, ctrl, fakeKubernetes, fn, rw)
		})
	}
}

func TestHandleHeartbeat(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(*testing.T, *fixture.MockControllerClient, *Controller) []*function.Heartbeat
		check func(*testing.T, *fixture.MockControllerClient, *Controller, []*function.Heartbeat)
	}{
		{
			name: "one",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []*function.Heartbeat {
				return []*function.Heartbeat{
					function.Heartbeat_builder{Function: fixture.NewFunction(), Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []*function.Heartbeat) {
				assert.Assert(t, ctrl.supervisors.Size() == 1)
				supervisor, ok := ctrl.supervisors.Load(heartbeats[0].GetFunction().Hash())
				assert.Assert(t, ok)
				assert.Assert(t, supervisor.routerHeartbeats[fixture.RouterIP].GetTimestamp().AsTime().Equal(heartbeats[0].GetTimestamp().AsTime()))
			},
		},
		{
			name: "multiple",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []*function.Heartbeat {
				return []*function.Heartbeat{
					function.Heartbeat_builder{Function: fixture.NewFunction(), Timestamp: timestamppb.Now()}.Build(),
					function.Heartbeat_builder{Function: fixture.NewFunction(), Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []*function.Heartbeat) {
				assert.Assert(t, ctrl.supervisors.Size() == 2)
				for _, hb := range heartbeats {
					supervisor, ok := ctrl.supervisors.Load(hb.GetFunction().Hash())
					assert.Assert(t, ok)
					assert.Assert(t, supervisor.routerHeartbeats[fixture.RouterIP].GetTimestamp().AsTime().Equal(hb.GetTimestamp().AsTime()))
				}
			},
		},
		{
			name: "keeps most recent",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []*function.Heartbeat {
				// seed the controller with a recent heartbeat
				fn := fixture.NewFunction()
				heartbeatTimestamp := time.Now()
				ctrl.supervisors.Store(fn.Hash(), &Supervisor{fn: fn, ctrl: ctrl, routerHeartbeats: map[string]*function.Heartbeat{
					fixture.RouterIP: function.Heartbeat_builder{Function: fn, Timestamp: timestamppb.New(heartbeatTimestamp)}.Build(),
				}})

				// send an old heartbeat
				return []*function.Heartbeat{
					function.Heartbeat_builder{Function: fn, Timestamp: timestamppb.New(heartbeatTimestamp.Add(-time.Hour))}.Build(),
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []*function.Heartbeat) {
				assert.Assert(t, ctrl.supervisors.Size() == 1)

				sentHeartbeat := heartbeats[0]
				supervisor, ok := ctrl.supervisors.Load(sentHeartbeat.GetFunction().Hash())
				assert.Assert(t, ok)
				assert.Assert(t, !supervisor.routerHeartbeats[fixture.RouterIP].GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))
				assert.Assert(t, sentHeartbeat.GetTimestamp().AsTime().Add(time.Hour).Equal(supervisor.routerHeartbeats[fixture.RouterIP].GetTimestamp().AsTime()))
			},
		},
		{
			name: "forwards heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []*function.Heartbeat {
				ctrl.ring.Add(fixture.ControllerIP)
				ctrl.ring.Add(fixture.ControllerIP2)
				hbs := []*function.Heartbeat{
					function.Heartbeat_builder{Function: fixture.NewFunction(), Timestamp: timestamppb.Now()}.Build(),
				}

				mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*function.Heartbeat, forwardedFor ...string) error {
					assert.DeepEqual(t, heartbeats, hbs)
					assert.DeepEqual(t, forwardedFor, []string{fixture.ControllerIP, fixture.ControllerIP2})
					return nil
				})

				return hbs
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []*function.Heartbeat) {
				// give the goroutine that forwards the heartbeats a chance to run
				time.Sleep(10 * time.Millisecond)
			},
		},
		{
			name: "garbage collects old heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []*function.Heartbeat {
				// seed the controller with a old heartbeat from a different router
				fn := fixture.NewFunction()
				ctrl.supervisors.Store(fn.Hash(), &Supervisor{fn: fn, ctrl: ctrl, routerHeartbeats: map[string]*function.Heartbeat{
					fixture.RouterIP2: function.Heartbeat_builder{Function: fn, Timestamp: timestamppb.New(time.Now().Add(-(FlagHeartbeatTimeout.Value() + time.Second)))}.Build(),
				}})

				return []*function.Heartbeat{
					function.Heartbeat_builder{Function: fn, Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []*function.Heartbeat) {
				sentHeartbeat := heartbeats[0]
				supervisor, ok := ctrl.supervisors.Load(sentHeartbeat.GetFunction().Hash())
				assert.Assert(t, ok)
				assert.Assert(t, len(supervisor.routerHeartbeats) == 1)
				assert.Assert(t, supervisor.routerHeartbeats[fixture.RouterIP2] == nil)
				assert.Assert(t, supervisor.routerHeartbeats[fixture.RouterIP].GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mcc := fixture.NewMockControllerClient(t)
			ctrl := New(func(host string, port int) Client { return mcc }, fake.NewClientset(), nil)

			heartbeats := tc.setup(t, mcc, ctrl)
			heartbeatBytes, err := json.Marshal(heartbeats)
			assert.NilError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewReader(heartbeatBytes))
			req.Header.Set(key.RouterIP.Header, fixture.RouterIP)
			rw := httptest.NewRecorder()
			ctrl.Handler().ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == http.StatusOK)
			assert.Assert(t, rw.Body.Len() == 0)
			tc.check(t, mcc, ctrl, heartbeats)
		})
	}
}
