package skipper

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
)

type Heartbeat struct {
	Function         *Function `json:"function"`
	Timestamp        time.Time `json:"timestamp"`
	InFlightRequests uint32    `json:"in_flight_requests"`
}

var _ slog.LogValuer = (*Heartbeat)(nil)

func (h *Heartbeat) LogValue() slog.Value {
	return slog.GroupValue(
		key.Timestamp.Slog(h.Timestamp),
		key.InFlightRequests.Slog(h.InFlightRequests),
	)
}

func (h *Heartbeat) Equal(other *Heartbeat) bool {
	if h == nil || other == nil {
		return h == other
	}
	return h.Function.Equal(other.Function) &&
		h.Timestamp.Equal(other.Timestamp) &&
		h.InFlightRequests == other.InFlightRequests
}

func (h *Heartbeat) GetInFlightRequests() uint32 {
	if h == nil {
		return 0
	}
	return h.InFlightRequests
}

// MarshalJSON implements json.Marshaler.
func (h *Heartbeat) MarshalJSON() ([]byte, error) {
	type Alias Heartbeat
	return json.Marshal((*Alias)(h))
}

// UnmarshalJSON implements json.Unmarshaler, accepting both number and string-encoded integers.
func (h *Heartbeat) UnmarshalJSON(data []byte) error {
	type Alias Heartbeat
	// Try with StringifyNumbers first (for string-encoded numbers, the common case),
	// then without it (for JSON numbers, backwards compatibility)
	if err := json.Unmarshal(data, (*Alias)(h), json.StringifyNumbers(true)); err == nil {
		return nil
	}
	return json.Unmarshal(data, (*Alias)(h))
}
