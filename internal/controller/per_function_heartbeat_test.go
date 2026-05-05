package controller

import (
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

// silencedHeartbeat returns a heartbeat for fn whose timestamp is in the past
// by the supplied amount, simulating a function that has gone idle.
func silencedHeartbeat(fn *skipper.Function, idleFor time.Duration) *skipper.Heartbeat {
	return skipper.Heartbeat_builder{
		Function:  fn,
		Timestamp: timestamppb.New(time.Now().Add(-idleFor)),
	}.Build()
}

func withHeartbeatTimeout(fn *skipper.Function, timeout time.Duration) *skipper.Function {
	cloned := proto.Clone(fn).(*skipper.Function)
	cloned.SetHeartbeat(skipper.HeartbeatPolicy_builder{
		Timeout: durationpb.New(timeout),
	}.Build())
	return cloned
}

// TestCalculateDesiredInstancesPerFunctionHeartbeat verifies that two
// functions silenced for the same wall-clock duration but configured with
// different heartbeat.timeout values are scaled to zero on independent
// schedules: the function with the shorter timeout terminates first; the
// function with the longer timeout is still running.
func TestCalculateDesiredInstancesPerFunctionHeartbeat(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HeartbeatTimeout = 90 * time.Second // cluster default

	idleFor := 30 * time.Second

	shortTimeoutFn := withHeartbeatTimeout(fixture.NewFunction(t), 15*time.Second)
	longTimeoutFn := withHeartbeatTimeout(fixture.NewFunction(t), 5*time.Minute)

	shortDecision := calculateDesiredInstances(t.Context(), cfg, shortTimeoutFn, silencedHeartbeat(shortTimeoutFn, idleFor), nil)
	longDecision := calculateDesiredInstances(t.Context(), cfg, longTimeoutFn, silencedHeartbeat(longTimeoutFn, idleFor), nil)

	assert.Equal(t, shortDecision.GetReason(), skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT,
		"short timeout (%s) should scale to zero after %s of idle", shortTimeoutFn.HeartbeatTimeout(cfg.HeartbeatTimeout), idleFor)
	assert.Equal(t, shortDecision.GetDesiredInstances(), uint32(0))

	assert.Assert(t, longDecision.GetReason() != skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT,
		"long timeout (%s) should still be alive after %s of idle, got reason %s",
		longTimeoutFn.HeartbeatTimeout(cfg.HeartbeatTimeout), idleFor, longDecision.GetReason())
}

// TestCalculateDesiredInstancesOmittedHeartbeatPolicy verifies that a
// function omitting the heartbeat sub-message falls back to the cluster flag
// at the heartbeat-timeout decision site -- behavior matches today.
func TestCalculateDesiredInstancesOmittedHeartbeatPolicy(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HeartbeatTimeout = 90 * time.Second

	fn := fixture.NewFunction(t)
	assert.Assert(t, fn.GetHeartbeat() == nil, "fixture function must omit heartbeat policy")

	// Idle for less than the cluster default -- should not scale to zero.
	live := calculateDesiredInstances(t.Context(), cfg, fn, silencedHeartbeat(fn, 30*time.Second), nil)
	assert.Assert(t, live.GetReason() != skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT)

	// Idle past the cluster default -- should scale to zero.
	dead := calculateDesiredInstances(t.Context(), cfg, fn, silencedHeartbeat(fn, 2*time.Minute), nil)
	assert.Equal(t, dead.GetReason(), skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT)
	assert.Equal(t, dead.GetDesiredInstances(), uint32(0))
}

// TestSupervisorUpdateFunctionPicksUpHeartbeatTimeout exercises the existing
// CAS path in Supervisor.updateFunction with two functions that share an
// identity but differ only in heartbeat.timeout. The new value drives the
// next idle decision; no new pool is created.
func TestSupervisorUpdateFunctionPicksUpHeartbeatTimeout(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HeartbeatTimeout = 90 * time.Second

	base := fixture.NewFunction(t)

	requestA := withHeartbeatTimeout(base, 15*time.Second)
	requestB := withHeartbeatTimeout(base, 5*time.Minute)

	assert.Equal(t, requestA.Hash(), requestB.Hash(), "requests must share identity")
	assert.Assert(t, !proto.Equal(requestA, requestB), "requests must differ in spec")

	ctrl := New(cfg, nil, nil, nil)
	supervisor := &Supervisor{
		ctrl: ctrl,
	}
	supervisor.fn.Store(requestA)

	// Idle for 30s. Under requestA's 15s timeout this would scale to zero.
	idleHeartbeat := silencedHeartbeat(requestA, 30*time.Second)
	beforeUpdate := calculateDesiredInstances(t.Context(), cfg, supervisor.fn.Load(), idleHeartbeat, nil)
	assert.Equal(t, beforeUpdate.GetReason(), skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT,
		"requestA with 15s timeout should scale to zero after 30s idle")

	// requestB arrives with a 5-minute timeout, which is wider than the 30s
	// idle window, so the heartbeat-timeout reason should not fire.
	supervisor.updateFunction(requestB)
	assert.Assert(t, supervisor.fn.Load() == requestB, "updateFunction should swap to requestB via CAS")

	idleHeartbeat = silencedHeartbeat(requestB, 30*time.Second)
	afterUpdate := calculateDesiredInstances(t.Context(), cfg, supervisor.fn.Load(), idleHeartbeat, nil)
	assert.Assert(t, afterUpdate.GetReason() != skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT,
		"requestB with 5m timeout should not scale to zero after 30s idle, got %s", afterUpdate.GetReason())
}
