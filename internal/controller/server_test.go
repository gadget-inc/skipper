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
	"github.com/shoenig/test/must"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	ctrl := New(nil, fake.NewClientset(), nil)
	ctrl.Handler().ServeHTTP(rw, req)

	must.Eq(t, http.StatusOK, rw.Code)
	must.Length(t, 0, rw.Body)
}

func TestHandleInstance(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(*testing.T, *Controller, *fake.Clientset) function.Function
		check func(*testing.T, *Controller, *fake.Clientset, function.Function, *httptest.ResponseRecorder)
	}{
		{
			name: "smoke",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) function.Function {
				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				return fn
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, fn function.Function, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())
			},
		},
		{
			name: "unassigned with unassigned pod",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) function.Function {
				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				return fn
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, fn function.Function, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())
			},
		},
		{
			name: "unassigned with eventual unassigned pod",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) function.Function {
				fn := fixture.NewFunction()
				go func() {
					time.Sleep(500 * time.Millisecond)
					fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				}()
				return fn
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, fn function.Function, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())
			},
		},
		{
			name: "instances > max",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) function.Function {
				fn := fixture.NewFunction()
				fn.Scale.MaxInstances = 1 // ensure we can only have one instance

				// add max instances
				for range fn.Scale.MaxInstances {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add another instance with an earlier assigned at
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Name = "earliest-assigned-at"
				pod.Annotations[key.AssignedAt.Label] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(pod)

				return fn
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, fn function.Function, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())

				// ensure we didn't receive the earliest assigned at instance
				must.NotEq(t, "earliest-assigned-at", instance.Name)
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
			must.NoError(t, err)

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
		setup func(*testing.T, *fixture.MockControllerClient, *Controller) []function.Heartbeat
		check func(*testing.T, *fixture.MockControllerClient, *Controller, []function.Heartbeat)
	}{
		{
			name: "one",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				return []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 1, ctrl.routerHeartbeats.Size())
				heartbeat, ok := ctrl.routerHeartbeats.Load(heartbeats[0].Function)
				must.True(t, ok)
				must.Eq(t, heartbeats[0].Timestamp, heartbeat[fixture.RouterIP].Timestamp)
			},
		},
		{
			name: "multiple",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				return []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 2, ctrl.routerHeartbeats.Size())
				for _, hb := range heartbeats {
					heartbeat, ok := ctrl.routerHeartbeats.Load(hb.Function)
					must.True(t, ok)
					must.Eq(t, hb.Timestamp, heartbeat[fixture.RouterIP].Timestamp)
				}
			},
		},
		{
			name: "keeps most recent",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				// seed the controller with a recent heartbeat
				fn := fixture.NewFunction()
				heartbeatTimestamp := time.Now()
				ctrl.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Timestamp: heartbeatTimestamp}})

				// send an old heartbeat
				return []function.Heartbeat{
					{Function: fn, Timestamp: heartbeatTimestamp.Add(-time.Hour)},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				must.Eq(t, 1, ctrl.routerHeartbeats.Size())

				sentHeartbeat := heartbeats[0]
				keptHeartbeat, ok := ctrl.routerHeartbeats.Load(sentHeartbeat.Function)

				must.True(t, ok)
				must.NotEq(t, keptHeartbeat[fixture.RouterIP].Timestamp, sentHeartbeat.Timestamp)
				must.Eq(t, keptHeartbeat[fixture.RouterIP].Timestamp, sentHeartbeat.Timestamp.Add(time.Hour))
			},
		},
		{
			name: "forwards heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				ctrl.ring.Add(fixture.ControllerIP)
				ctrl.ring.Add(fixture.ControllerIP2)
				hbs := []function.Heartbeat{{Function: fixture.NewFunction(), Timestamp: time.Now()}}

				mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []function.Heartbeat, forwardedFor ...string) error {
					must.Eq(t, hbs, heartbeats)
					must.Eq(t, []string{fixture.ControllerIP, fixture.ControllerIP2}, forwardedFor)
					return nil
				})

				return hbs
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				// give the goroutine that forwards the heartbeats a chance to run
				time.Sleep(10 * time.Millisecond)
			},
		},
		{
			name: "garbage collects old heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				// seed the controller with a old heartbeat from a different router
				fn := fixture.NewFunction()
				ctrl.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP2: {Timestamp: time.Now().Add(-(FlagHeartbeatTimeout.Value() + time.Second))}})

				return []function.Heartbeat{
					{Function: fn, Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, heartbeats []function.Heartbeat) {
				sentHeartbeat := heartbeats[0]
				keptHeartbeat, ok := ctrl.routerHeartbeats.Load(sentHeartbeat.Function)
				must.True(t, ok)
				must.Eq(t, 1, len(keptHeartbeat))
				must.MapNotContainsKey(t, keptHeartbeat, fixture.RouterIP2)
				must.Eq(t, sentHeartbeat.Timestamp, keptHeartbeat[fixture.RouterIP].Timestamp)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mcc := fixture.NewMockControllerClient(t)
			ctrl := New(func(host string, port int) Client { return mcc }, fake.NewClientset(), nil)

			heartbeats := tc.setup(t, mcc, ctrl)
			heartbeatBytes, err := json.Marshal(heartbeats)
			must.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewReader(heartbeatBytes))
			req.Header.Set(key.RouterIP.Header, fixture.RouterIP)
			rw := httptest.NewRecorder()
			ctrl.Handler().ServeHTTP(rw, req)

			must.Eq(t, http.StatusOK, rw.Code)
			must.Length(t, 0, rw.Body)
			tc.check(t, mcc, ctrl, heartbeats)
		})
	}
}

func TestShouldThrottle(t *testing.T) {
	ctrl := New(nil, fake.NewClientset(), nil)
	fn := fixture.NewFunction()
	fn.Scale.TargetInFlightRequests = 4

	instances := []*function.Instance{
		{Function: fn, Name: "a"},
		{Function: fn, Name: "b"},
	}

	loads := map[string]int{
		"a": 6,
		"b": 6,
	}

	ctrl.markScaleActivity(fn)
	must.True(t, ctrl.shouldThrottle(context.Background(), fn, instances, loads, time.Now()))
}

func TestShouldThrottleRespectsDeadline(t *testing.T) {
	ctrl := New(nil, fake.NewClientset(), nil)
	fn := fixture.NewFunction()
	fn.Scale.TargetInFlightRequests = 4

	instances := []*function.Instance{
		{Function: fn, Name: "a"},
		{Function: fn, Name: "b"},
	}

	loads := map[string]int{
		"a": 6,
		"b": 6,
	}

	ctrl.markScaleActivity(fn)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(instanceThrottleDeadlineBuffer/2))
	defer cancel()

	must.False(t, ctrl.shouldThrottle(ctx, fn, instances, loads, time.Now()))
}

func TestChooseLeastBusyInstance(t *testing.T) {
	ctrl := New(nil, fake.NewClientset(), nil)
	fn := fixture.NewFunction()
	fn.Scale.TargetInFlightRequests = 50 // threshold will be 50 * 1.25 = 62.5 -> 63

	t.Run("filters out overloaded instances", func(t *testing.T) {
		instances := []*function.Instance{
			{Function: fn, Name: "a"},
			{Function: fn, Name: "b"},
			{Function: fn, Name: "c"},
		}

		loads := map[string]int{
			"a": 100, // above threshold (63)
			"b": 1,   // below threshold (63)
			"c": 1,   // below threshold (63)
		}

		selected := ctrl.chooseLeastBusyInstance(fn, instances, loads)
		// Only b and c should be selected (both below threshold)
		must.True(t, selected.Name == "b" || selected.Name == "c")
		must.NotEq(t, "a", selected.Name)
	})

	t.Run("all instances below threshold", func(t *testing.T) {
		instances := []*function.Instance{
			{Function: fn, Name: "a"},
			{Function: fn, Name: "b"},
			{Function: fn, Name: "c"},
		}

		loads := map[string]int{
			"a": 5, // below threshold (63)
			"b": 1, // below threshold (63)
			"c": 1, // below threshold (63)
		}

		selected := ctrl.chooseLeastBusyInstance(fn, instances, loads)
		// All instances are below threshold, so any can be selected
		must.True(t, selected.Name == "a" || selected.Name == "b" || selected.Name == "c")
	})

	t.Run("all instances above threshold falls back to all", func(t *testing.T) {
		instances := []*function.Instance{
			{Function: fn, Name: "a"},
			{Function: fn, Name: "b"},
		}

		loads := map[string]int{
			"a": 100, // above threshold (63)
			"b": 200, // above threshold (63)
		}

		selected := ctrl.chooseLeastBusyInstance(fn, instances, loads)
		// All instances are above threshold, so fall back to all instances
		must.True(t, selected.Name == "a" || selected.Name == "b")
	})

	t.Run("no threshold set uses all instances", func(t *testing.T) {
		fnNoThreshold := fixture.NewFunction()
		fnNoThreshold.Scale.TargetInFlightRequests = 0 // no threshold

		instances := []*function.Instance{
			{Function: fnNoThreshold, Name: "a"},
			{Function: fnNoThreshold, Name: "b"},
			{Function: fnNoThreshold, Name: "c"},
		}

		loads := map[string]int{
			"a": 1000, // any load is acceptable when no threshold
			"b": 5000,
			"c": 100,
		}

		selected := ctrl.chooseLeastBusyInstance(fnNoThreshold, instances, loads)
		// No threshold means all instances are candidates
		must.True(t, selected.Name == "a" || selected.Name == "b" || selected.Name == "c")
	})
}

func TestInFlightPerInstanceAggregates(t *testing.T) {
	ctrl := New(nil, fake.NewClientset(), nil)
	fn := fixture.NewFunction()

	ctrl.routerHeartbeats.Store(fn, RouterHeartbeats{
		"router-1": {
			Function:            fn,
			InFlightPerInstance: map[string]int{"a": 2},
		},
		"router-2": {
			Function:            fn,
			InFlightPerInstance: map[string]int{"a": 3, "b": 1},
		},
	})

	loads := ctrl.inFlightPerInstance(fn)
	must.Eq(t, 5, loads["a"])
	must.Eq(t, 1, loads["b"])
}
