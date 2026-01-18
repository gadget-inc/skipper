package function

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
)

type Scale struct {
	MinInstances           uint64 `json:"min_instances"`
	MaxInstances           uint64 `json:"max_instances"`
	TargetCPUUsageMilli    uint64 `json:"target_cpu_usage_milli"`
	TargetMemoryUsageMiB   uint64 `json:"target_memory_usage_mib"`
	TargetInFlightRequests uint64 `json:"target_in_flight_requests"`
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

// ScalingDecision contains the inputs and result of one scaling loop for one tenant
type ScalingDecision struct {
	DesiredInstances          int
	UnclampedDesiredInstances int
	Reason                    ScalingReason
	Metrics                   []ScalingMetric
}

var _ slog.LogValuer = ScalingDecision{}

// LogValue implements slog.LogValuer for structured logging.
func (sd ScalingDecision) LogValue() slog.Value {
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

// ScalingReason represents the reason for a scaling decision.
type ScalingReason string

const (
	ScalingReasonCPU              ScalingReason = "cpu"
	ScalingReasonHeartbeatTimeout ScalingReason = "heartbeat_timeout"
	ScalingReasonInFlightRequests ScalingReason = "in_flight_requests"
	ScalingReasonMemory           ScalingReason = "memory"
	ScalingReasonNoReadyInstances ScalingReason = "no ready instances"
	ScalingReasonUnknown          ScalingReason = "unknown"
)

// IsValidScalingReason returns true if the given string is a known scaling reason.
func IsValidScalingReason(reason string) bool {
	switch ScalingReason(reason) {
	case ScalingReasonCPU,
		ScalingReasonHeartbeatTimeout,
		ScalingReasonInFlightRequests,
		ScalingReasonMemory,
		ScalingReasonNoReadyInstances,
		ScalingReasonUnknown:
		return true
	default:
		return false
	}
}

// ScalingMetric represents an unclamped metric value for a specific metric observed for scaling decisions
type ScalingMetric struct {
	Name  string
	Value float64
}
