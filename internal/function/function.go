package function

import (
	"log/slog"
	"strconv"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

type Function struct {
	Namespace               string `json:"namespace"`
	Deployment              string `json:"deployment"`
	Tenant                  string `json:"tenant"`
	Metadata                string `json:"metadata"`
	MinInstances            int    `json:"min_instances"`
	MaxInstances            int    `json:"max_instances"`
	TargetCPUUtilization    int    `json:"target_cpu_utilization"`
	TargetMemoryUtilization int    `json:"target_memory_utilization"`
}

func (f Function) RingKey() string {
	return f.Tenant +
		f.Namespace +
		f.Deployment +
		strconv.Itoa(f.MinInstances) +
		strconv.Itoa(f.MaxInstances) +
		strconv.Itoa(f.TargetCPUUtilization) +
		strconv.Itoa(f.TargetMemoryUtilization)
}

func (f Function) Fields() []slog.Attr {
	return []slog.Attr{
		key.Tenant.Field(f.Tenant),
		// key.Namespace.Field(f.Namespace),
		// key.Deployment.Field(f.Deployment),
		// key.MinInstances.Field(f.MinInstances),
		// key.MaxInstances.Field(f.MaxInstances),
		// key.TargetCPUUtilization.Field(f.TargetCPUUtilization),
		// key.TargetMemoryUtilization.Field(f.TargetMemoryUtilization),
	}
}

func (f Function) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		key.Tenant.Attribute(f.Tenant),
		key.Namespace.Attribute(f.Namespace),
		key.Deployment.Attribute(f.Deployment),
		key.MinInstances.Attribute(f.MinInstances),
		key.MaxInstances.Attribute(f.MaxInstances),
		key.TargetCPUUtilization.Attribute(f.TargetCPUUtilization),
		key.TargetMemoryUtilization.Attribute(f.TargetMemoryUtilization),
	}
}
