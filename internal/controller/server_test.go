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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	type setupStruct struct {
		fn      function.Function
		headers map[string]string
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *Controller, *fake.Clientset) setupStruct
		check func(*testing.T, *Controller, *fake.Clientset, setupStruct, *httptest.ResponseRecorder)
	}{
		{
			name: "filters by excluded instance names",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) setupStruct {
				fn := fixture.NewFunction()
				// add two ready instances
				podA := fixture.NewAssignedPod(t, fn, nil)
				podA.Name = fn.Deployment + "-a"
				podB := fixture.NewAssignedPod(t, fn, nil)
				podB.Name = fn.Deployment + "-b"
				fakeKubernetes.Tracker().Add(podA)
				fakeKubernetes.Tracker().Add(podB)
				return setupStruct{fn: fn, headers: map[string]string{key.ExcludeInstanceNames.Header: fn.Deployment + "-a"}}
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, setupStruct setupStruct, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)
				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				// ensure we did not receive the excluded one
				must.Eq(t, setupStruct.fn.Deployment+"-b", instance.Name)
			},
		},
		{
			name: "reverts to all instances when all instances on excluded list",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) setupStruct {
				fn := fixture.NewFunction()
				// add multiple ready instances
				podA := fixture.NewAssignedPod(t, fn, nil)
				podA.Name = fn.Deployment + "-a"
				podB := fixture.NewAssignedPod(t, fn, nil)
				podB.Name = fn.Deployment + "-b"
				podC := fixture.NewAssignedPod(t, fn, nil)
				podC.Name = fn.Deployment + "-c"
				fakeKubernetes.Tracker().Add(podA)
				fakeKubernetes.Tracker().Add(podB)
				fakeKubernetes.Tracker().Add(podC)
				return setupStruct{fn: fn, headers: map[string]string{key.ExcludeInstanceNames.Header: fn.Deployment + "-a," + fn.Deployment + "-b," + fn.Deployment + "-c"}}
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, setupStruct setupStruct, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)
				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				// should return one of the instances since we revert to unfiltered list
				must.True(t, instance.Name == setupStruct.fn.Deployment+"-a" || instance.Name == setupStruct.fn.Deployment+"-b" || instance.Name == setupStruct.fn.Deployment+"-c")
			},
		},
		{
			name: "smoke",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) setupStruct {
				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				return setupStruct{fn: fn, headers: map[string]string{}}
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, setupStruct setupStruct, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, setupStruct.fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())
			},
		},
		{
			name: "unassigned with unassigned pod",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) setupStruct {
				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				return setupStruct{fn: fn, headers: map[string]string{}}
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, setupStruct setupStruct, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, setupStruct.fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())
			},
		},
		{
			name: "unassigned with eventual unassigned pod",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) setupStruct {
				fn := fixture.NewFunction()
				go func() {
					time.Sleep(500 * time.Millisecond)
					fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				}()
				return setupStruct{fn: fn, headers: map[string]string{}}
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, setupStruct setupStruct, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, setupStruct.fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())
			},
		},
		{
			name: "instances > max",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) setupStruct {
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

				return setupStruct{fn: fn, headers: map[string]string{}}
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, setupStruct setupStruct, rw *httptest.ResponseRecorder) {
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, setupStruct.fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())

				// ensure we didn't receive the earliest assigned at instance
				must.NotEq(t, "earliest-assigned-at", instance.Name)
			},
		},
		{
			name: "no ready instances while scaling up race",
			setup: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset) setupStruct {
				fn := fixture.NewFunction()

				// grab scale lock
				scaleMu := ctrl.getScaleMu(fn)
				scaleMu.Lock()

				go func() {
					// wait for GET /instance and have it scale to 1 because there are no ready instances
					time.Sleep(500 * time.Millisecond)

					// add 2 ready instances
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))

					// give the informers a chance to update their caches
					time.Sleep(10 * time.Millisecond)

					// release scale lock
					scaleMu.Unlock()
				}()

				return setupStruct{fn: fn, headers: map[string]string{}}
			},
			check: func(t *testing.T, ctrl *Controller, fakeKubernetes *fake.Clientset, setupStruct setupStruct, rw *httptest.ResponseRecorder) {
				// ensure we received an instance
				must.Eq(t, http.StatusOK, rw.Code)

				var instance *function.Instance
				must.NoError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				must.Eq(t, setupStruct.fn, instance.Function)
				must.False(t, instance.ReadyAt.IsZero())

				// ensure we still have the 2 ready instances because we didn't scale down to 1
				pods, err := fakeKubernetes.CoreV1().Pods(setupStruct.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 2, pods.Items)
				for _, pod := range pods.Items {
					ensurePodIsAssignedToFunction(t, pod, setupStruct.fn)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			ctrl := New(nil, fakeKubernetes, nil)
			setupStruct := tc.setup(t, ctrl, fakeKubernetes)

			err := ctrl.startInformers(ctx)
			must.NoError(t, err)

			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/instance", nil)
			setupStruct.fn.SetHeader(req)
			for header, value := range setupStruct.headers {
				req.Header.Set(header, value)
			}
			rw := httptest.NewRecorder()
			ctrl.Handler().ServeHTTP(rw, req)

			tc.check(t, ctrl, fakeKubernetes, setupStruct, rw)
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
