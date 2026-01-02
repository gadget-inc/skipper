package function

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
)

type Scale struct {
	MinInstances           int `json:"min_instances"`
	MaxInstances           int `json:"max_instances"`
	TargetCPUUsageMilli    int `json:"target_cpu_usage_milli"`
	TargetMemoryUsageMiB   int `json:"target_memory_usage_mib"`
	TargetInFlightRequests int `json:"target_in_flight_requests"`
}

var _ slog.LogValuer = Scale{}

func (s Scale) LogValue() slog.Value {
	return slog.GroupValue(
		key.MinInstances.Slog(s.MinInstances),
		key.MaxInstances.Slog(s.MaxInstances),
		key.TargetCPUUsageMilli.Slog(s.TargetCPUUsageMilli),
		key.TargetMemoryUsageMiB.Slog(s.TargetMemoryUsageMiB),
		key.TargetInFlightRequests.Slog(s.TargetInFlightRequests),
	)
}
