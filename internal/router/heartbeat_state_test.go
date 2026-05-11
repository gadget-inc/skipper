package router

import (
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
)

func TestHeartbeatStateConcurrentInFlight(t *testing.T) {
	t.Parallel()

	fn := fixture.NewAssignment(t)
	state := newHeartbeatState(fn)

	const goroutines = 100
	const iterations = 1000

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range iterations {
				state.inFlight.Add(1)
				state.inFlight.Add(-1)
			}
		})
	}
	wg.Wait()

	assert.Assert(t, state.inFlight.Load() == 0,
		"expected 0 in-flight after balanced inc/dec, got %d", state.inFlight.Load())
}

func TestHeartbeatStateLastActiveUpdated(t *testing.T) {
	t.Parallel()

	fn := fixture.NewAssignment(t)
	state := newHeartbeatState(fn)

	before := state.lastActiveTime()
	time.Sleep(time.Millisecond)
	state.touch()
	after := state.lastActiveTime()

	assert.Assert(t, after.After(before),
		"expected lastActive to advance after touch: before=%v after=%v", before, after)
}

func TestHeartbeatStateToProto(t *testing.T) {
	t.Parallel()

	fn := fixture.NewAssignment(t)
	state := newHeartbeatState(fn)

	state.inFlight.Add(3)
	state.touch()

	hb := state.toProto()

	assert.Assert(t, proto.Equal(hb.GetAssignment(), fn),
		"toProto function should match the original function")
	assert.Assert(t, hb.GetInFlightRequests() == 3,
		"expected 3 in-flight requests, got %d", hb.GetInFlightRequests())
	assert.Assert(t, !hb.GetTimestamp().AsTime().IsZero(),
		"expected non-zero timestamp")
	assert.Assert(t, time.Since(hb.GetTimestamp().AsTime()) < time.Second,
		"timestamp should be recent")
}

func TestHeartbeatStateUpdateAssignmentOnMetadataChange(t *testing.T) {
	t.Parallel()

	fn := fixture.NewAssignment(t)
	state := newHeartbeatState(fn)

	// Create an assignment with the same identity but different metadata.
	updatedFn := proto.Clone(fn).(*skipper.Assignment)
	updatedFn.SetMetadata("updated-metadata")

	// Precondition: same hash, different proto.
	assert.Equal(t, fn.Hash(), updatedFn.Hash())
	assert.Assert(t, !proto.Equal(fn, updatedFn))

	// Update the assignment on the heartbeat state.
	state.updateAssignment(updatedFn)

	// The heartbeat should now carry the updated assignment.
	hb := state.toProto()
	assert.Assert(t, proto.Equal(hb.GetAssignment(), updatedFn),
		"heartbeat should send the updated assignment, not the stale one")
}

func TestHeartbeatStateConcurrentUpdateAssignment(t *testing.T) {
	t.Parallel()

	fn := fixture.NewAssignment(t)
	state := newHeartbeatState(fn)

	updatedFn := proto.Clone(fn).(*skipper.Assignment)
	updatedFn.SetMetadata("concurrent-metadata")

	const goroutines = 100
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			state.updateAssignment(updatedFn)
			_ = state.toProto() // concurrent read
		})
	}
	wg.Wait()

	hb := state.toProto()
	assert.Assert(t, proto.Equal(hb.GetAssignment(), updatedFn))
}

func TestHeartbeatStateToProtoClampNegative(t *testing.T) {
	t.Parallel()

	fn := fixture.NewAssignment(t)
	state := newHeartbeatState(fn)

	// Simulate a race where decrement happens before the proto is built
	state.inFlight.Add(-1)

	hb := state.toProto()
	assert.Assert(t, hb.GetInFlightRequests() == 0,
		"negative in-flight should be clamped to 0, got %d", hb.GetInFlightRequests())
}
