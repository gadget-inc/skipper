package function

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
)

type Instance struct {
	*Function
	Name           string    `json:"Name"`           // pod name
	Addr           string    `json:"Addr"`           // pod ip : pod port
	ReplicaSet     string    `json:"ReplicaSet"`     // replica set name
	AssignedAt     time.Time `json:"AssignedAt"`     // time the instance was assigned to the pod
	ReadyAt        time.Time `json:"ReadyAt"`        // time the instance was ready to receive traffic
	CPUUsageMilli  uint64    `json:"CPUUsageMilli"`  // cpu usage in millicores
	MemoryUsageMiB uint64    `json:"MemoryUsageMiB"` // memory usage in MiB
}

// UnmarshalJSON implements custom unmarshaling to accept both PascalCase (legacy)
// and snake_case (new) field names for backward compatibility during migration.
func (i *Instance) UnmarshalJSON(data []byte) error {
	type instanceAlias struct {
		*Function
		// PascalCase (legacy)
		Name           string    `json:"Name"`
		Addr           string    `json:"Addr"`
		ReplicaSet     string    `json:"ReplicaSet"`
		AssignedAt     time.Time `json:"AssignedAt"`
		ReadyAt        time.Time `json:"ReadyAt"`
		CPUUsageMilli  uint64    `json:"CPUUsageMilli"`
		MemoryUsageMiB uint64    `json:"MemoryUsageMiB"`
		// snake_case (new) - use pointers for numerics to distinguish absent vs zero
		NameSnake           string    `json:"name"`
		AddrSnake           string    `json:"addr"`
		ReplicaSetSnake     string    `json:"replica_set"`
		AssignedAtSnake     time.Time `json:"assigned_at"`
		ReadyAtSnake        time.Time `json:"ready_at"`
		CPUUsageMilliSnake  *uint64   `json:"cpu_usage_milli"`
		MemoryUsageMiBSnake *uint64   `json:"memory_usage_mib"`
	}

	var alias instanceAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	i.Function = alias.Function

	// Prefer snake_case if present, otherwise use PascalCase
	if alias.NameSnake != "" {
		i.Name = alias.NameSnake
	} else {
		i.Name = alias.Name
	}

	if alias.AddrSnake != "" {
		i.Addr = alias.AddrSnake
	} else {
		i.Addr = alias.Addr
	}

	if alias.ReplicaSetSnake != "" {
		i.ReplicaSet = alias.ReplicaSetSnake
	} else {
		i.ReplicaSet = alias.ReplicaSet
	}

	if !alias.AssignedAtSnake.IsZero() {
		i.AssignedAt = alias.AssignedAtSnake
	} else {
		i.AssignedAt = alias.AssignedAt
	}

	if !alias.ReadyAtSnake.IsZero() {
		i.ReadyAt = alias.ReadyAtSnake
	} else {
		i.ReadyAt = alias.ReadyAt
	}

	if alias.CPUUsageMilliSnake != nil {
		i.CPUUsageMilli = *alias.CPUUsageMilliSnake
	} else {
		i.CPUUsageMilli = alias.CPUUsageMilli
	}

	if alias.MemoryUsageMiBSnake != nil {
		i.MemoryUsageMiB = *alias.MemoryUsageMiBSnake
	} else {
		i.MemoryUsageMiB = alias.MemoryUsageMiB
	}

	return nil
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
