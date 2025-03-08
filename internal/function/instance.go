package function

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/key"
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
	cpuUsage := "unknown"
	if instance.CPUUsage != 0 {
		cpuUsage = strconv.Itoa(instance.CPUUsage)
	}
	memoryUsage := "unknown"
	if instance.MemoryUsage != 0 {
		memoryUsage = strconv.Itoa(instance.MemoryUsage / 1024 / 1024)
	}
	return []slog.Attr{
		key.Name.Field(instance.Name),
		key.Addr.Field(instance.Addr),
		key.AssignedAt.Field(instance.AssignedAt),
		key.ReadyAt.Field(instance.ReadyAt),
		key.Function.Field(instance.Function),
		slog.String("cpu_usage", cpuUsage),
		slog.String("memory_usage", memoryUsage),
	}
}

func (instance *Instance) Attributes() []attribute.KeyValue {
	cpuUsage := "unknown"
	if instance.CPUUsage != 0 {
		cpuUsage = strconv.Itoa(instance.CPUUsage)
	}
	memoryUsage := "unknown"
	if instance.MemoryUsage != 0 {
		memoryUsage = strconv.Itoa(instance.MemoryUsage / 1024 / 1024)
	}
	return append(key.Function.Attributes(instance.Function),
		key.Name.Attribute(instance.Name),
		key.Addr.Attribute(instance.Addr),
		attribute.String("replica_set", instance.ReplicaSet),
		key.AssignedAt.Attribute(instance.AssignedAt),
		key.ReadyAt.Attribute(instance.ReadyAt),
		attribute.String("cpu_usage", cpuUsage),
		attribute.String("memory_usage", memoryUsage),
	)
}
