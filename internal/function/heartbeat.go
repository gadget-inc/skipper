package function

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
)

type Heartbeat struct {
	Function         Function  `json:"function"`
	Timestamp        time.Time `json:"timestamp"`
	InFlightRequests int       `json:"in_flight_requests"`
}

var _ slog.LogValuer = Heartbeat{}

func (h Heartbeat) LogValue() slog.Value {
	return slog.GroupValue(
		key.Timestamp.Slog(h.Timestamp),
		key.InFlightRequests.Slog(h.InFlightRequests),
	)
}
