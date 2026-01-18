package function

import (
	"encoding/binary"
	"errors"
	"hash/maphash"
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
)

// hashSeed is a fixed seed for deterministic hashing within a process.
var hashSeed = maphash.MakeSeed()

type Function struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	Tenant     string `json:"tenant"`
	Metadata   string `json:"metadata"`
	Scale      *Scale `json:"scale"`
}

// Hash is a unique identifier for a Function, suitable for use as a map key.
type Hash = uint64

// Hash returns a hash of all Function fields, suitable for use as a map key.
func (f *Function) Hash() Hash {
	var h maphash.Hash
	h.SetSeed(hashSeed)
	h.WriteString(f.Namespace)
	h.WriteByte(0) // null byte separator to prevent collisions (e.g., "ab"+"cd" vs "abc"+"d")
	h.WriteString(f.Deployment)
	h.WriteByte(0)
	h.WriteString(f.Tenant)
	h.WriteByte(0)
	h.WriteString(f.Metadata)
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], f.Scale.MinInstances)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], f.Scale.MaxInstances)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], f.Scale.TargetCPUUsageMilli)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], f.Scale.TargetMemoryUsageMiB)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], f.Scale.TargetInFlightRequests)
	h.Write(buf[:])
	return h.Sum64()
}

func (f *Function) RingKey() string {
	return f.Namespace + f.Deployment + f.Tenant
}

var _ slog.LogValuer = (*Function)(nil)

func (f *Function) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(f.Namespace),
		key.Deployment.Slog(f.Deployment),
		key.Tenant.Slog(f.Tenant),
		key.Metadata.Slog(f.Metadata),
		key.Scale.Slog(f.Scale),
	)
}

func (f *Function) Equal(other *Function) bool {
	if f == nil || other == nil {
		return f == other
	}
	return f.Namespace == other.Namespace &&
		f.Deployment == other.Deployment &&
		f.Tenant == other.Tenant &&
		f.Metadata == other.Metadata &&
		f.Scale.Equal(other.Scale)
}

func (f *Function) Validate() error {
	if f.Namespace == "" {
		return errors.New("missing namespace")
	}
	if f.Deployment == "" {
		return errors.New("missing deployment")
	}
	if f.Tenant == "" {
		return errors.New("missing tenant")
	}
	if f.Scale == nil {
		return errors.New("missing scale")
	}
	return nil
}
