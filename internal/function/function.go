package function

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

type Function struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	Tenant     string `json:"tenant"`
	Metadata   string `json:"metadata"`
	Scale      Scale  `json:"scale"`
}

func (f Function) RingKey() string {
	return f.Namespace + f.Deployment + f.Tenant
}

func (f Function) Fields() []slog.Attr {
	return []slog.Attr{
		key.Namespace.Field(f.Namespace),
		key.Deployment.Field(f.Deployment),
		key.Tenant.Field(f.Tenant),
		key.Metadata.Field(f.Metadata),
		key.Scale.Field(f.Scale),
	}
}

func (f Function) Attributes() []attribute.KeyValue {
	return append([]attribute.KeyValue{
		key.Namespace.Attribute(f.Namespace),
		key.Deployment.Attribute(f.Deployment),
		key.Tenant.Attribute(f.Tenant),
		key.Metadata.Attribute(f.Metadata),
	}, key.Scale.Attributes(f.Scale)...)
}
