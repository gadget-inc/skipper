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

	ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
	ctrl.Handler().ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Body.Len() == 0)
}

func TestHandleInstance(t *testing.T) {
	type testState struct {
		fn             *function.Function
		headers        map[string]string
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
		rw             *httptest.ResponseRecorder
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "filters by excluded instance names",
			setup: func(t *testing.T, state *testState) {
				// add two ready instances
				podA := fixture.NewAssignedPod(t, state.fn, nil)
				podA.Name = state.fn.Deployment + "-a"
				podB := fixture.NewAssignedPod(t, state.fn, nil)
				podB.Name = state.fn.Deployment + "-b"
				state.fakeKubernetes.Tracker().Add(podA)
				state.fakeKubernetes.Tracker().Add(podB)
				state.headers = map[string]string{key.ExcludeInstanceNames.Header: state.fn.Deployment + "-a"}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instance))
				// ensure we did not receive the excluded one
				assert.Assert(t, instance.Name == state.fn.Deployment+"-b")
			},
		},
		{
			name: "reverts to all instances when all instances on excluded list",
			setup: func(t *testing.T, state *testState) {
				// add multiple ready instances
				podA := fixture.NewAssignedPod(t, state.fn, nil)
				podA.Name = state.fn.Deployment + "-a"
				podB := fixture.NewAssignedPod(t, state.fn, nil)
				podB.Name = state.fn.Deployment + "-b"
				podC := fixture.NewAssignedPod(t, state.fn, nil)
				podC.Name = state.fn.Deployment + "-c"
				state.fakeKubernetes.Tracker().Add(podA)
				state.fakeKubernetes.Tracker().Add(podB)
				state.fakeKubernetes.Tracker().Add(podC)
				state.headers = map[string]string{key.ExcludeInstanceNames.Header: state.fn.Deployment + "-a," + state.fn.Deployment + "-b," + state.fn.Deployment + "-c"}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instance))
				// should return one of the instances since we revert to unfiltered list
				assert.Assert(t, instance.Name == state.fn.Deployment+"-a" || instance.Name == state.fn.Deployment+"-b" || instance.Name == state.fn.Deployment+"-c")
			},
		},
		{
			name: "smoke",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function.Equal(state.fn))
				assert.Assert(t, !instance.ReadyAt.IsZero())
			},
		},
		{
			name: "unassigned with unassigned pod",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function.Equal(state.fn))
				assert.Assert(t, !instance.ReadyAt.IsZero())
			},
		},
		{
			name: "unassigned with eventual unassigned pod",
			setup: func(t *testing.T, state *testState) {
				go func() {
					time.Sleep(500 * time.Millisecond)
					state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				}()
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function.Equal(state.fn))
				assert.Assert(t, !instance.ReadyAt.IsZero())
			},
		},
		{
			name: "instances > max",
			setup: func(t *testing.T, state *testState) {
				state.fn.Scale.MaxInstances = 1 // ensure we can only have one instance

				// add max instances
				for range state.fn.Scale.MaxInstances {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}

				// add another instance with an earlier assigned at
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Name = "earliest-assigned-at"
				pod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function.Equal(state.fn))
				assert.Assert(t, !instance.ReadyAt.IsZero())

				// ensure we didn't receive the earliest assigned at instance
				assert.Assert(t, instance.Name != "earliest-assigned-at")
			},
		},
		{
			name: "no ready instances while scaling up race",
			setup: func(t *testing.T, state *testState) {
				// grab supervisor lock
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.mu.Lock()

				go func() {
					// wait for GET /instance and have it scale to 1 because there are no ready instances
					time.Sleep(500 * time.Millisecond)

					// add 2 ready instances
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))

					// give the informers a chance to update their caches
					time.Sleep(10 * time.Millisecond)

					// release supervisor lock
					supervisor.mu.Unlock()
				}()
			},
			check: func(t *testing.T, state *testState) {
				// ensure we received an instance
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instance *function.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instance))
				assert.Assert(t, instance.Function.Equal(state.fn))
				assert.Assert(t, !instance.ReadyAt.IsZero())

				// ensure we still have the 2 ready instances because we didn't scale down to 1
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)
				for _, pod := range pods.Items {
					ensurePodIsAssignedToFunction(t, pod, state.fn)
				}
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
			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

			tc.setup(t, state)

			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/instance", nil)
			state.fn.SetHeader(req)
			for header, value := range state.headers {
				req.Header.Set(header, value)
			}
			state.rw = httptest.NewRecorder()
			state.ctrl.Handler().ServeHTTP(state.rw, req)

			tc.check(t, state)
		})
	}
}

func TestHandleHeartbeat(t *testing.T) {
	type testState struct {
		mcc        *fixture.MockControllerClient
		ctrl       *Controller
		heartbeats []*function.Heartbeat
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "receiving one heartbeat",
			setup: func(t *testing.T, state *testState) {
				state.heartbeats = []*function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, state *testState) {
				// ensure the supervisor has added a heartbeat
				sentHeartbeat := state.heartbeats[0]
				supervisor := state.ctrl.supervisor(sentHeartbeat.Function)
				assert.Assert(t, supervisor.routerHeartbeats.Size() == 1)

				// ensure the heartbeat was associated with the expected router IP and has the expected timestamp
				receivedHeartbeat, ok := supervisor.routerHeartbeats.Load(fixture.RouterIP)
				assert.Assert(t, ok)
				assert.Assert(t, receivedHeartbeat.Timestamp.Equal(sentHeartbeat.Timestamp))
			},
		},
		{
			name: "receiving multiple heartbeats",
			setup: func(t *testing.T, state *testState) {
				state.heartbeats = []*function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, state *testState) {
				// ensure the controller has added both supervisors
				assert.Assert(t, state.ctrl.supervisors.Size() == len(state.heartbeats))
				for _, sentHeartbeat := range state.heartbeats {
					// ensure the supervisor has added the heartbeat
					supervisor := state.ctrl.supervisor(sentHeartbeat.Function)
					assert.Assert(t, supervisor.routerHeartbeats.Size() == 1)

					// ensure the heartbeat was associated with the expected router IP and has the expected timestamp
					receivedHeartbeat, ok := supervisor.routerHeartbeats.Load(fixture.RouterIP)
					assert.Assert(t, ok)
					assert.Assert(t, receivedHeartbeat.Timestamp.Equal(sentHeartbeat.Timestamp))
				}
			},
		},
		{
			name: "keeps most recent",
			setup: func(t *testing.T, state *testState) {
				// seed the supervisor with a recent heartbeat
				fn := fixture.NewFunction()
				heartbeatTimestamp := time.Now()
				supervisor := state.ctrl.supervisor(fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, &function.Heartbeat{Function: fn, Timestamp: heartbeatTimestamp})

				// send an old heartbeat
				state.heartbeats = []*function.Heartbeat{
					{Function: fn, Timestamp: heartbeatTimestamp.Add(-time.Hour)},
				}
			},
			check: func(t *testing.T, state *testState) {
				sentHeartbeat := state.heartbeats[0]
				keptHeartbeat, ok := state.ctrl.supervisor(sentHeartbeat.Function).routerHeartbeats.Load(fixture.RouterIP)

				// ensure the supervisor kept the heartbeat with the most recent timestamp
				assert.Assert(t, ok)
				assert.Assert(t, !keptHeartbeat.Timestamp.Equal(sentHeartbeat.Timestamp))
				assert.Assert(t, sentHeartbeat.Timestamp.Add(time.Hour).Equal(keptHeartbeat.Timestamp))
			},
		},
		{
			name: "forwards heartbeats",
			setup: func(t *testing.T, state *testState) {
				// seed the ring with multiple controller IPs
				state.ctrl.ring.Add(fixture.ControllerIP)
				state.ctrl.ring.Add(fixture.ControllerIP2)

				// send multiple heartbeats for different functions
				state.heartbeats = []*function.Heartbeat{
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
					{Function: fixture.NewFunction(), Timestamp: time.Now()},
				}

				// ensure the controller sends the heartbeats to the other controllers
				state.mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*function.Heartbeat, forwardedFor ...string) error {
					// ensure the controller forwards the same heartbeats to the other controllers
					assert.DeepEqual(t, heartbeats, state.heartbeats)
					// ensure the controller forwards the list of controllers that have received heartbeats
					assert.DeepEqual(t, forwardedFor, []string{fixture.ControllerIP, fixture.ControllerIP2})
					return nil
				})
			},
			check: func(t *testing.T, state *testState) {
				// give the goroutine that forwards the heartbeats a chance to run
				time.Sleep(10 * time.Millisecond)
			},
		},
		{
			name: "deletes expired heartbeats",
			setup: func(t *testing.T, state *testState) {
				// seed the supervisor with an expired heartbeat from a different router
				fn := fixture.NewFunction()
				supervisor := state.ctrl.supervisor(fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP2, &function.Heartbeat{Function: fn, Timestamp: time.Now().Add(-(state.ctrl.config.HeartbeatTimeout + time.Second))})

				state.heartbeats = []*function.Heartbeat{
					{Function: fn, Timestamp: time.Now()},
				}
			},
			check: func(t *testing.T, state *testState) {
				sentHeartbeat := state.heartbeats[0]
				supervisor := state.ctrl.supervisor(sentHeartbeat.Function)

				// ensure the supervisor kept the sent heartbeat
				keptHeartbeat, ok := supervisor.routerHeartbeats.Load(fixture.RouterIP)
				assert.Assert(t, ok)
				assert.Assert(t, keptHeartbeat.Timestamp.Equal(sentHeartbeat.Timestamp))

				// ensure the expired heartbeat was deleted
				_, ok = supervisor.routerHeartbeats.Load(fixture.RouterIP2)
				assert.Assert(t, !ok)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := &testState{
				mcc: fixture.NewMockControllerClient(t),
			}
			state.ctrl = New(testConfig(), func(host string, port int) Client { return state.mcc }, fake.NewClientset(), nil)

			tc.setup(t, state)

			heartbeatBytes, err := json.Marshal(state.heartbeats)
			assert.NilError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewReader(heartbeatBytes))
			req.Header.Set(key.RouterIP.Header, fixture.RouterIP)
			rw := httptest.NewRecorder()
			state.ctrl.Handler().ServeHTTP(rw, req)

			assert.Assert(t, rw.Code == http.StatusOK)
			assert.Assert(t, rw.Body.Len() == 0)

			tc.check(t, state)
		})
	}
}
