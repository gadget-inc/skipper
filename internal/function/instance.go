package function

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

type Instance struct {
	Function
	Name        string    // pod name
	Addr        string    // pod ip : pod port
	ReplicaSet  string    // replica set name
	AssignedAt  time.Time // time the instance was assigned to the pod
	ReadyAt     time.Time // time the instance was ready to receive traffic
	CPUUsage    int       // cpu usage in millicores
	MemoryUsage int       // memory usage in bytes
}

func (instance *Instance) Fields() []slog.Attr {
	return []slog.Attr{
		key.Name.Field(instance.Name),
		key.Addr.Field(instance.Addr),
		key.AssignedAt.Field(instance.AssignedAt),
		key.ReadyAt.Field(instance.ReadyAt),
		key.Function.Field(instance.Function),
		key.CPUUsage.Field(instance.CPUUsage),
		key.MemoryUsage.Field(instance.MemoryUsage),
	}
}

func (instance *Instance) Attributes() []attribute.KeyValue {
	return append(key.Function.Attributes(instance.Function),
		key.Name.Attribute(instance.Name),
		key.Addr.Attribute(instance.Addr),
		attribute.String("replica_set", instance.ReplicaSet),
		key.AssignedAt.Attribute(instance.AssignedAt),
		key.ReadyAt.Attribute(instance.ReadyAt),
		key.CPUUsage.Attribute(instance.CPUUsage),
		key.MemoryUsage.Attribute(instance.MemoryUsage),
	)
}
