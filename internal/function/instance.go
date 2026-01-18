package function

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
)

type Instance struct {
	*Function
	Name           string    // pod name
	Addr           string    // pod ip : pod port
	ReplicaSet     string    // replica set name
	AssignedAt     time.Time // time the instance was assigned to the pod
	ReadyAt        time.Time // time the instance was ready to receive traffic
	CPUUsageMilli  uint64    // cpu usage in millicores
	MemoryUsageMiB uint64    // memory usage in MiB
}

var _ slog.LogValuer = (*Instance)(nil)

func (instance *Instance) LogValue() slog.Value {
	return slog.GroupValue(
		key.Name.Slog(instance.Name),
		key.Addr.Slog(instance.Addr),
		key.ReplicaSet.Slog(instance.ReplicaSet),
		key.AssignedAt.Slog(instance.AssignedAt),
		key.ReadyAt.Slog(instance.ReadyAt),
		key.CPUUsageMilli.Slog(instance.CPUUsageMilli),
		key.MemoryUsageMiB.Slog(instance.MemoryUsageMiB),
	)
}

func (i *Instance) Equal(other *Instance) bool {
	if i == nil || other == nil {
		return i == other
	}
	return i.Function.Equal(other.Function) &&
		i.Name == other.Name &&
		i.Addr == other.Addr &&
		i.ReplicaSet == other.ReplicaSet &&
		i.AssignedAt.Equal(other.AssignedAt) &&
		i.ReadyAt.Equal(other.ReadyAt) &&
		i.CPUUsageMilli == other.CPUUsageMilli &&
		i.MemoryUsageMiB == other.MemoryUsageMiB
}
