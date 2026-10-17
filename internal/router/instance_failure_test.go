package router

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fixtureserver "github.com/gadget-inc/skipper/internal/cmd/fixture"
	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
	"gotest.tools/v3/assert"
)

// TestInstanceFailureBody pins the 502 body for each termination reason.
// Gadget's api parses these fields, so a change here is a contract change.
func TestInstanceFailureBody(t *testing.T) {
	t.Parallel()

	inst := skipper.Instance_builder{Name: new("operation-runner-node-22-15-7d9c4-x2k8p")}.Build()

	testCases := []struct {
		name        string
		termination *skipper.InstanceTermination
		want        string
	}{
		{
			name: "OOM kill",
			termination: skipper.InstanceTermination_builder{
				Reason:           skipper.TerminationReason_TERMINATION_REASON_OOM_KILLED.Enum(),
				Container:        new("operation-runner-node-22-15"),
				ExitCode:         new(int32(137)),
				MemoryLimitBytes: new(int64(6 << 30)),
			}.Build(),
			want: `{"error":"instance operation-runner-node-22-15-7d9c4-x2k8p stopped before it responded: container \"operation-runner-node-22-15\" ran out of memory and was killed at its 6Gi limit (exit code 137)","reason":"OOMKilled","instance":"operation-runner-node-22-15-7d9c4-x2k8p","container":"operation-runner-node-22-15","exitCode":137,"memoryLimitBytes":6442450944}`,
		},
		{
			name: "container exited cleanly",
			termination: skipper.InstanceTermination_builder{
				Reason:    skipper.TerminationReason_TERMINATION_REASON_CONTAINER_EXITED.Enum(),
				Container: new("operation-runner-node-22-15"),
				ExitCode:  new(int32(0)),
			}.Build(),
			want: `{"error":"instance operation-runner-node-22-15-7d9c4-x2k8p stopped before it responded: container \"operation-runner-node-22-15\" exited with code 0","reason":"ContainerExited","instance":"operation-runner-node-22-15-7d9c4-x2k8p","container":"operation-runner-node-22-15","exitCode":0}`,
		},
		{
			name: "pod deleted by skipper after a heartbeat timeout",
			termination: skipper.InstanceTermination_builder{
				Reason:       skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED.Enum(),
				TerminatedBy: new(skipper.TerminatedByHeartbeatTimeout),
			}.Build(),
			want: `{"error":"instance operation-runner-node-22-15-7d9c4-x2k8p stopped before it responded: skipper deleted the pod because its function's heartbeats timed out","reason":"PodTerminated","instance":"operation-runner-node-22-15-7d9c4-x2k8p","terminatedBy":"HeartbeatTimeout"}`,
		},
		{
			name: "pod evicted by the kubelet",
			termination: skipper.InstanceTermination_builder{
				Reason:       skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED.Enum(),
				TerminatedBy: new("TerminationByKubelet"),
				Message:      new("The node was low on resource: memory."),
			}.Build(),
			want: `{"error":"instance operation-runner-node-22-15-7d9c4-x2k8p stopped before it responded: the pod was terminated by TerminationByKubelet: The node was low on resource: memory.","reason":"PodTerminated","instance":"operation-runner-node-22-15-7d9c4-x2k8p","terminatedBy":"TerminationByKubelet"}`,
		},
		{
			name:        "no reason found",
			termination: &skipper.InstanceTermination{},
			want:        `{"error":"lost the connection to instance operation-runner-node-22-15-7d9c4-x2k8p before it responded (EOF), and Kubernetes reported no reason the pod stopped","reason":"Unknown","instance":"operation-runner-node-22-15-7d9c4-x2k8p"}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := json.Marshal(newInstanceFailure(inst, tc.termination, io.EOF))
			assert.NilError(t, err)
			assert.Equal(t, string(body), tc.want)
		})
	}
}

// newDyingInstance returns an instance that drops the connection without
// responding, the way an OOM-killed pod does.
func newDyingInstance(t *testing.T, fn *skipper.Function) *skipper.Instance {
	t.Helper()
	return fixture.NewInstance(t, fn, func(rw http.ResponseWriter, req *http.Request) {
		conn, _, err := http.NewResponseController(rw).Hijack()
		assert.Check(t, err)
		_ = conn.Close()
	})
}

func TestOneshotInstanceFailure(t *testing.T) {
	t.Parallel()

	oomKilled := skipper.InstanceTermination_builder{
		Reason:           skipper.TerminationReason_TERMINATION_REASON_OOM_KILLED.Enum(),
		Container:        new("main"),
		ExitCode:         new(int32(137)),
		MemoryLimitBytes: new(int64(6 << 30)),
	}.Build()

	type testState struct {
		fn       *skipper.Function
		instance *skipper.Instance
		mcc      *fixture.MockControllerClient
		released chan struct{}
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState, *httptest.ResponseRecorder, instanceFailure)
	}{
		{
			name: "reports why the instance stopped",
			setup: func(t *testing.T, state *testState) {
				state.mcc.HandleReleaseFailedInstance(func(ctx context.Context, inst *skipper.Instance) (*skipper.InstanceTermination, error) {
					assert.Equal(t, inst.GetName(), state.instance.GetName())
					return oomKilled, nil
				})
				// ReleaseInstance is not mocked: ReleaseFailedInstance already
				// released the pod, so a second release would fail the test.
			},
			check: func(t *testing.T, state *testState, rw *httptest.ResponseRecorder, body instanceFailure) {
				assert.Equal(t, body.Reason, instanceFailureReasonOOMKilled)
				assert.Equal(t, body.Instance, state.instance.GetName())
				assert.Equal(t, body.Container, "main")
				assert.Equal(t, *body.ExitCode, int32(137))
				assert.Equal(t, body.MemoryLimitBytes, int64(6<<30))
				assert.Assert(t, strings.Contains(body.Error, "ran out of memory"), "error: %s", body.Error)
			},
		},
		{
			name: "falls back to a plain release when the controller can't answer",
			setup: func(t *testing.T, state *testState) {
				state.mcc.HandleReleaseFailedInstance(func(ctx context.Context, inst *skipper.Instance) (*skipper.InstanceTermination, error) {
					return nil, errors.New("controller unavailable")
				})
				state.mcc.HandleReleaseInstance(func(ctx context.Context, inst *skipper.Instance) error {
					assert.Equal(t, inst.GetName(), state.instance.GetName())
					close(state.released)
					return nil
				})
			},
			check: func(t *testing.T, state *testState, rw *httptest.ResponseRecorder, body instanceFailure) {
				assert.Equal(t, body.Reason, instanceFailureReasonUnknown)
				assert.Equal(t, body.Instance, state.instance.GetName())
				assert.Assert(t, strings.Contains(body.Error, "lost the connection to instance"), "error: %s", body.Error)

				select {
				case <-state.released:
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for the fallback ReleaseInstance")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fn := fixture.NewFunction(t)
			fn.SetOneshot(true)

			state := &testState{
				fn:       fn,
				instance: newDyingInstance(t, fn),
				mcc:      fixture.NewMockControllerClient(t),
				released: make(chan struct{}),
			}
			state.mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
				return state.instance, nil
			})
			tc.setup(t, state)

			rw := httptest.NewRecorder()
			req := fixture.NewFunctionRequest(t, fn, http.MethodPost, "/?runCommand", strings.NewReader(`{"command":["yarn","build"]}`))
			New(testConfig(), state.mcc).ServeHTTP(rw, req)

			assert.Equal(t, rw.Code, http.StatusBadGateway)
			assert.Equal(t, rw.Header().Get("Content-Type"), "application/json; charset=utf-8")

			var body instanceFailure
			assert.NilError(t, json.Unmarshal(rw.Body.Bytes(), &body), "body: %s", rw.Body.String())
			tc.check(t, state, rw, body)
		})
	}
}

// TestStandardInstanceFailureKeepsEmptyBody verifies that a non-oneshot
// function still gets the reverse proxy's default empty 502, and the
// router doesn't ask the controller about the instance.
func TestStandardInstanceFailureKeepsEmptyBody(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	instance := newDyingInstance(t, fn)

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fn *skipper.Function, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return instance, nil
	})

	rw := httptest.NewRecorder()
	req := fixture.NewFunctionRequest(t, fn, http.MethodPost, "/", strings.NewReader("{}"))
	New(testConfig(), mcc).ServeHTTP(rw, req)

	assert.Equal(t, rw.Code, http.StatusBadGateway)
	assert.Equal(t, rw.Body.Len(), 0, "body: %s", rw.Body.String())
}

// TestOneshotInstanceFailureIntegration kills a real fixture container
// mid-request and checks that the router's 502 says so. Most of the
// response time is the kubelet noticing the exit, so the test logs it.
func TestOneshotInstanceFailureIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	fn := fixture.NewFixtureFunction(t)
	fn.SetOneshot(true)
	req := fixture.NewFunctionRequest(t, fn, http.MethodPost, fixture.RouterIntegrationURL()+fixtureserver.ExitPath, nil)

	start := time.Now()
	res, err := http.DefaultTransport.RoundTrip(req)
	assert.NilError(t, err)
	defer res.Body.Close()
	t.Logf("router answered %d after %s", res.StatusCode, time.Since(start))

	assert.Equal(t, res.StatusCode, http.StatusBadGateway)
	var body instanceFailure
	assert.NilError(t, json.UnmarshalRead(res.Body, &body))
	assert.Equal(t, body.Reason, instanceFailureReasonContainerExited, "body: %+v", body)
	assert.Equal(t, body.Container, "fixture")
	assert.Assert(t, body.ExitCode != nil, "body: %+v", body)
	assert.Equal(t, *body.ExitCode, int32(fixtureserver.ExitCode))
}
