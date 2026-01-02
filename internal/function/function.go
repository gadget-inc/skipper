package function

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
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

var _ slog.LogValuer = Function{}

func (f Function) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(f.Namespace),
		key.Deployment.Slog(f.Deployment),
		key.Tenant.Slog(f.Tenant),
		key.Metadata.Slog(f.Metadata),
		key.Scale.Slog(f.Scale),
	)
}
