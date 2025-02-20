package function

import (
	"log/slog"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

type Scale struct {
	MinInstances      int `json:"min_instances"`
	MaxInstances      int `json:"max_instances"`
	TargetCPUUsage    int `json:"target_cpu_usage"`    // in millicores
	TargetMemoryUsage int `json:"target_memory_usage"` // in MiB
}

func (s Scale) Fields() []slog.Attr {
	return []slog.Attr{
		key.MinInstances.Field(s.MinInstances),
		key.MaxInstances.Field(s.MaxInstances),
		key.TargetCPUUsage.Field(s.TargetCPUUsage),
		key.TargetMemoryUsage.Field(s.TargetMemoryUsage),
	}
}

func (s Scale) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		key.MinInstances.Attribute(s.MinInstances),
		key.MaxInstances.Attribute(s.MaxInstances),
		key.TargetCPUUsage.Attribute(s.TargetCPUUsage),
		key.TargetMemoryUsage.Attribute(s.TargetMemoryUsage),
	}
}
