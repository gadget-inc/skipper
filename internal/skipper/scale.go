package skipper

import (
	"log/slog"
	"strings"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
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

// UnmarshalJSON implements json.Unmarshaler, accepting both number and string-encoded integers.
func (s *Scale) UnmarshalJSON(data []byte) error {
	type Alias Scale
	// Try normal unmarshal first (for JSON numbers), then with StringifyNumbers (for string-encoded numbers)
	if err := json.Unmarshal(data, (*Alias)(s)); err == nil {
		return nil
	}
	return json.Unmarshal(data, (*Alias)(s), json.StringifyNumbers(true))
}

// ScaleDecision contains the inputs and result of one scaling loop for one tenant
type ScaleDecision struct {
	DesiredInstances          uint64        `json:"desired_instances"`
	UnclampedDesiredInstances uint64        `json:"unclamped_desired_instances"`
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

// UnmarshalJSON implements json.Unmarshaler, accepting both number and string-encoded integers.
func (sd *ScaleDecision) UnmarshalJSON(data []byte) error {
	type Alias ScaleDecision
	// Try normal unmarshal first (for JSON numbers), then with StringifyNumbers (for string-encoded numbers)
	if err := json.Unmarshal(data, (*Alias)(sd)); err == nil {
		return nil
	}
	return json.Unmarshal(data, (*Alias)(sd), json.StringifyNumbers(true))
}

// ScaleReason represents the reason for a scaling decision.
type ScaleReason string

const (
	// New UPPER_SNAKE_CASE values (protobuf style)
	ScaleReasonCPU              ScaleReason = "CPU"
	ScaleReasonHeartbeatTimeout ScaleReason = "HEARTBEAT_TIMEOUT"
	ScaleReasonInFlightRequests ScaleReason = "IN_FLIGHT_REQUESTS"
	ScaleReasonMemory           ScaleReason = "MEMORY"
	ScaleReasonNoReadyInstances ScaleReason = "NO_READY_INSTANCES"
	ScaleReasonUnknown          ScaleReason = "UNKNOWN"

	// Deprecated: use ScaleReason* constants instead
	ScalingReasonCPU              ScaleReason = "cpu"
	ScalingReasonHeartbeatTimeout ScaleReason = "heartbeat_timeout"
	ScalingReasonInFlightRequests ScaleReason = "in_flight_requests"
	ScalingReasonMemory           ScaleReason = "memory"
	ScalingReasonNoReadyInstances ScaleReason = "no ready instances"
	ScalingReasonUnknown          ScaleReason = "unknown"
)

// UnmarshalJSON implements json.Unmarshaler, accepting both old lowercase
// values and new UPPER_SNAKE_CASE values for backwards compatibility.
func (r *ScaleReason) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*r = NormalizeScaleReason(s)
	return nil
}

// IsValidScaleReason returns true if the given string is a known scale reason.
// Accepts both old lowercase and new UPPER_SNAKE_CASE values.
func IsValidScaleReason(reason string) bool {
	// Normalize to UPPER_SNAKE_CASE for comparison
	normalized := strings.ToUpper(strings.ReplaceAll(reason, " ", "_"))
	switch normalized {
	case "CPU", "HEARTBEAT_TIMEOUT", "IN_FLIGHT_REQUESTS", "MEMORY", "NO_READY_INSTANCES", "UNKNOWN":
		return true
	default:
		return false
	}
}

// NormalizeScaleReason normalizes a reason string to its canonical ScaleReason value.
// Accepts both old lowercase and new UPPER_SNAKE_CASE values.
func NormalizeScaleReason(reason string) ScaleReason {
	switch strings.ToUpper(strings.ReplaceAll(reason, " ", "_")) {
	case "CPU":
		return ScaleReasonCPU
	case "HEARTBEAT_TIMEOUT":
		return ScaleReasonHeartbeatTimeout
	case "IN_FLIGHT_REQUESTS":
		return ScaleReasonInFlightRequests
	case "MEMORY":
		return ScaleReasonMemory
	case "NO_READY_INSTANCES":
		return ScaleReasonNoReadyInstances
	case "UNKNOWN":
		return ScaleReasonUnknown
	default:
		return ScaleReason(reason) // preserve unknown values
	}
}

// ScaleMetric represents an unclamped metric value for a specific metric observed for scaling decisions
type ScaleMetric struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// UnmarshalJSON implements json.Unmarshaler, accepting both number and string-encoded numbers.
func (sm *ScaleMetric) UnmarshalJSON(data []byte) error {
	type Alias ScaleMetric
	// Try normal unmarshal first (for JSON numbers), then with StringifyNumbers (for string-encoded numbers)
	if err := json.Unmarshal(data, (*Alias)(sm)); err == nil {
		return nil
	}
	return json.Unmarshal(data, (*Alias)(sm), json.StringifyNumbers(true))
}
