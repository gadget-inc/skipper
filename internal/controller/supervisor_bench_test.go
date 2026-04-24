package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	sinkAttr key.Attr
	sinkCtx  context.Context
)

// BenchmarkConvergeTelemetry measures the per-converge cost of building span
// attributes on the Supervisor.converge hot path: a cached-identity *Function
// and a per-tick *Heartbeat, both routed through telemetry.With.
//
// FunctionAttr and HeartbeatAttr isolate the Attr() cost by priming the
// pointer (Function cache hit, Heartbeat pre-built); Combined rebuilds the
// Heartbeat each iteration to reflect the full converge tick.
//
// Measured on Apple M4 Pro with -benchmem -count=6 medians. Baseline runs
// the former key.Function.Attr / key.Heartbeat.Attr path; "after" runs the
// weak-memoized fn.Attr() / direct-to-OTel hb.Attr() path.
//
//	Baseline:
//	  FunctionAttr-14         1000000    1141 ns/op    2232 B/op    26 allocs/op
//	  HeartbeatAttr-14        5003590     242 ns/op     320 B/op     7 allocs/op
//	  Combined-14              680533    1800 ns/op    3800 B/op    41 allocs/op
//
//	After:
//	  FunctionAttr-14        97354860    12.2 ns/op      0 B/op     0 allocs/op
//	  HeartbeatAttr-14       10018592     120 ns/op    232 B/op     3 allocs/op
//	  Combined-14             2598534     460 ns/op   1480 B/op    11 allocs/op
//
// Allocation reductions (the whole point -- GC was the bottleneck):
// FunctionAttr 26->0 (100%), HeartbeatAttr 7->3 (57%), Combined 41->11 (73%).
func BenchmarkConvergeTelemetry(b *testing.B) {
	fn := fixture.NewFunction(b)
	ctx := context.Background()

	b.Run("FunctionAttr", func(b *testing.B) {
		// Prime the cache so we measure the steady-state converge path where
		// the Function pointer has already been seen.
		_ = fn.Attr()
		b.ReportAllocs()
		for b.Loop() {
			sinkAttr = fn.Attr()
		}
	})

	b.Run("HeartbeatAttr", func(b *testing.B) {
		// Pre-build hb so the sub-bench isolates the Attr() cost from the
		// tick's message allocation (timestamppb + Heartbeat struct). The
		// Combined sub-bench below still rebuilds per iteration to reflect
		// the full converge loop.
		hb := newBenchHeartbeat(fn)
		b.ReportAllocs()
		for b.Loop() {
			sinkAttr = hb.Attr()
		}
	})

	b.Run("Combined", func(b *testing.B) {
		_ = fn.Attr()
		b.ReportAllocs()
		for b.Loop() {
			hb := newBenchHeartbeat(fn)
			sinkCtx = telemetry.With(ctx, fn.Attr(), hb.Attr())
		}
	})
}

func newBenchHeartbeat(fn *skipper.Function) *skipper.Heartbeat {
	hb := &skipper.Heartbeat{}
	hb.SetFunction(fn)
	hb.SetTimestamp(timestamppb.New(time.Unix(1700000000, 0)))
	hb.SetInFlightRequests(42)
	return hb
}
