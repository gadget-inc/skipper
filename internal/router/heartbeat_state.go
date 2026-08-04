package router

import (
	"sync/atomic"
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// heartbeatState tracks in-flight requests and last activity for a function
// using atomic operations, avoiding per-request protobuf allocations. The
// proto is materialised only when the heartbeat sender needs to transmit.
type heartbeatState struct {
	fn         atomic.Pointer[skipper.Assignment]
	inFlight   atomic.Int32
	lastActive atomic.Int64 // UnixNano
}

func newHeartbeatState(fn *skipper.Assignment) *heartbeatState {
	s := &heartbeatState{}
	s.fn.Store(fn)
	s.lastActive.Store(time.Now().UnixNano())
	return s
}

// updateAssignment replaces the stored function if the new one differs. This
// prevents heartbeats from sending stale metadata after upstream changes.
func (s *heartbeatState) updateAssignment(fn *skipper.Assignment) {
	if s.fn.Load() != fn {
		s.fn.Store(fn)
	}
}

func (s *heartbeatState) touch() {
	s.lastActive.Store(time.Now().UnixNano())
}

func (s *heartbeatState) lastActiveTime() time.Time {
	return time.Unix(0, s.lastActive.Load())
}

func (s *heartbeatState) toProto() *skipper.Heartbeat {
	hb := &skipper.Heartbeat{}
	hb.SetAssignment(s.fn.Load())
	hb.SetTimestamp(timestamppb.New(s.lastActiveTime()))
	hb.SetInFlightRequests(uint32(max(s.inFlight.Load(), 0)))
	return hb
}
