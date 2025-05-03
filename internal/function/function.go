package function

import (
	"encoding/binary"
	"hash/maphash"
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

type Function struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	Tenant     string `json:"tenant"`
	Metadata   string `json:"metadata"`
	Scale      *Scale `json:"scale"`
}

type Hash = uint64

var maphashSeed = maphash.MakeSeed()

func (f *Function) Hash() Hash {
	if f == nil {
		return 0
	}
	var h maphash.Hash
	h.SetSeed(maphashSeed)
	h.WriteString(f.Namespace)
	h.WriteString(f.Deployment)
	h.WriteString(f.Tenant)
	h.WriteString(f.Metadata)
	_ = binary.Write(&h, binary.BigEndian, uint32(f.Scale.MinInstances))
	_ = binary.Write(&h, binary.BigEndian, uint32(f.Scale.MaxInstances))
	_ = binary.Write(&h, binary.BigEndian, uint32(f.Scale.TargetCPUUsageMilli))
	_ = binary.Write(&h, binary.BigEndian, uint32(f.Scale.TargetMemoryUsageMiB))
	_ = binary.Write(&h, binary.BigEndian, uint32(f.Scale.TargetInFlightRequests))
	return h.Sum64()
}

func (f *Function) RingKey() string {
	return f.Namespace + f.Deployment + f.Tenant
}

func (f *Function) Clone() *Function {
	return &Function{
		Namespace:  f.Namespace,
		Deployment: f.Deployment,
		Tenant:     f.Tenant,
		Metadata:   f.Metadata,
		Scale:      f.Scale,
	}
}

func (f *Function) Fields() []slog.Attr {
	return []slog.Attr{
		key.Namespace.Field(f.Namespace),
		key.Deployment.Field(f.Deployment),
		key.Tenant.Field(f.Tenant),
		key.Metadata.Field(f.Metadata),
		key.Scale.Field(f.Scale),
	}
}

func (f *Function) Attributes() []attribute.KeyValue {
	return append(
		key.Scale.Attributes(f.Scale),
		key.Namespace.Attribute(f.Namespace),
		key.Deployment.Attribute(f.Deployment),
		key.Tenant.Attribute(f.Tenant),
		key.Metadata.Attribute(f.Metadata),
	)
}
