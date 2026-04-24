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
// and a fresh *Heartbeat per tick, both routed through telemetry.With.
//
// Baseline (Phase 1, bac5b83, Apple M4 Pro, -count=6 median):
//
//	BenchmarkConvergeTelemetry/FunctionAttr-14      1000000    1059 ns/op    2232 B/op    26 allocs/op
//	BenchmarkConvergeTelemetry/HeartbeatAttr-14     4414580     272 ns/op     448 B/op     9 allocs/op
//	BenchmarkConvergeTelemetry/Combined-14           713140    1720 ns/op    3800 B/op    41 allocs/op
//
// Phase 5 targets: FunctionAttr drops to 0 allocs/op after first call (cache
// hit); HeartbeatAttr drops >=50% (direct-to-OTel construction).
func BenchmarkConvergeTelemetry(b *testing.B) {
	fn := fixture.NewFunction(b)
	ctx := context.Background()

	b.Run("FunctionAttr", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkAttr = key.Function.Attr(fn)
		}
	})

	b.Run("HeartbeatAttr", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			hb := newBenchHeartbeat(fn)
			sinkAttr = key.Heartbeat.Attr(hb)
		}
	})

	b.Run("Combined", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			hb := newBenchHeartbeat(fn)
			sinkCtx = telemetry.With(ctx, key.Function.Attr(fn), key.Heartbeat.Attr(hb))
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
