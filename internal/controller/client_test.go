package controller

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	"k8s.io/client-go/kubernetes/fake"
)

// clientSetup contains the common setup for testing both HTTP and gRPC clients.
type clientSetup struct {
	client  Client
	cleanup func()
}

// setupHTTPClient creates an HTTP test server and client.
func setupHTTPClient(t *testing.T, ctrl *Controller) clientSetup {
	server := httptest.NewServer(ctrl.Handler())
	client := NewHTTPClient(server.Listener.Addr().String(), 0)
	client.(*HTTPClient).addr = server.URL
	return clientSetup{
		client:  client,
		cleanup: server.Close,
	}
}

// setupGRPCClient creates a gRPC test server and client.
func setupGRPCClient(t *testing.T, ctrl *Controller) clientSetup {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)

	srv := grpc.NewServer()
	skipper.RegisterControllerServiceServer(srv, NewGRPCServer(ctrl))
	go func() {
		_ = srv.Serve(lis)
	}()

	addr := lis.Addr().(*net.TCPAddr)
	client, err := NewGRPCClient(addr.IP.String(), addr.Port)
	assert.NilError(t, err)

	return clientSetup{
		client: client,
		cleanup: func() {
			srv.Stop()
			client.(*GRPCClient).Close()
		},
	}
}

type clientFactory func(t *testing.T, ctrl *Controller) clientSetup

func TestClientInstance(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
		client         Client
		excludeNames   []string
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState, *skipper.Instance, error)
	}{
		{
			name: "returns assigned instance",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState, instance *skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, instance != nil)
				assert.Assert(t, proto.Equal(instance.GetFunction(), state.fn))
				assert.Assert(t, instance.HasReadyAt())
			},
		},
		{
			name: "assigns available pod",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState, instance *skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, instance != nil)
				assert.Assert(t, proto.Equal(instance.GetFunction(), state.fn))
			},
		},
		{
			name: "filters by excluded instance names",
			setup: func(t *testing.T, state *testState) {
				podA := fixture.NewAssignedPod(t, state.fn, nil)
				podA.Name = state.fn.GetDeployment() + "-a"
				podB := fixture.NewAssignedPod(t, state.fn, nil)
				podB.Name = state.fn.GetDeployment() + "-b"
				state.fakeKubernetes.Tracker().Add(podA)
				state.fakeKubernetes.Tracker().Add(podB)
				state.excludeNames = []string{state.fn.GetDeployment() + "-a"}
			},
			check: func(t *testing.T, state *testState, instance *skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, instance != nil)
				assert.Assert(t, instance.GetName() == state.fn.GetDeployment()+"-b")
			},
		},
		{
			name: "reverts to all instances when all excluded",
			setup: func(t *testing.T, state *testState) {
				podA := fixture.NewAssignedPod(t, state.fn, nil)
				podA.Name = state.fn.GetDeployment() + "-a"
				state.fakeKubernetes.Tracker().Add(podA)
				state.excludeNames = []string{state.fn.GetDeployment() + "-a"}
			},
			check: func(t *testing.T, state *testState, instance *skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, instance != nil)
				assert.Assert(t, instance.GetName() == state.fn.GetDeployment()+"-a")
			},
		},
		{
			name: "waits for eventual instance",
			setup: func(t *testing.T, state *testState) {
				go func() {
					time.Sleep(200 * time.Millisecond)
					state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				}()
			},
			check: func(t *testing.T, state *testState, instance *skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, instance != nil)
				assert.Assert(t, proto.Equal(instance.GetFunction(), state.fn))
			},
		},
	}

	protocols := []struct {
		name    string
		factory clientFactory
	}{
		{"HTTP", setupHTTPClient},
		{"gRPC", setupGRPCClient},
	}

	for _, proto := range protocols {
		t.Run(proto.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
					defer cancel()

					state := &testState{
						fn:             fixture.NewFunction(t),
						fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
					}
					state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

					tc.setup(t, state)

					err := state.ctrl.startInformers(ctx)
					assert.NilError(t, err)

					setup := proto.factory(t, state.ctrl)
					defer setup.cleanup()
					state.client = setup.client

					instance, err := state.client.Instance(ctx, state.fn, state.excludeNames...)
					tc.check(t, state, instance, err)
				})
			}
		})
	}
}

func TestClientScaleReasonHeaderSerialization(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		reason         skipper.ScaleReason
		expectedHeader string
	}{
		{
			name:           "in flight requests",
			reason:         skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS,
			expectedHeader: "SCALE_REASON_IN_FLIGHT_REQUESTS",
		},
		{
			name:           "cpu",
			reason:         skipper.ScaleReason_SCALE_REASON_CPU,
			expectedHeader: "SCALE_REASON_CPU",
		},
		{
			name:           "memory",
			reason:         skipper.ScaleReason_SCALE_REASON_MEMORY,
			expectedHeader: "SCALE_REASON_MEMORY",
		},
		{
			name:           "unspecified",
			reason:         skipper.ScaleReason_SCALE_REASON_UNSPECIFIED,
			expectedHeader: "SCALE_REASON_UNSPECIFIED",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedReasonHeader string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedReasonHeader = r.Header.Get(key.Reason.Header)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("[]")) // return empty instances array
			}))
			defer server.Close()

			// Parse the server URL to get host and port
			client := NewHTTPClient(server.Listener.Addr().String(), 0)
			// Override the addr since NewHTTPClient formats it with http:// prefix
			client.(*HTTPClient).addr = server.URL

			fn := fixture.NewFunction(t)
			_, err := client.Scale(t.Context(), fn, 1, tc.reason)
			assert.NilError(t, err)

			assert.Equal(t, capturedReasonHeader, tc.expectedHeader,
				"ScaleReason header should be the enum name string, not a control character. Got %q (len=%d), expected %q",
				capturedReasonHeader, len(capturedReasonHeader), tc.expectedHeader)
		})
	}
}

func TestClientHeartbeat(t *testing.T) {
	t.Parallel()

	type testState struct {
		ctrl       *Controller
		client     Client
		routerIP   string
		heartbeats []*skipper.Heartbeat
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState, error)
	}{
		{
			name: "single heartbeat",
			setup: func(t *testing.T, state *testState) {
				state.routerIP = fixture.RouterIP
				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, state *testState, err error) {
				assert.NilError(t, err)

				// Verify heartbeat was recorded
				sentHeartbeat := state.heartbeats[0]
				supervisor := state.ctrl.supervisor(sentHeartbeat.GetFunction())
				assert.Assert(t, supervisor.routerHeartbeats.Size() == 1)

				receivedHeartbeat, ok := supervisor.routerHeartbeats.Load(fixture.RouterIP)
				assert.Assert(t, ok)
				assert.Assert(t, receivedHeartbeat.GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))
			},
		},
		{
			name: "multiple heartbeats",
			setup: func(t *testing.T, state *testState) {
				state.routerIP = fixture.RouterIP
				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
					skipper.Heartbeat_builder{Function: fixture.NewFunction(t), Timestamp: timestamppb.Now()}.Build(),
				}
			},
			check: func(t *testing.T, state *testState, err error) {
				assert.NilError(t, err)

				// Verify all heartbeats were recorded
				assert.Assert(t, state.ctrl.supervisors.Size() == len(state.heartbeats))
				for _, sentHeartbeat := range state.heartbeats {
					supervisor := state.ctrl.supervisor(sentHeartbeat.GetFunction())
					assert.Assert(t, supervisor.routerHeartbeats.Size() == 1)

					receivedHeartbeat, ok := supervisor.routerHeartbeats.Load(fixture.RouterIP)
					assert.Assert(t, ok)
					assert.Assert(t, receivedHeartbeat.GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))
				}
			},
		},
		{
			name: "empty heartbeats returns early",
			setup: func(t *testing.T, state *testState) {
				state.routerIP = fixture.RouterIP
				state.heartbeats = []*skipper.Heartbeat{}
			},
			check: func(t *testing.T, state *testState, err error) {
				assert.NilError(t, err)
				// No supervisors should be created
				assert.Assert(t, state.ctrl.supervisors.Size() == 0)
			},
		},
		{
			name: "keeps most recent heartbeat",
			setup: func(t *testing.T, state *testState) {
				state.routerIP = fixture.RouterIP
				fn := fixture.NewFunction(t)

				// Seed supervisor with a recent heartbeat
				recentTime := time.Now()
				supervisor := state.ctrl.supervisor(fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{Function: fn, Timestamp: timestamppb.New(recentTime)}.Build())

				// Send an old heartbeat
				state.heartbeats = []*skipper.Heartbeat{
					skipper.Heartbeat_builder{Function: fn, Timestamp: timestamppb.New(recentTime.Add(-time.Hour))}.Build(),
				}
			},
			check: func(t *testing.T, state *testState, err error) {
				assert.NilError(t, err)

				sentHeartbeat := state.heartbeats[0]
				keptHeartbeat, ok := state.ctrl.supervisor(sentHeartbeat.GetFunction()).routerHeartbeats.Load(fixture.RouterIP)
				assert.Assert(t, ok)
				// Should have kept the more recent heartbeat, not the one we sent
				assert.Assert(t, !keptHeartbeat.GetTimestamp().AsTime().Equal(sentHeartbeat.GetTimestamp().AsTime()))
				assert.Assert(t, keptHeartbeat.GetTimestamp().AsTime().After(sentHeartbeat.GetTimestamp().AsTime()))
			},
		},
	}

	protocols := []struct {
		name    string
		factory clientFactory
	}{
		{"HTTP", setupHTTPClient},
		{"gRPC", setupGRPCClient},
	}

	for _, proto := range protocols {
		t.Run(proto.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					ctx := t.Context()

					state := &testState{}
					state.ctrl = New(testConfig(), nil, fake.NewClientset(), nil)

					tc.setup(t, state)

					setup := proto.factory(t, state.ctrl)
					defer setup.cleanup()
					state.client = setup.client

					err := state.client.Heartbeat(ctx, state.routerIP, state.heartbeats)
					tc.check(t, state, err)
				})
			}
		})
	}
}

// failingServer is a gRPC server that fails a configurable number of times before succeeding.
type failingServer struct {
	skipper.UnimplementedControllerServiceServer
	getInstanceFailures int32
	heartbeatFailures   int32
	scaleFailures       int32
	getInstanceCalls    atomic.Int32
	heartbeatCalls      atomic.Int32
	scaleCalls          atomic.Int32
}

func (s *failingServer) GetInstance(ctx context.Context, req *skipper.GetInstanceRequest) (*skipper.GetInstanceResponse, error) {
	call := s.getInstanceCalls.Add(1)
	if call <= s.getInstanceFailures {
		return nil, status.Error(codes.Unavailable, "simulated failure")
	}
	return &skipper.GetInstanceResponse{}, nil
}

func (s *failingServer) Heartbeat(ctx context.Context, req *skipper.HeartbeatRequest) (*skipper.HeartbeatResponse, error) {
	call := s.heartbeatCalls.Add(1)
	if call <= s.heartbeatFailures {
		return nil, status.Error(codes.Unavailable, "simulated failure")
	}
	return &skipper.HeartbeatResponse{}, nil
}

func (s *failingServer) Scale(ctx context.Context, req *skipper.ScaleRequest) (*skipper.ScaleResponse, error) {
	call := s.scaleCalls.Add(1)
	if call <= s.scaleFailures {
		return nil, status.Error(codes.Unavailable, "simulated failure")
	}
	return &skipper.ScaleResponse{}, nil
}

func TestGRPCClientRetries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                string
		getInstanceFailures int32
		heartbeatFailures   int32
		scaleFailures       int32
		expectGetInstance   bool
		expectHeartbeat     bool
		expectScale         bool
	}{
		{
			name:                "succeeds on first try",
			getInstanceFailures: 0,
			heartbeatFailures:   0,
			scaleFailures:       0,
			expectGetInstance:   true,
			expectHeartbeat:     true,
			expectScale:         true,
		},
		{
			name:                "GetInstance retries up to 2 failures (3 attempts)",
			getInstanceFailures: 2,
			heartbeatFailures:   0,
			scaleFailures:       0,
			expectGetInstance:   true,
			expectHeartbeat:     true,
			expectScale:         true,
		},
		{
			name:                "GetInstance fails after 3 failures (exceeds 3 attempts)",
			getInstanceFailures: 3,
			heartbeatFailures:   0,
			scaleFailures:       0,
			expectGetInstance:   false,
			expectHeartbeat:     true,
			expectScale:         true,
		},
		{
			name:                "Heartbeat retries up to 1 failure (2 attempts)",
			getInstanceFailures: 0,
			heartbeatFailures:   1,
			scaleFailures:       0,
			expectGetInstance:   true,
			expectHeartbeat:     true,
			expectScale:         true,
		},
		{
			name:                "Heartbeat fails after 2 failures (exceeds 2 attempts)",
			getInstanceFailures: 0,
			heartbeatFailures:   2,
			scaleFailures:       0,
			expectGetInstance:   true,
			expectHeartbeat:     false,
			expectScale:         true,
		},
		{
			name:                "Scale retries up to 2 failures (3 attempts)",
			getInstanceFailures: 0,
			heartbeatFailures:   0,
			scaleFailures:       2,
			expectGetInstance:   true,
			expectHeartbeat:     true,
			expectScale:         true,
		},
		{
			name:                "Scale fails after 3 failures (exceeds 3 attempts)",
			getInstanceFailures: 0,
			heartbeatFailures:   0,
			scaleFailures:       3,
			expectGetInstance:   true,
			expectHeartbeat:     true,
			expectScale:         false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			// Create failing server
			server := &failingServer{
				getInstanceFailures: tc.getInstanceFailures,
				heartbeatFailures:   tc.heartbeatFailures,
				scaleFailures:       tc.scaleFailures,
			}

			lis, err := net.Listen("tcp", "127.0.0.1:0")
			assert.NilError(t, err)

			srv := grpc.NewServer()
			skipper.RegisterControllerServiceServer(srv, server)
			go func() {
				_ = srv.Serve(lis)
			}()
			defer srv.Stop()

			addr := lis.Addr().(*net.TCPAddr)
			client, err := NewGRPCClient(addr.IP.String(), addr.Port)
			assert.NilError(t, err)
			defer client.(*GRPCClient).Close()

			fn := fixture.NewFunction(t)

			// Test GetInstance
			_, err = client.Instance(ctx, fn)
			if tc.expectGetInstance {
				assert.NilError(t, err, "GetInstance should succeed after retries")
			} else {
				assert.Assert(t, err != nil, "GetInstance should fail when retries exhausted")
			}

			// Test Heartbeat
			err = client.Heartbeat(ctx, fixture.RouterIP, []*skipper.Heartbeat{
				skipper.Heartbeat_builder{Function: fn, Timestamp: timestamppb.Now()}.Build(),
			})
			if tc.expectHeartbeat {
				assert.NilError(t, err, "Heartbeat should succeed after retries")
			} else {
				assert.Assert(t, err != nil, "Heartbeat should fail when retries exhausted")
			}

			// Test Scale
			_, err = client.Scale(ctx, fn, 1, skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS)
			if tc.expectScale {
				assert.NilError(t, err, "Scale should succeed after retries")
			} else {
				assert.Assert(t, err != nil, "Scale should fail when retries exhausted")
			}
		})
	}
}

func TestClientScale(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn               *skipper.Function
		fakeKubernetes   *fake.Clientset
		ctrl             *Controller
		client           Client
		desiredInstances uint32
		reason           skipper.ScaleReason
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState, []*skipper.Instance, error)
	}{
		{
			name: "returns scaled instances",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.desiredInstances = 1
				state.reason = skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, len(instances) == 1)
				assert.Assert(t, proto.Equal(instances[0].GetFunction(), state.fn))
			},
		},
		{
			name: "scale up",
			setup: func(t *testing.T, state *testState) {
				// Start with one assigned, one available
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				state.desiredInstances = 2
				state.reason = skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, len(instances) == 2)
			},
		},
		{
			name: "scale down",
			setup: func(t *testing.T, state *testState) {
				// Start with two assigned pods
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.desiredInstances = 1
				state.reason = skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, len(instances) == 1)
			},
		},
		{
			name: "cpu reason",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.desiredInstances = 1
				state.reason = skipper.ScaleReason_SCALE_REASON_CPU
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, len(instances) == 1)
			},
		},
		{
			name: "memory reason",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				state.desiredInstances = 1
				state.reason = skipper.ScaleReason_SCALE_REASON_MEMORY
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance, err error) {
				assert.NilError(t, err)
				assert.Assert(t, len(instances) == 1)
			},
		},
	}

	protocols := []struct {
		name    string
		factory clientFactory
	}{
		{"HTTP", setupHTTPClient},
		{"gRPC", setupGRPCClient},
	}

	for _, proto := range protocols {
		t.Run(proto.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
					defer cancel()

					state := &testState{
						fn:             fixture.NewFunction(t),
						fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
					}
					state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

					tc.setup(t, state)

					err := state.ctrl.startInformers(ctx)
					assert.NilError(t, err)

					setup := proto.factory(t, state.ctrl)
					defer setup.cleanup()
					state.client = setup.client

					instances, err := state.client.Scale(ctx, state.fn, state.desiredInstances, state.reason)
					tc.check(t, state, instances, err)
				})
			}
		})
	}
}
