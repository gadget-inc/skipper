package skipper

import (
	"sort"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

func TestHeartbeatAttrEquivalence(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		hb   *Heartbeat
	}{
		{
			name: "populated",
			hb: Heartbeat_builder{
				Timestamp:        timestamppb.New(time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)),
				InFlightRequests: proto.Uint32(42),
			}.Build(),
		},
		{
			name: "zero in_flight",
			hb: Heartbeat_builder{
				Timestamp:        timestamppb.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
				InFlightRequests: proto.Uint32(0),
			}.Build(),
		},
		{
			name: "missing timestamp",
			hb: Heartbeat_builder{
				InFlightRequests: proto.Uint32(7),
			}.Build(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.hb.Attr()
			want := key.Heartbeat.Attr(tc.hb)

			// Slog must resolve to the same logged output.
			assert.Assert(t, got.Slog.Value.Resolve().Equal(want.Slog.Value.Resolve()),
				"Slog mismatch:\n got: %v\nwant: %v", got.Slog.Value.Resolve(), want.Slog.Value.Resolve())
			assert.Equal(t, got.Slog.Key, want.Slog.Key)

			// Otel: compare order-insensitive since the source doesn't guarantee order.
			gotOtel := append([]attribute.KeyValue(nil), got.Otel...)
			wantOtel := append([]attribute.KeyValue(nil), want.Otel...)
			sort.Slice(gotOtel, func(i, j int) bool { return gotOtel[i].Key < gotOtel[j].Key })
			sort.Slice(wantOtel, func(i, j int) bool { return wantOtel[i].Key < wantOtel[j].Key })
			assert.DeepEqual(t, gotOtel, wantOtel, cmpopts.EquateComparable(attribute.KeyValue{}))
		})
	}
}

func BenchmarkHeartbeatAttr(b *testing.B) {
	hb := Heartbeat_builder{
		Timestamp:        timestamppb.New(time.Unix(1700000000, 0)),
		InFlightRequests: proto.Uint32(42),
	}.Build()

	b.Run("method", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkAttrResult = hb.Attr()
		}
	})

	b.Run("key_based", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkAttrResult = key.Heartbeat.Attr(hb)
		}
	})
}
