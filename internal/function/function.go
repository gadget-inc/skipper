package function

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
	v1 "k8s.io/api/core/v1"
)

type Function struct {
	Deployment                 string `json:"deployment"`
	MaxReplicas                int    `json:"maxReplicas"`
	MaxReplicasStr             string `json:"maxReplicasStr"`
	Metadata                   string `json:"-"`
	MinReplicas                int    `json:"minReplicas"`
	MinReplicasStr             string `json:"minReplicasStr"`
	Namespace                  string `json:"namespace"`
	TargetCPUUtilization       int    `json:"targetCPUUtilization"`
	TargetCPUUtilizationStr    string `json:"targetCPUUtilizationStr"`
	TargetMemoryUtilization    int    `json:"targetMemoryUtilization"`
	TargetMemoryUtilizationStr string `json:"targetMemoryUtilizationStr"`
	Tenant                     string `json:"tenant"`
}

type Instance struct {
	Function
	Pod        *v1.Pod
	ReplicaSet string
	AssignedAt time.Time
	ReadyAt    time.Time
}

var (
	emptyFunction = Function{}
	emptyInstance = Instance{Function: emptyFunction}
)

func new(
	deployment string,
	maxReplicasStr string,
	metadata string,
	minReplicasStr string,
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

	if maxReplicasStr == "" {
		return emptyFunction, ErrMissingMaxReplicas
	}

	maxReplicas, err := strconv.Atoi(maxReplicasStr)
	if err != nil {
		return emptyFunction, ErrInvalidMaxReplicas
	}

	if minReplicasStr == "" {
		return emptyFunction, ErrMissingMinReplicas
	}

	minReplicas, err := strconv.Atoi(minReplicasStr)
	if err != nil {
		return emptyFunction, ErrInvalidMinReplicas
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
		MaxReplicas:                maxReplicas,
		MaxReplicasStr:             maxReplicasStr,
		Metadata:                   metadata,
		MinReplicas:                minReplicas,
		MinReplicasStr:             minReplicasStr,
		Namespace:                  namespace,
		TargetCPUUtilization:       targetCPUUtilization,
		TargetCPUUtilizationStr:    targetCPUUtilizationStr,
		TargetMemoryUtilization:    targetMemoryUtilization,
		TargetMemoryUtilizationStr: targetMemoryUtilizationStr,
		Tenant:                     tenant,
	}, nil
}

func FromPod(pod *v1.Pod) (Instance, error) {
	fn, err := new(
		pod.Labels[key.Deployment.Label],
		pod.Labels[key.MaxReplicas.Label],
		"", // we don't store metadata in labels because they may contain sensitive information
		pod.Labels[key.MinReplicas.Label],
		pod.Labels[key.Namespace.Label],
		pod.Labels[key.TargetCPUUtilization.Label],
		pod.Labels[key.TargetMemoryUtilization.Label],
		pod.Labels[key.Tenant.Label],
	)
	if err != nil {
		return emptyInstance, err
	}

	assignedAtStr := pod.Labels[key.AssignedAt.Label]
	if assignedAtStr == "" {
		return emptyInstance, ErrMissingAssignedAt
	}

	assignedAt, err := strconv.ParseInt(assignedAtStr, 10, 64)
	if err != nil || assignedAt == 0 {
		return emptyInstance, ErrInvalidAssignedAt
	}

	var readyAt time.Time
	if readyAtStr, ok := pod.Labels[key.ReadyAt.Label]; ok {
		readyAtInt, err := strconv.ParseInt(readyAtStr, 10, 64)
		if err != nil || readyAtInt == 0 {
			return emptyInstance, ErrInvalidReadyAt
		}
		readyAt = time.Unix(readyAtInt, 0)
	}

	replicaSet := pod.Labels[key.ReplicaSet.Label]
	if replicaSet == "" {
		return emptyInstance, ErrMissingReplicaSet
	}

	return Instance{
		Function:   fn,
		Pod:        pod,
		AssignedAt: time.Unix(assignedAt, 0),
		ReadyAt:    readyAt,
		ReplicaSet: replicaSet,
	}, nil
}

func (f Function) RingKey() string {
	return f.Tenant + ":" +
		f.Namespace + ":" +
		f.Deployment + ":" +
		f.MinReplicasStr + ":" +
		f.MaxReplicasStr + ":" +
		f.TargetCPUUtilizationStr + ":" +
		f.TargetMemoryUtilizationStr
}

func (f Function) Fields() []slog.Attr {
	return []slog.Attr{
		key.Tenant.Field(f.Tenant),
		// key.Namespace.Field(f.Namespace),
		// key.Deployment.Field(f.Deployment),
		slog.String("deployment", f.Deployment),
		// key.MinReplicas.Field(f.MinReplicas),
		// key.MaxReplicas.Field(f.MaxReplicas),
		// key.TargetCPUUtilization.Field(f.TargetCPUUtilization),
		// key.TargetMemoryUtilization.Field(f.TargetMemoryUtilization),
	}
}

func (f Function) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		key.Tenant.Attribute(f.Tenant),
		key.Namespace.Attribute(f.Namespace),
		// key.Deployment.Field(f.Deployment),
		attribute.String("deployment", f.Deployment),
		key.MinReplicas.Attribute(f.MinReplicas),
		key.MaxReplicas.Attribute(f.MaxReplicas),
		key.TargetCPUUtilization.Attribute(f.TargetCPUUtilization),
		key.TargetMemoryUtilization.Attribute(f.TargetMemoryUtilization),
	}
}
