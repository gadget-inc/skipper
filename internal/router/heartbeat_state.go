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
	fn         *skipper.Function
	inFlight   atomic.Int32
	lastActive atomic.Int64 // UnixNano
}

func newHeartbeatState(fn *skipper.Function) *heartbeatState {
	s := &heartbeatState{fn: fn}
	s.lastActive.Store(time.Now().UnixNano())
	return s
}

func (s *heartbeatState) touch() {
	s.lastActive.Store(time.Now().UnixNano())
}

func (s *heartbeatState) lastActiveTime() time.Time {
	return time.Unix(0, s.lastActive.Load())
}

func (s *heartbeatState) toProto() *skipper.Heartbeat {
	hb := &skipper.Heartbeat{}
	hb.SetFunction(s.fn)
	hb.SetTimestamp(timestamppb.New(s.lastActiveTime()))
	hb.SetInFlightRequests(uint32(max(s.inFlight.Load(), 0)))
	return hb
}
