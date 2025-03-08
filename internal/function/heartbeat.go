package function

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

type Heartbeat struct {
	Function  Function  `json:"function"`
	Timestamp time.Time `json:"timestamp"`
	Requests  int       `json:"requests"`
}

func (h Heartbeat) Fields() []slog.Attr {
	return []slog.Attr{
		key.Function.Field(h.Function),
		key.Timestamp.Field(h.Timestamp),
		key.Requests.Field(h.Requests),
	}
}

func (h Heartbeat) Attributes() []attribute.KeyValue {
	return append(h.Function.Attributes(),
		key.Timestamp.Attribute(h.Timestamp),
		key.Requests.Attribute(h.Requests),
	)
}
