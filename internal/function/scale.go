package function

import (
	"log/slog"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

type Scale struct {
	MinInstances         int `json:"min_instances"`
	MaxInstances         int `json:"max_instances"`
	TargetCPUUsageMilli  int `json:"target_cpu_usage_milli"`
	TargetMemoryUsageMiB int `json:"target_memory_usage_mib"`
}

func (s Scale) Fields() []slog.Attr {
	return []slog.Attr{
		key.MinInstances.Field(s.MinInstances),
		key.MaxInstances.Field(s.MaxInstances),
		key.TargetCPUUsageMilli.Field(s.TargetCPUUsageMilli),
		key.TargetMemoryUsageMiB.Field(s.TargetMemoryUsageMiB),
	}
}

func (s Scale) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		key.MinInstances.Attribute(s.MinInstances),
		key.MaxInstances.Attribute(s.MaxInstances),
		key.TargetCPUUsageMilli.Attribute(s.TargetCPUUsageMilli),
		key.TargetMemoryUsageMiB.Attribute(s.TargetMemoryUsageMiB),
	}
}
