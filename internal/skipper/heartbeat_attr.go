package skipper

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

var (
	heartbeatTimestampOtelKey        = attribute.Key(key.Heartbeat.Name + "." + key.Timestamp.Name)
	heartbeatInFlightRequestsOtelKey = attribute.Key(key.Heartbeat.Name + "." + key.InFlightRequests.Name)
)

// Attr returns the telemetry Attr for this heartbeat, built directly against
// OTel so the converge hot path skips the slog.GroupValue -> appendOtelAttrs
// detour. The Slog field preserves the LogValue group shape so log output is
// unchanged.
func (h *Heartbeat) Attr() key.Attr {
	ts := h.GetTimestamp().AsTime()
	inFlight := h.GetInFlightRequests()

	return key.Attr{
		Slog: slog.Attr{
			Key: key.Heartbeat.Name,
			Value: slog.GroupValue(
				key.Timestamp.Slog(ts),
				key.InFlightRequests.Slog(inFlight),
			),
		},
		Otel: []attribute.KeyValue{
			{Key: heartbeatTimestampOtelKey, Value: attribute.StringValue(ts.Format(time.RFC3339))},
			{Key: heartbeatInFlightRequestsOtelKey, Value: attribute.Int64Value(int64(inFlight))},
		},
	}
}
