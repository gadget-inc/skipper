package skipper

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
)

var _ slog.LogValuer = (*Instance)(nil)

func (instance *Instance) LogValue() slog.Value {
	return slog.GroupValue(
		key.Name.Slog(instance.GetName()),
		key.Addr.Slog(instance.GetAddr()),
		key.ReplicaSet.Slog(instance.GetReplicaSet()),
		key.AssignedAt.Slog(instance.GetAssignedAt().AsTime()),
		key.ReadyAt.Slog(instance.GetReadyAt().AsTime()),
		key.CPUUsageMilli.Slog(instance.GetCpuUsageMilli()),
		key.MemoryUsageMiB.Slog(instance.GetMemoryUsageMib()),
	)
}
