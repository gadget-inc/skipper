package router

import (
	"sync/atomic"
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// heartbeatState tracks in-flight requests and last activity for an assignment
// using atomic operations, avoiding per-request protobuf allocations. The
// proto is materialised only when the heartbeat sender needs to transmit.
type heartbeatState struct {
	assignment atomic.Pointer[skipper.Assignment]
	inFlight   atomic.Int32
	lastActive atomic.Int64 // UnixNano
}

func newHeartbeatState(a *skipper.Assignment) *heartbeatState {
	s := &heartbeatState{}
	s.assignment.Store(a)
	s.lastActive.Store(time.Now().UnixNano())
	return s
}

// updateAssignment replaces the stored assignment if the new one differs. This
// prevents heartbeats from sending stale metadata after upstream changes.
func (s *heartbeatState) updateAssignment(a *skipper.Assignment) {
	if s.assignment.Load() != a {
		s.assignment.Store(a)
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
	hb.SetAssignment(s.assignment.Load())
	hb.SetTimestamp(timestamppb.New(s.lastActiveTime()))
	hb.SetInFlightRequests(uint32(max(s.inFlight.Load(), 0)))
	return hb
}
