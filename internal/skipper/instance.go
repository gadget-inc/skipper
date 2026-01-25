package skipper

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
)

type Instance struct {
	Function       *Function `json:"function"`
	Name           string    `json:"name"`             // pod name
	Addr           string    `json:"addr"`             // pod ip : pod port
	ReplicaSet     string    `json:"replica_set"`      // replica set name
	AssignedAt     time.Time `json:"assigned_at"`      // time the instance was assigned to the pod
	ReadyAt        time.Time `json:"ready_at"`         // time the instance was ready to receive traffic
	CPUUsageMilli  uint64    `json:"cpu_usage_milli"`  // cpu usage in millicores
	MemoryUsageMiB uint64    `json:"memory_usage_mib"` // memory usage in MiB
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

// MarshalJSON implements json.Marshaler, encoding integers as strings for protobuf compatibility.
func (i *Instance) MarshalJSON() ([]byte, error) {
	type Alias Instance
	return json.Marshal((*Alias)(i), json.StringifyNumbers(true))
}

// UnmarshalJSON implements json.Unmarshaler, accepting both number and string-encoded integers.
func (i *Instance) UnmarshalJSON(data []byte) error {
	type Alias Instance
	// Try normal unmarshal first (for JSON numbers), then with StringifyNumbers (for string-encoded numbers)
	if err := json.Unmarshal(data, (*Alias)(i)); err == nil {
		return nil
	}
	return json.Unmarshal(data, (*Alias)(i), json.StringifyNumbers(true))
}
