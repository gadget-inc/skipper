package skipper

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
)

var _ slog.LogValuer = (*Heartbeat)(nil)

func (h *Heartbeat) LogValue() slog.Value {
	return slog.GroupValue(
		key.Timestamp.Slog(h.GetTimestamp().AsTime()),
		key.InFlightRequests.Slog(h.GetInFlightRequests()),
	)
}
