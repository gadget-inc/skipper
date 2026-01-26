package skipper

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/maphash"
	"log/slog"
	"net/http"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
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

// FunctionHash is a unique identifier for a Function, suitable for use as a map key.
type FunctionHash = uint64

// Hash returns a hash of all Function fields, suitable for use as a map key.
func (f *Function) Hash() FunctionHash {
	var h maphash.Hash
	h.SetSeed(hashSeed)
	h.WriteString(f.Namespace)
	h.WriteByte(0) // null byte separator to prevent collisions (e.g., "ab"+"cd" vs "abc"+"d")
	h.WriteString(f.Deployment)
	h.WriteByte(0)
	h.WriteString(f.Tenant)
	h.WriteByte(0)
	h.WriteString(f.Metadata)
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], f.Scale.MinInstances)
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.Scale.MaxInstances)
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.Scale.TargetCPUUsageMilli)
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.Scale.TargetMemoryUsageMiB)
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.Scale.TargetInFlightRequests)
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

func (f *Function) SetHeader(r *http.Request) {
	fnJSON, err := json.Marshal(f)
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("failed to marshal function: %w", err))
	}
	r.Header[key.Function.Header] = []string{string(fnJSON)}
}

func FunctionFromHeader(req *http.Request) (*Function, error) {
	fn := &Function{}

	header, ok := req.Header[key.Function.Header]
	if !ok || len(header) == 0 {
		return nil, errors.New("missing " + key.Function.Header)
	}

	err := json.Unmarshal([]byte(header[0]), fn)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s header: %w", key.Function.Header, err)
	}

	if err := fn.Validate(); err != nil {
		return nil, err
	}

	return fn, nil
}
