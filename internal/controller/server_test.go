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
	"gotest.tools/v3/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				assert.Assert(t, rw.Code == http.StatusOK)
				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				// ensure we did not receive the excluded one
				assert.Assert(t, instance.Name == setupStruct.fn.Deployment+"-b")
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
				assert.Assert(t, rw.Code == http.StatusOK)
				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				// should return one of the instances since we revert to unfiltered list
				assert.Assert(t, instance.Name == setupStruct.fn.Deployment+"-a" || instance.Name == setupStruct.fn.Deployment+"-b" || instance.Name == setupStruct.fn.Deployment+"-c")
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
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function == setupStruct.fn)
				assert.Assert(t, !instance.ReadyAt.IsZero())
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
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function == setupStruct.fn)
				assert.Assert(t, !instance.ReadyAt.IsZero())
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
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function == setupStruct.fn)
				assert.Assert(t, !instance.ReadyAt.IsZero())
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
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function == setupStruct.fn)
				assert.Assert(t, !instance.ReadyAt.IsZero())

				// ensure we didn't receive the earliest assigned at instance
				assert.Assert(t, instance.Name != "earliest-assigned-at")
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
				assert.Assert(t, rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function == setupStruct.fn)
				assert.Assert(t, !instance.ReadyAt.IsZero())

				// ensure we still have the 2 ready instances because we didn't scale down to 1
				pods, err := fakeKubernetes.CoreV1().Pods(setupStruct.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)
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
			assert.NilError(t, err)

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
			name: "receiving one heartbeat",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				return []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, sentHeartbeats []function.Heartbeat) {
				// ensure the controller has added a heartbeat
				assert.Assert(t, ctrl.routerHeartbeats.Size() == 1)

				// ensure the heartbeat was associated with the expected router IP and has the expected timestamp
				sentHeartbeat := sentHeartbeats[0]
				heartbeat, ok := ctrl.routerHeartbeats.Load(sentHeartbeat.Function)
				assert.Assert(t, ok)
				assert.Assert(t, heartbeat[fixture.RouterIP].Timestamp.Equal(sentHeartbeat.Timestamp))
			},
		},
		{
			name: "receiving multiple heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				return []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, sentHeartbeats []function.Heartbeat) {
				// ensure the controller has added both heartbeats
				assert.Assert(t, ctrl.routerHeartbeats.Size() == 2)
				for _, sentHeartbeat := range sentHeartbeats {
					// ensure each heartbeat was associated with the expected router IP and has the expected timestamp
					heartbeat, ok := ctrl.routerHeartbeats.Load(sentHeartbeat.Function)
					assert.Assert(t, ok)
					assert.Assert(t, heartbeat[fixture.RouterIP].Timestamp.Equal(sentHeartbeat.Timestamp))
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
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, sentHeartbeats []function.Heartbeat) {
				assert.Assert(t, ctrl.routerHeartbeats.Size() == 1)

				// ensure the controller kept the heartbeat with the most recent timestamp
				sentHeartbeat := sentHeartbeats[0]
				keptHeartbeat, ok := ctrl.routerHeartbeats.Load(sentHeartbeat.Function)
				assert.Assert(t, ok)
				assert.Assert(t, !keptHeartbeat[fixture.RouterIP].Timestamp.Equal(sentHeartbeat.Timestamp))
				assert.Assert(t, sentHeartbeat.Timestamp.Add(time.Hour).Equal(keptHeartbeat[fixture.RouterIP].Timestamp))
			},
		},
		{
			name: "forwards heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				// seed the ring with multiple controller IPs
				ctrl.ring.Add(fixture.ControllerIP)
				ctrl.ring.Add(fixture.ControllerIP2)

				// send multiple heartbeats for different functions
				sentHeartbeats := []function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}

				// ensure the controller sends the heartbeats to the other controllers
				mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []function.Heartbeat, forwardedFor ...string) error {
					// ensure the controller forwards the same heartbeats to the other controllers
					assert.DeepEqual(t, heartbeats, sentHeartbeats)
					// ensure the controller forwards the list of controllers that have received heartbeats
					assert.DeepEqual(t, forwardedFor, []string{fixture.ControllerIP, fixture.ControllerIP2})
					return nil
				})

				return sentHeartbeats
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, sentHeartbeats []function.Heartbeat) {
				// give the goroutine that forwards the heartbeats a chance to run
				time.Sleep(10 * time.Millisecond)
			},
		},
		{
			name: "deletes expired heartbeats",
			setup: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller) []function.Heartbeat {
				// seed the controller with an expired heartbeat from a different router
				fn := fixture.NewFunction()
				ctrl.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP2: {Timestamp: time.Now().Add(-(FlagHeartbeatTimeout.Value() + time.Second))}})

				return []function.Heartbeat{
					{Function: fn, Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, mcc *fixture.MockControllerClient, ctrl *Controller, sentHeartbeats []function.Heartbeat) {
				sentHeartbeat := sentHeartbeats[0]
				keptHeartbeat, ok := ctrl.routerHeartbeats.Load(sentHeartbeat.Function)
				assert.Assert(t, ok)

				// ensure the controller kept the sent heartbeat
				assert.Assert(t, len(keptHeartbeat) == 1)
				assert.Assert(t, keptHeartbeat[fixture.RouterIP].Timestamp.Equal(sentHeartbeat.Timestamp))

				// ensure the expired heartbeat was deleted
				_, ok = keptHeartbeat[fixture.RouterIP2]
				assert.Assert(t, !ok)
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
