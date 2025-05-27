package function

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

func (s *Scale) Fields() []slog.Attr {
	return []slog.Attr{
		key.MinInstances.Field(int(s.GetMinInstances())),
		key.MaxInstances.Field(int(s.GetMaxInstances())),
		key.TargetCPUUsageMilli.Field(int(s.GetTargetCpuUsageMilli())),
		key.TargetMemoryUsageMiB.Field(int(s.GetTargetMemoryUsageMib())),
		key.TargetInFlightRequests.Field(int(s.GetTargetInFlightRequests())),
	}
}

func (s *Scale) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		key.MinInstances.Attribute(int(s.GetMinInstances())),
		key.MaxInstances.Attribute(int(s.GetMaxInstances())),
		key.TargetCPUUsageMilli.Attribute(int(s.GetTargetCpuUsageMilli())),
		key.TargetMemoryUsageMiB.Attribute(int(s.GetTargetMemoryUsageMib())),
		key.TargetInFlightRequests.Attribute(int(s.GetTargetInFlightRequests())),
	}
}
