package function

import (
	"encoding/binary"
	"hash/maphash"
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Hash = uint64

var maphashSeed = maphash.MakeSeed()

func (f *Function) Hash() Hash {
	if f == nil {
		return 0
	}
	var h maphash.Hash
	h.SetSeed(maphashSeed)
	h.WriteString(f.GetNamespace())
	h.WriteString(f.GetDeployment())
	h.WriteString(f.GetTenant())
	h.WriteString(f.GetMetadata())
	_ = binary.Write(&h, binary.BigEndian, f.GetScale().GetMinInstances())
	_ = binary.Write(&h, binary.BigEndian, f.GetScale().GetMaxInstances())
	_ = binary.Write(&h, binary.BigEndian, f.GetScale().GetTargetCpuUsageMilli())
	_ = binary.Write(&h, binary.BigEndian, f.GetScale().GetTargetMemoryUsageMib())
	_ = binary.Write(&h, binary.BigEndian, f.GetScale().GetTargetInFlightRequests())
	return h.Sum64()
}

func (f *Function) Equal(other *Function) bool {
	return f.Hash() == other.Hash()
}

func (f *Function) RingKey() string {
	return f.GetNamespace() + f.GetDeployment() + f.GetTenant()
}

func (f *Function) Clone() *Function {
	return proto.Clone(f).(*Function)
}

func (f *Function) Fields() []slog.Attr {
	return []slog.Attr{
		key.Namespace.Field(f.GetNamespace()),
		key.Deployment.Field(f.GetDeployment()),
		key.Tenant.Field(f.GetTenant()),
		key.Metadata.Field(f.GetMetadata()),
		key.Scale.Field(f.GetScale()),
	}
}

func (f *Function) Attributes() []attribute.KeyValue {
	return append(
		key.Scale.Attributes(f.GetScale()),
		key.Namespace.Attribute(f.GetNamespace()),
		key.Deployment.Attribute(f.GetDeployment()),
		key.Tenant.Attribute(f.GetTenant()),
		key.Metadata.Attribute(f.GetMetadata()),
	)
}

func (f *Function) MarshalJSON() ([]byte, error) {
	return protojson.MarshalOptions{
		UseProtoNames:   true,
		UseEnumNumbers:  true,
		EmitUnpopulated: false,
	}.Marshal(f)
}

func (f *Function) UnmarshalJSON(data []byte) error {
	return protojson.Unmarshal(data, f)
}
