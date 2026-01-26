package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()

	ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
	ctrl.Handler().ServeHTTP(rw, req)

	assert.Assert(t, rw.Code == http.StatusOK)
	assert.Assert(t, rw.Body.Len() == 0)
}

func TestHandleInstance(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		headers        map[string]string
		setFnHeader    bool
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
			name: "missing function header",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = false
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadRequest)
			},
		},
		{
			name: "invalid function header",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = false
				state.headers = map[string]string{
					key.Function.Header: "not-valid-json",
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadRequest)
			},
		},
		{
			name: "filters by excluded instance names",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				// add two ready instances
				podA := fixture.NewAssignedPod(t, state.fn, nil)
				podA.Name = state.fn.GetDeployment() + "-a"
				podB := fixture.NewAssignedPod(t, state.fn, nil)
				podB.Name = state.fn.GetDeployment() + "-b"
				state.fakeKubernetes.Tracker().Add(podA)
				state.fakeKubernetes.Tracker().Add(podB)
				state.headers = map[string]string{key.ExcludeInstanceNames.Header: state.fn.GetDeployment() + "-a"}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				instance := &skipper.Instance{}
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), instance))
				// ensure we did not receive the excluded one
				assert.Assert(t, instance.GetName() == state.fn.GetDeployment()+"-b")
			},
		},
		{
			name: "reverts to all instances when all instances on excluded list",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				// add multiple ready instances
				podA := fixture.NewAssignedPod(t, state.fn, nil)
				podA.Name = state.fn.GetDeployment() + "-a"
				podB := fixture.NewAssignedPod(t, state.fn, nil)
				podB.Name = state.fn.GetDeployment() + "-b"
				podC := fixture.NewAssignedPod(t, state.fn, nil)
				podC.Name = state.fn.GetDeployment() + "-c"
				state.fakeKubernetes.Tracker().Add(podA)
				state.fakeKubernetes.Tracker().Add(podB)
				state.fakeKubernetes.Tracker().Add(podC)
				state.headers = map[string]string{key.ExcludeInstanceNames.Header: state.fn.GetDeployment() + "-a," + state.fn.GetDeployment() + "-b," + state.fn.GetDeployment() + "-c"}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				instance := &skipper.Instance{}
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), instance))
				// should return one of the instances since we revert to unfiltered list
				assert.Assert(t, instance.GetName() == state.fn.GetDeployment()+"-a" || instance.GetName() == state.fn.GetDeployment()+"-b" || instance.GetName() == state.fn.GetDeployment()+"-c")
			},
		},
		{
			name: "smoke",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				instance := &skipper.Instance{}
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), instance))
				assert.Assert(t, proto.Equal(instance.GetFunction(), state.fn))
				assert.Assert(t, instance.HasReadyAt())
			},
		},
		{
			name: "unassigned with unassigned pod",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				instance := &skipper.Instance{}
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), instance))
				assert.Assert(t, proto.Equal(instance.GetFunction(), state.fn))
				assert.Assert(t, instance.HasReadyAt())
			},
		},
		{
			name: "unassigned with eventual unassigned pod",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				go func() {
					time.Sleep(500 * time.Millisecond)
					state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				}()
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				instance := &skipper.Instance{}
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), instance))
				assert.Assert(t, proto.Equal(instance.GetFunction(), state.fn))
				assert.Assert(t, instance.HasReadyAt())
			},
		},
		{
			name: "instances > max",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				state.fn.GetScale().SetMaxInstances(1) // ensure we can only have one instance

				// add max instances
				for range state.fn.GetScale().GetMaxInstances() {
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

				instance := &skipper.Instance{}
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), instance))
				assert.Assert(t, proto.Equal(instance.GetFunction(), state.fn))
				assert.Assert(t, instance.HasReadyAt())

				// ensure we didn't receive the earliest assigned at instance
				assert.Assert(t, instance.GetName() != "earliest-assigned-at")
			},
		},
		{
			name: "no ready instances while scaling up race",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
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

				instance := &skipper.Instance{}
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), instance))
				assert.Assert(t, proto.Equal(instance.GetFunction(), state.fn))
				assert.Assert(t, instance.HasReadyAt())

				// ensure we still have the 2 ready instances because we didn't scale down to 1
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
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
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}
			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

			tc.setup(t, state)

			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/instance", nil)
			if state.setFnHeader {
				state.fn.SetHeader(req)
			}
			for header, value := range state.headers {
				req.Header.Set(header, value)
			}
			state.rw = httptest.NewRecorder()
			state.ctrl.Handler().ServeHTTP(state.rw, req)

			tc.check(t, state)
		})
	}
}

func TestHandleScale(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		headers        map[string]string
		setFnHeader    bool
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
			name: "missing function header",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = false
				state.headers = map[string]string{
					key.DesiredInstances.Header: "1",
					key.Reason.Header:           skipper.ScaleReason_SCALE_REASON_UNSPECIFIED.String(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadRequest)
			},
		},
		{
			name: "invalid function header",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = false
				state.headers = map[string]string{
					key.Function.Header:         "not-valid-json",
					key.DesiredInstances.Header: "1",
					key.Reason.Header:           skipper.ScaleReason_SCALE_REASON_UNSPECIFIED.String(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadRequest)
			},
		},
		{
			name: "missing desired instances header",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				state.headers = map[string]string{
					key.Reason.Header: skipper.ScaleReason_SCALE_REASON_UNSPECIFIED.String(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadRequest)
			},
		},
		{
			name: "invalid desired instances header",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				state.headers = map[string]string{
					key.DesiredInstances.Header: "not-a-number",
					key.Reason.Header:           skipper.ScaleReason_SCALE_REASON_UNSPECIFIED.String(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadRequest)
			},
		},
		{
			name: "invalid scaling reason uses unknown",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				// add an assigned pod so scale succeeds
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.headers = map[string]string{
					key.DesiredInstances.Header: "1",
					key.Reason.Header:           "invalid-reason",
				}
			},
			check: func(t *testing.T, state *testState) {
				// should succeed since invalid reasons default to "unknown"
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instances []*skipper.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instances))
				assert.Assert(t, len(instances) == 1)
			},
		},
		{
			name: "smoke",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				// add an assigned pod
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.headers = map[string]string{
					key.DesiredInstances.Header: "1",
					key.Reason.Header:           skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS.String(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instances []*skipper.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instances))
				assert.Assert(t, len(instances) == 1)
				assert.Assert(t, proto.Equal(instances[0].GetFunction(), state.fn))
			},
		},
		{
			name: "scale up",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				// add one assigned pod and one available pod to scale up to
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				state.headers = map[string]string{
					key.DesiredInstances.Header: "2",
					key.Reason.Header:           skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS.String(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instances []*skipper.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instances))
				assert.Assert(t, len(instances) == 2)
			},
		},
		{
			name: "scale down",
			setup: func(t *testing.T, state *testState) {
				state.setFnHeader = true
				// add two assigned pods
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.headers = map[string]string{
					key.DesiredInstances.Header: "1",
					key.Reason.Header:           skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS.String(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)

				var instances []*skipper.Instance
				assert.NilError(t, json.Unmarshal(state.rw.Body.Bytes(), &instances))
				assert.Assert(t, len(instances) == 1)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}
			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

			tc.setup(t, state)

			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/scale", nil)
			if state.setFnHeader {
				state.fn.SetHeader(req)
			}
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
	t.Parallel()

	type testState struct {
		mcc                *fixture.MockControllerClient
		ctrl               *Controller
		heartbeats         []*skipper.Heartbeat
		rawBody            []byte
		setRouterIP        bool
		rw                 *httptest.ResponseRecorder
		heartbeatForwarded atomic.Bool
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "missing router IP header",
			setup: func(t *testing.T, state *testState) {
				state.setRouterIP = false
				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadRequest)
			},
		},
		{
			name: "invalid heartbeat body",
			setup: func(t *testing.T, state *testState) {
				state.setRouterIP = true
				state.rawBody = []byte("not-valid-json")
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusBadRequest)
			},
		},
		{
			name: "empty heartbeats array",
			setup: func(t *testing.T, state *testState) {
				state.setRouterIP = true
				state.heartbeats = []*skipper.Heartbeat{}
			},
			check: func(t *testing.T, state *testState) {
				// should succeed with no heartbeats to process
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.Len() == 0)
			},
		},
		{
			name: "receiving one heartbeat",
			setup: func(t *testing.T, state *testState) {
				state.setRouterIP = true
				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.Len() == 0)

				// ensure the supervisor has added a heartbeat
				sentHeartbeat := state.heartbeats[0]
				supervisor := state.ctrl.supervisor(sentHeartbeat.GetFunction())
				assert.Assert(t, supervisor.routerHeartbeats.Size() == 1)

				// ensure the heartbeat was associated with the expected router IP and has the expected timestamp
				receivedHeartbeat, ok := supervisor.routerHeartbeats.Load(fixture.RouterIP)
				assert.Assert(t, ok)
				assert.Assert(t, receivedHeartbeat.GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))
			},
		},
		{
			name: "receiving multiple heartbeats",
			setup: func(t *testing.T, state *testState) {
				state.setRouterIP = true
				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.Len() == 0)

				// ensure the controller has added both supervisors
				assert.Assert(t, state.ctrl.supervisors.Size() == len(state.heartbeats))
				for _, sentHeartbeat := range state.heartbeats {
					// ensure the supervisor has added the heartbeat
					supervisor := state.ctrl.supervisor(sentHeartbeat.GetFunction())
					assert.Assert(t, supervisor.routerHeartbeats.Size() == 1)

					// ensure the heartbeat was associated with the expected router IP and has the expected timestamp
					receivedHeartbeat, ok := supervisor.routerHeartbeats.Load(fixture.RouterIP)
					assert.Assert(t, ok)
					assert.Assert(t, receivedHeartbeat.GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))
				}
			},
		},
		{
			name: "keeps most recent",
			setup: func(t *testing.T, state *testState) {
				state.setRouterIP = true
				// seed the supervisor with a recent heartbeat
				fn := fixture.NewFunction(t)
				heartbeatTimestamp := time.Now()
				supervisor := state.ctrl.supervisor(fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{Function: fn, Timestamp: timestamppb.New(heartbeatTimestamp)}.Build())

				// send an old heartbeat
				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fn, Timestamp: timestamppb.New(heartbeatTimestamp.Add(-time.Hour))}.Build(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.Len() == 0)

				sentHeartbeat := state.heartbeats[0]
				keptHeartbeat, ok := state.ctrl.supervisor(sentHeartbeat.GetFunction()).routerHeartbeats.Load(fixture.RouterIP)

				// ensure the supervisor kept the heartbeat with the most recent timestamp
				assert.Assert(t, ok)
				assert.Assert(t, !keptHeartbeat.GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))
				assert.Assert(t, sentHeartbeat.GetTimestamp().AsTime().Add(time.Hour).Equal(keptHeartbeat.GetTimestamp().AsTime()))
			},
		},
		{
			name: "forwards heartbeats",
			setup: func(t *testing.T, state *testState) {
				state.setRouterIP = true
				// seed the ring with multiple controller IPs
				state.ctrl.ring.Add(fixture.ControllerIP)
				state.ctrl.ring.Add(fixture.ControllerIP2)

				// send multiple heartbeats for different functions
				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
				}

				// ensure the controller sends the heartbeats to the other controllers
				expectedHeartbeats := state.heartbeats
				state.mcc.HandleHeartbeat(func(ctx context.Context, routerIP string, heartbeats []*skipper.Heartbeat, forwardedFor ...string) error {
					state.heartbeatForwarded.Store(true)
					// ensure the controller forwards the same heartbeats to the other controllers
					assert.DeepEqual(t, heartbeats, expectedHeartbeats, protocmp.Transform())
					// ensure the controller forwards the list of controllers that have received heartbeats
					assert.DeepEqual(t, forwardedFor, []string{fixture.ControllerIP, fixture.ControllerIP2})
					return nil
				})
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.Len() == 0)

				// give the goroutine that forwards the heartbeats a chance to run
				time.Sleep(10 * time.Millisecond)

				// ensure the heartbeat callback was actually invoked
				assert.Assert(t, state.heartbeatForwarded.Load(), "heartbeat forwarding callback was never invoked")
			},
		},
		{
			name: "deletes expired heartbeats",
			setup: func(t *testing.T, state *testState) {
				state.setRouterIP = true
				// seed the supervisor with an expired heartbeat from a different router
				fn := fixture.NewFunction(t)
				supervisor := state.ctrl.supervisor(fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP2, skipper.Heartbeat_builder{Function: fn, Timestamp: timestamppb.New(time.Now().Add(-(state.ctrl.config.HeartbeatTimeout + time.Second)))}.Build())

				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fn, Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.rw.Code == http.StatusOK)
				assert.Assert(t, state.rw.Body.Len() == 0)

				sentHeartbeat := state.heartbeats[0]
				supervisor := state.ctrl.supervisor(sentHeartbeat.GetFunction())

				// ensure the supervisor kept the sent heartbeat
				keptHeartbeat, ok := supervisor.routerHeartbeats.Load(fixture.RouterIP)
				assert.Assert(t, ok)
				assert.Assert(t, keptHeartbeat.GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))

				// ensure the expired heartbeat was deleted
				_, ok = supervisor.routerHeartbeats.Load(fixture.RouterIP2)
				assert.Assert(t, !ok)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			state := &testState{
				mcc: fixture.NewMockControllerClient(t),
			}
			state.ctrl = New(testConfig(), func(host string, port int) Client { return state.mcc }, fake.NewClientset(), nil)

			tc.setup(t, state)

			var body []byte
			if state.rawBody != nil {
				body = state.rawBody
			} else {
				var err error
				body, err = json.Marshal(state.heartbeats)
				assert.NilError(t, err)
			}

			req := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewReader(body))
			if state.setRouterIP {
				req.Header.Set(key.RouterIP.Header, fixture.RouterIP)
			}
			state.rw = httptest.NewRecorder()
			state.ctrl.Handler().ServeHTTP(state.rw, req)

			tc.check(t, state)
		})
	}
}
