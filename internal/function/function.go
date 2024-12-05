package function

import (
	"log/slog"
	"strconv"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

var emptyFunction = Function{}

type Function struct {
	Tenant                  string `json:"tenant"`
	Metadata                string `json:"metadata"`
	Namespace               string `json:"namespace"`
	Deployment              string `json:"deployment"`
	MinInstances            int    `json:"minInstances"`
	MaxInstances            int    `json:"maxInstances"`
	TargetCPUUtilization    int    `json:"targetCPUUtilization"`
	TargetMemoryUtilization int    `json:"targetMemoryUtilization"`
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
		Tenant:                  tenant,
		Metadata:                metadata,
		Namespace:               namespace,
		Deployment:              deployment,
		MinInstances:            minInstances,
		MaxInstances:            maxInstances,
		TargetCPUUtilization:    targetCPUUtilization,
		TargetMemoryUtilization: targetMemoryUtilization,
	}, nil
}

func (f Function) RingKey() string {
	return f.Tenant +
		f.Namespace +
		f.Deployment +
		strconv.Itoa(f.MinInstances) +
		strconv.Itoa(f.MaxInstances) +
		strconv.Itoa(f.TargetCPUUtilization) +
		strconv.Itoa(f.TargetMemoryUtilization)
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
