package skipper

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
)

type Scale struct {
	MinInstances           uint32 `json:"min_instances"`
	MaxInstances           uint32 `json:"max_instances"`
	TargetCPUUsageMilli    uint32 `json:"target_cpu_usage_milli"`
	TargetMemoryUsageMiB   uint32 `json:"target_memory_usage_mib"`
	TargetInFlightRequests uint32 `json:"target_in_flight_requests"`
}

var _ slog.LogValuer = (*Scale)(nil)

func (s *Scale) LogValue() slog.Value {
	return slog.GroupValue(
		key.MinInstances.Slog(s.MinInstances),
		key.MaxInstances.Slog(s.MaxInstances),
		key.TargetCPUUsageMilli.Slog(s.TargetCPUUsageMilli),
		key.TargetMemoryUsageMiB.Slog(s.TargetMemoryUsageMiB),
		key.TargetInFlightRequests.Slog(s.TargetInFlightRequests),
	)
}

func (s *Scale) Equal(other *Scale) bool {
	if s == nil || other == nil {
		return s == other
	}
	return s.MinInstances == other.MinInstances &&
		s.MaxInstances == other.MaxInstances &&
		s.TargetCPUUsageMilli == other.TargetCPUUsageMilli &&
		s.TargetMemoryUsageMiB == other.TargetMemoryUsageMiB &&
		s.TargetInFlightRequests == other.TargetInFlightRequests
}

// ScaleDecision contains the inputs and result of one scaling loop for one tenant
type ScaleDecision struct {
	DesiredInstances          uint32        `json:"desired_instances"`
	UnclampedDesiredInstances uint32        `json:"unclamped_desired_instances"`
	Reason                    ScaleReason   `json:"reason"`
	Metrics                   []ScaleMetric `json:"metrics"`
}

var _ slog.LogValuer = ScaleDecision{}

// LogValue implements slog.LogValuer for structured logging.
func (sd ScaleDecision) LogValue() slog.Value {
	var metricAttrs []slog.Attr
	for _, metric := range sd.Metrics {
		metricAttrs = append(metricAttrs, slog.Float64(metric.Name, metric.Value))
	}

	return slog.GroupValue(
		key.DesiredInstances.Slog(sd.DesiredInstances),
		key.UnclampedDesiredInstances.Slog(sd.UnclampedDesiredInstances),
		key.Reason.Slog(string(sd.Reason)),
		slog.GroupAttrs("metrics", metricAttrs...),
	)
}

// ScaleReason represents the reason for a scaling decision.
type ScaleReason string

const (
	ScaleReasonCPU              ScaleReason = "CPU"
	ScaleReasonHeartbeatTimeout ScaleReason = "HEARTBEAT_TIMEOUT"
	ScaleReasonInFlightRequests ScaleReason = "IN_FLIGHT_REQUESTS"
	ScaleReasonMemory           ScaleReason = "MEMORY"
	ScaleReasonNoReadyInstances ScaleReason = "NO_READY_INSTANCES"
	ScaleReasonUnknown          ScaleReason = "UNKNOWN"
)

// IsValidScaleReason returns true if the given string is a known scale reason.
func IsValidScaleReason(reason string) bool {
	switch reason {
	case "CPU", "HEARTBEAT_TIMEOUT", "IN_FLIGHT_REQUESTS", "MEMORY", "NO_READY_INSTANCES", "UNKNOWN":
		return true
	default:
		return false
	}
}

// ScaleMetric represents an unclamped metric value for a specific metric observed for scaling decisions
type ScaleMetric struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}
