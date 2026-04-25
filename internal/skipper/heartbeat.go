package skipper

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

var _ slog.LogValuer = (*Heartbeat)(nil)

const heartbeatName = "heartbeat"

// HeartbeatKey is the typed telemetry key for a Heartbeat. Heartbeats mutate
// per converge tick, so HeartbeatKey is uncached -- pointer-keyed memoization
// would never hit. WithOtel routes Attr's OTel construction directly to
// (*Heartbeat).otelAttrs so the converge hot path skips the slog.GroupValue
// -> appendOtelAttrs detour.
var HeartbeatKey = key.New(heartbeatName, (*Heartbeat).LogValue, key.WithOtel((*Heartbeat).otelAttrs))

var (
	heartbeatTimestampOtelKey        = attribute.Key(heartbeatName + "." + key.Timestamp.Name)
	heartbeatInFlightRequestsOtelKey = attribute.Key(heartbeatName + "." + key.InFlightRequests.Name)
)

func (h *Heartbeat) LogValue() slog.Value {
	return slog.GroupValue(
		key.Timestamp.Slog(h.GetTimestamp().AsTime()),
		key.InFlightRequests.Slog(h.GetInFlightRequests()),
	)
}

// otelAttrs builds OTel attributes directly, bypassing the slog -> OTel walk.
// Wired in via key.WithOtel on HeartbeatKey to keep the converge hot path
// allocation-light.
func (h *Heartbeat) otelAttrs() []attribute.KeyValue {
	return []attribute.KeyValue{
		{Key: heartbeatTimestampOtelKey, Value: attribute.StringValue(h.GetTimestamp().AsTime().Format(time.RFC3339))},
		{Key: heartbeatInFlightRequestsOtelKey, Value: attribute.Int64Value(int64(h.GetInFlightRequests()))},
	}
}
