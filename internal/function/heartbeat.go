package function

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
)

type Heartbeat struct {
	Function         *Function `json:"function"`
	Timestamp        time.Time `json:"timestamp"`
	InFlightRequests int       `json:"in_flight_requests"`
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

func (h *Heartbeat) GetInFlightRequests() int {
	if h == nil {
		return 0
	}
	return h.InFlightRequests
}
