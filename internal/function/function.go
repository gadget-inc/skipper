package function

import (
	"log/slog"
	"strconv"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

var emptyFunction = Function{}

type Function struct {
	Deployment                 string `json:"deployment"`
	MaxInstances               int    `json:"maxInstances"`
	MaxInstancesStr            string `json:"maxInstancesStr"`
	Metadata                   string `json:"-"`
	MinInstances               int    `json:"minInstances"`
	MinInstancesStr            string `json:"minInstancesStr"`
	Namespace                  string `json:"namespace"`
	TargetCPUUtilization       int    `json:"targetCPUUtilization"`
	TargetCPUUtilizationStr    string `json:"targetCPUUtilizationStr"`
	TargetMemoryUtilization    int    `json:"targetMemoryUtilization"`
	TargetMemoryUtilizationStr string `json:"targetMemoryUtilizationStr"`
	Tenant                     string `json:"tenant"`
}

func new(
	deployment string,
	maxInstancesStr string,
	metadata string,
	minInstancesStr string,
	namespace string,
	targetCPUUtilizationStr string,
	targetMemoryUtilizationStr string,
	tenant string,
) (Function, error) {
	if tenant == "" {
		return emptyFunction, ErrMissingTenant
	}

	if namespace == "" {
		return emptyFunction, ErrMissingNamespace
	}

	if deployment == "" {
		return emptyFunction, ErrMissingDeployment
	}

	if maxInstancesStr == "" {
		return emptyFunction, ErrMissingMaxInstances
	}

	maxInstances, err := strconv.Atoi(maxInstancesStr)
	if err != nil {
		return emptyFunction, ErrInvalidMaxInstances
	}

	if minInstancesStr == "" {
		return emptyFunction, ErrMissingMinInstances
	}

	minInstances, err := strconv.Atoi(minInstancesStr)
	if err != nil {
		return emptyFunction, ErrInvalidMinInstances
	}

	if targetCPUUtilizationStr == "" {
		return emptyFunction, ErrMissingTargetCPUUtilization
	}

	targetCPUUtilization, err := strconv.Atoi(targetCPUUtilizationStr)
	if err != nil {
		return emptyFunction, ErrInvalidTargetCPUUtilization
	}

	if targetMemoryUtilizationStr == "" {
		return emptyFunction, ErrMissingTargetMemoryUtilization
	}

	targetMemoryUtilization, err := strconv.Atoi(targetMemoryUtilizationStr)
	if err != nil {
		return emptyFunction, ErrInvalidTargetMemoryUtilization
	}

	return Function{
		Deployment:                 deployment,
		MaxInstances:               maxInstances,
		MaxInstancesStr:            maxInstancesStr,
		Metadata:                   metadata,
		MinInstances:               minInstances,
		MinInstancesStr:            minInstancesStr,
		Namespace:                  namespace,
		TargetCPUUtilization:       targetCPUUtilization,
		TargetCPUUtilizationStr:    targetCPUUtilizationStr,
		TargetMemoryUtilization:    targetMemoryUtilization,
		TargetMemoryUtilizationStr: targetMemoryUtilizationStr,
		Tenant:                     tenant,
	}, nil
}

func (f Function) RingKey() string {
	return f.Tenant + ":" +
		f.Namespace + ":" +
		f.Deployment + ":" +
		f.MinInstancesStr + ":" +
		f.MaxInstancesStr + ":" +
		f.TargetCPUUtilizationStr + ":" +
		f.TargetMemoryUtilizationStr
}

func (f Function) Fields() []slog.Attr {
	return []slog.Attr{
		key.Tenant.Field(f.Tenant),
		// key.Namespace.Field(f.Namespace),
		// key.Deployment.Field(f.Deployment),
		// key.MinInstances.Field(f.MinInstances),
		// key.MaxInstances.Field(f.MaxInstances),
		// key.TargetCPUUtilization.Field(f.TargetCPUUtilization),
		// key.TargetMemoryUtilization.Field(f.TargetMemoryUtilization),
	}
}

func (f Function) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		key.Tenant.Attribute(f.Tenant),
		key.Namespace.Attribute(f.Namespace),
		key.Deployment.Attribute(f.Deployment),
		key.MinInstances.Attribute(f.MinInstances),
		key.MaxInstances.Attribute(f.MaxInstances),
		key.TargetCPUUtilization.Attribute(f.TargetCPUUtilization),
		key.TargetMemoryUtilization.Attribute(f.TargetMemoryUtilization),
	}
}
