package function

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
	"go.opentelemetry.io/otel/attribute"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

func (h *Heartbeat) Equal(other *Heartbeat) bool {
	return h.GetFunction().Equal(other.GetFunction()) &&
		h.GetTimestamp().AsTime().Equal(other.GetTimestamp().AsTime()) &&
		h.GetInFlightRequests() == other.GetInFlightRequests()
}

func (h *Heartbeat) MarshalJSON() ([]byte, error) {
	return json.Marshal(OldHeartbeat{
		Function:         *h.GetFunction(),
		Timestamp:        h.GetTimestamp().AsTime(),
		InFlightRequests: int(h.GetInFlightRequests()),
	})
}

func (h *Heartbeat) UnmarshalJSON(data []byte) error {
	var oldHeartbeat OldHeartbeat
	if err := json.Unmarshal(data, &oldHeartbeat); err != nil {
		return err
	}

	h.Reset()
	h.SetFunction(&oldHeartbeat.Function)
	h.SetTimestamp(timestamppb.New(oldHeartbeat.Timestamp))
	h.SetInFlightRequests(uint32(oldHeartbeat.InFlightRequests))

	return nil
}

func (h *Heartbeat) Fields() []slog.Attr {
	return []slog.Attr{
		key.Function.Field(h.GetFunction()),
		key.Timestamp.Field(h.GetTimestamp().AsTime()),
		key.InFlightRequests.Field(int(h.GetInFlightRequests())),
	}
}

func (h *Heartbeat) Attributes() []attribute.KeyValue {
	return append(
		key.Function.Attributes(h.GetFunction()),
		key.Timestamp.Attribute(h.GetTimestamp().AsTime()),
		key.InFlightRequests.Attribute(int(h.GetInFlightRequests())),
	)
}

func (h *Heartbeat) AttributesToNotPrefix() []string {
	return []string{"function"}
}
