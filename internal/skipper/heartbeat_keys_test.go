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

// TestHeartbeatKeyEquivalence pins HeartbeatKey's WithOtel-driven Attr to the
// canonical (slog-walk) construction path so the direct-to-OTel optimization
// cannot silently drift the span attribute output.
func TestHeartbeatKeyEquivalence(t *testing.T) {
	t.Parallel()

	uncached := key.New("heartbeat", (*Heartbeat).LogValue)

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

			got := HeartbeatKey.Attr(tc.hb)
			want := uncached.Attr(tc.hb)

			assert.Assert(t, got.Slog.Value.Resolve().Equal(want.Slog.Value.Resolve()),
				"Slog mismatch:\n got: %v\nwant: %v", got.Slog.Value.Resolve(), want.Slog.Value.Resolve())
			assert.Equal(t, got.Slog.Key, want.Slog.Key)

			// Otel order isn't guaranteed by either path -- compare order-insensitive.
			gotOtel := append([]attribute.KeyValue(nil), got.Otel...)
			wantOtel := append([]attribute.KeyValue(nil), want.Otel...)
			sort.Slice(gotOtel, func(i, j int) bool { return gotOtel[i].Key < gotOtel[j].Key })
			sort.Slice(wantOtel, func(i, j int) bool { return wantOtel[i].Key < wantOtel[j].Key })
			assert.DeepEqual(t, gotOtel, wantOtel, cmpopts.EquateComparable(attribute.KeyValue{}))
		})
	}
}

func BenchmarkHeartbeatKeyAttr(b *testing.B) {
	hb := Heartbeat_builder{
		Timestamp:        timestamppb.New(time.Unix(1700000000, 0)),
		InFlightRequests: proto.Uint32(42),
	}.Build()

	b.ReportAllocs()
	for b.Loop() {
		sinkAttrResult = HeartbeatKey.Attr(hb)
	}
}
