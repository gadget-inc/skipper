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

// FunctionHash is a unique identifier for a Function, suitable for use as a map key.
type FunctionHash = uint64

// Hash returns a hash of all Function fields, suitable for use as a map key.
func (f *Function) Hash() FunctionHash {
	var h maphash.Hash
	h.SetSeed(hashSeed)
	h.WriteString(f.GetNamespace())
	h.WriteByte(0) // null byte separator to prevent collisions (e.g., "ab"+"cd" vs "abc"+"d")
	h.WriteString(f.GetDeployment())
	h.WriteByte(0)
	h.WriteString(f.GetTenant())
	h.WriteByte(0)
	h.WriteString(f.GetMetadata())
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetMinInstances())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetMaxInstances())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetCpuUsageMilli())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetMemoryUsageMib())
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], f.GetScale().GetTargetInFlightRequests())
	h.Write(buf[:])
	return h.Sum64()
}

func (f *Function) RingKey() string {
	return f.GetNamespace() + f.GetDeployment() + f.GetTenant()
}

var _ slog.LogValuer = (*Function)(nil)

func (f *Function) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(f.GetNamespace()),
		key.Deployment.Slog(f.GetDeployment()),
		key.Tenant.Slog(f.GetTenant()),
		key.Metadata.Slog(f.GetMetadata()),
		key.Scale.Slog(f.GetScale()),
	)
}

func (f *Function) Validate() error {
	if f.GetNamespace() == "" {
		return errors.New("missing namespace")
	}
	if f.GetDeployment() == "" {
		return errors.New("missing deployment")
	}
	if f.GetTenant() == "" {
		return errors.New("missing tenant")
	}
	if f.GetScale() == nil {
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
