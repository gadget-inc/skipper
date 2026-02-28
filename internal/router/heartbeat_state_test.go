package router

import (
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
)

func TestHeartbeatStateConcurrentInFlight(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
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

	fn := fixture.NewFunction(t)
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

	fn := fixture.NewFunction(t)
	state := newHeartbeatState(fn)

	state.inFlight.Add(3)
	state.touch()

	hb := state.toProto()

	assert.Assert(t, proto.Equal(hb.GetFunction(), fn),
		"toProto function should match the original function")
	assert.Assert(t, hb.GetInFlightRequests() == 3,
		"expected 3 in-flight requests, got %d", hb.GetInFlightRequests())
	assert.Assert(t, !hb.GetTimestamp().AsTime().IsZero(),
		"expected non-zero timestamp")
	assert.Assert(t, time.Since(hb.GetTimestamp().AsTime()) < time.Second,
		"timestamp should be recent")
}

func TestHeartbeatStateToProtoClampNegative(t *testing.T) {
	t.Parallel()

	fn := fixture.NewFunction(t)
	state := newHeartbeatState(fn)

	// Simulate a race where decrement happens before the proto is built
	state.inFlight.Add(-1)

	hb := state.toProto()
	assert.Assert(t, hb.GetInFlightRequests() == 0,
		"negative in-flight should be clamped to 0, got %d", hb.GetInFlightRequests())
}
