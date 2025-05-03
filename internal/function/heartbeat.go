package function

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

type Heartbeat struct {
	Function         *Function `json:"function"`
	Timestamp        time.Time `json:"timestamp"`
	InFlightRequests int       `json:"in_flight_requests"`
}

func (h *Heartbeat) Fields() []slog.Attr {
	return []slog.Attr{
		key.Function.Field(h.Function),
		key.Timestamp.Field(h.Timestamp),
		key.InFlightRequests.Field(h.InFlightRequests),
	}
}

func (h *Heartbeat) Attributes() []attribute.KeyValue {
	return append(
		key.Function.Attributes(h.Function),
		key.Timestamp.Attribute(h.Timestamp),
		key.InFlightRequests.Attribute(h.InFlightRequests),
	)
}

func (h *Heartbeat) AttributesToNotPrefix() []string {
	return []string{"function"}
}
