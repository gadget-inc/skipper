package skipper

import (
	"log/slog"
	"strings"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
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

// MarshalJSON implements json.Marshaler.
func (s *Scale) MarshalJSON() ([]byte, error) {
	type Alias Scale
	return json.Marshal((*Alias)(s))
}

// UnmarshalJSON implements json.Unmarshaler, accepting both number and string-encoded integers.
func (s *Scale) UnmarshalJSON(data []byte) error {
	type Alias Scale
	// Try with StringifyNumbers first (for string-encoded numbers, the common case),
	// then without it (for JSON numbers, backwards compatibility)
	if err := json.Unmarshal(data, (*Alias)(s), json.StringifyNumbers(true)); err == nil {
		return nil
	}
	return json.Unmarshal(data, (*Alias)(s))
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

// MarshalJSON implements json.Marshaler.
func (sd *ScaleDecision) MarshalJSON() ([]byte, error) {
	type Alias ScaleDecision
	return json.Marshal((*Alias)(sd))
}

// UnmarshalJSON implements json.Unmarshaler, accepting both number and string-encoded integers.
func (sd *ScaleDecision) UnmarshalJSON(data []byte) error {
	type Alias ScaleDecision
	// Try with StringifyNumbers first (for string-encoded numbers, the common case),
	// then without it (for JSON numbers, backwards compatibility)
	if err := json.Unmarshal(data, (*Alias)(sd), json.StringifyNumbers(true)); err == nil {
		return nil
	}
	return json.Unmarshal(data, (*Alias)(sd))
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

// scaleReasonPrefix is the protobuf enum prefix for ScaleReason values.
const scaleReasonPrefix = "SCALE_REASON_"

// NormalizeScaleReason normalizes a reason string to its canonical ScaleReason value.
// Accepts both protobuf-prefixed values (e.g., "SCALE_REASON_CPU") and
// non-prefixed values (e.g., "CPU").
func NormalizeScaleReason(reason string) ScaleReason {
	switch strings.TrimPrefix(reason, scaleReasonPrefix) {
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
		return ScaleReason(reason)
	}
}

// UnmarshalJSON implements json.Unmarshaler, accepting both protobuf-prefixed
// values (e.g., "SCALE_REASON_CPU") and non-prefixed values (e.g., "CPU").
func (r *ScaleReason) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*r = NormalizeScaleReason(s)
	return nil
}

// IsValidScaleReason returns true if the given string is a known scale reason.
// Accepts both protobuf-prefixed values (e.g., "SCALE_REASON_CPU") and
// non-prefixed values (e.g., "CPU").
func IsValidScaleReason(reason string) bool {
	switch strings.TrimPrefix(reason, scaleReasonPrefix) {
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

// MarshalJSON implements json.Marshaler.
// Floats are marshaled as JSON numbers to match protobuf behavior.
func (sm *ScaleMetric) MarshalJSON() ([]byte, error) {
	type Alias ScaleMetric
	return json.Marshal((*Alias)(sm))
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
