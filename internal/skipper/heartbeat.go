package skipper

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

var _ slog.LogValuer = (*Heartbeat)(nil)

// HeartbeatKey is the typed telemetry key for a Heartbeat. Heartbeats mutate
// per converge tick so pointer-keyed memoization would never hit; the OTel
// override (via OtelAttrs) lets the converge hot path skip the slog walk.
var HeartbeatKey = key.NewWithOtel("heartbeat", (*Heartbeat).LogValue, (*Heartbeat).OtelAttrs)

// Pre-computed OTel keys for the OtelAttrs path. The literal "heartbeat" is
// duplicated from HeartbeatKey above because referencing HeartbeatKey.Name
// here would create an init cycle (HeartbeatKey -> OtelAttrs body ->
// these keys -> HeartbeatKey).
var (
	heartbeatTimestampOtelKey        = attribute.Key("heartbeat." + key.Timestamp.Name)
	heartbeatInFlightRequestsOtelKey = attribute.Key("heartbeat." + key.InFlightRequests.Name)
)

func (h *Heartbeat) LogValue() slog.Value {
	return slog.GroupValue(
		key.Timestamp.Slog(h.GetTimestamp().AsTime()),
		key.InFlightRequests.Slog(h.GetInFlightRequests()),
	)
}

// OtelAttrs is consumed by HeartbeatKey via [key.NewWithOtel] as the direct
// otelOf path. Exported so the method value can be passed as an argument.
func (h *Heartbeat) OtelAttrs() []attribute.KeyValue {
	return []attribute.KeyValue{
		{Key: heartbeatTimestampOtelKey, Value: attribute.StringValue(h.GetTimestamp().AsTime().Format(time.RFC3339))},
		{Key: heartbeatInFlightRequestsOtelKey, Value: attribute.Int64Value(int64(h.GetInFlightRequests()))},
	}
}
