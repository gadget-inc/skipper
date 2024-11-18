package function

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
)

var (
	ErrMissingDeploymentHeader              = errors.New("missing required header: " + key.Deployment.Header)
	ErrMissingMetadataHeader                = errors.New("missing required header: " + key.Metadata.Header)
	ErrMissingNamespaceHeader               = errors.New("missing required header: " + key.Namespace.Header)
	ErrMissingTenantHeader                  = errors.New("missing required header: " + key.Tenant.Header)
	ErrMissingMaxReplicasHeader             = errors.New("missing required header: " + key.MaxReplicas.Header)
	ErrMissingMinReplicasHeader             = errors.New("missing required header: " + key.MinReplicas.Header)
	ErrMissingTargetCPUUtilizationHeader    = errors.New("missing required header: " + key.TargetCPUUtilization.Header)
	ErrMissingTargetMemoryUtilizationHeader = errors.New("missing required header: " + key.TargetMemoryUtilization.Header)

	ErrInvalidMaxReplicasHeader             = errors.New("invalid value for header: " + key.MaxReplicas.Header)
	ErrInvalidMinReplicasHeader             = errors.New("invalid value for header: " + key.MinReplicas.Header)
	ErrInvalidTargetCPUUtilizationHeader    = errors.New("invalid value for header: " + key.TargetCPUUtilization.Header)
	ErrInvalidTargetMemoryUtilizationHeader = errors.New("invalid value for header: " + key.TargetMemoryUtilization.Header)

	ErrMissingAssignedAtLabel              = errors.New("missing required label: " + key.AssignedAt.Label)
	ErrMissingDeploymentLabel              = errors.New("missing required label: " + key.Deployment.Label)
	ErrMissingMaxReplicasLabel             = errors.New("missing required label: " + key.MaxReplicas.Label)
	ErrMissingMinReplicasLabel             = errors.New("missing required label: " + key.MinReplicas.Label)
	ErrMissingNamespaceLabel               = errors.New("missing required label: " + key.Namespace.Label)
	ErrMissingTargetCPUUtilizationLabel    = errors.New("missing required label: " + key.TargetCPUUtilization.Label)
	ErrMissingTargetMemoryUtilizationLabel = errors.New("missing required label: " + key.TargetMemoryUtilization.Label)
	ErrMissingTenantLabel                  = errors.New("missing required label: " + key.Tenant.Label)

	ErrInvalidAssignedAtLabel              = errors.New("invalid value for label: " + key.AssignedAt.Label)
	ErrInvalidMaxReplicasLabel             = errors.New("invalid value for label: " + key.MaxReplicas.Label)
	ErrInvalidMinReplicasLabel             = errors.New("invalid value for label: " + key.MinReplicas.Label)
	ErrInvalidTargetCPUUtilizationLabel    = errors.New("invalid value for label: " + key.TargetCPUUtilization.Label)
	ErrInvalidTargetMemoryUtilizationLabel = errors.New("invalid value for label: " + key.TargetMemoryUtilization.Label)
	ErrInvalidReadyAtLabel                 = errors.New("invalid value for label: " + key.ReadyAt.Label)
)

var emptyInstance = Instance{}

type Function struct {
	Deployment                 string
	MaxReplicas                int
	MaxReplicasStr             string
	Metadata                   string
	MinReplicas                int
	MinReplicasStr             string
	Namespace                  string
	TargetCPUUtilization       int
	TargetCPUUtilizationStr    string
	TargetMemoryUtilization    int
	TargetMemoryUtilizationStr string
	Tenant                     string
}

type Instance struct {
	Function
	AssignedAt time.Time
	ReadyAt    *time.Time
}

func FromRequest(req *http.Request) (Function, error) {
	tenant := req.Header.Get(key.Tenant.Header)
	if tenant == "" {
		return emptyInstance.Function, ErrMissingTenantHeader
	}

	namespace := req.Header.Get(key.Namespace.Header)
	if namespace == "" {
		return emptyInstance.Function, ErrMissingNamespaceHeader
	}

	deployment := req.Header.Get(key.Deployment.Header)
	if deployment == "" {
		return emptyInstance.Function, ErrMissingDeploymentHeader
	}

	metadata := req.Header.Get(key.Metadata.Header)
	if metadata == "" {
		return emptyInstance.Function, ErrMissingMetadataHeader
	}

	maxReplicaStr := req.Header.Get(key.MaxReplicas.Header)
	if maxReplicaStr == "" {
		return emptyInstance.Function, ErrMissingMaxReplicasHeader
	}

	maxReplicas, err := strconv.Atoi(maxReplicaStr)
	if err != nil {
		return emptyInstance.Function, ErrInvalidMaxReplicasHeader
	}

	minReplicaStr := req.Header.Get(key.MinReplicas.Header)
	if minReplicaStr == "" {
		return emptyInstance.Function, ErrMissingMinReplicasHeader
	}

	minReplicas, err := strconv.Atoi(minReplicaStr)
	if err != nil {
		return emptyInstance.Function, ErrInvalidMinReplicasHeader
	}

	targetCPUUtilizationStr := req.Header.Get(key.TargetCPUUtilization.Header)
	if targetCPUUtilizationStr == "" {
		return emptyInstance.Function, ErrMissingTargetCPUUtilizationHeader
	}

	targetCPUUtilization, err := strconv.Atoi(targetCPUUtilizationStr)
	if err != nil {
		return emptyInstance.Function, ErrInvalidTargetCPUUtilizationHeader
	}

	targetMemoryUtilizationStr := req.Header.Get(key.TargetMemoryUtilization.Header)
	if targetMemoryUtilizationStr == "" {
		return emptyInstance.Function, ErrMissingTargetMemoryUtilizationHeader
	}

	targetMemoryUtilization, err := strconv.Atoi(targetMemoryUtilizationStr)
	if err != nil {
		return emptyInstance.Function, ErrInvalidTargetMemoryUtilizationHeader
	}

	return Function{
		Deployment:                 deployment,
		MaxReplicas:                maxReplicas,
		MaxReplicasStr:             maxReplicaStr,
		Metadata:                   metadata,
		MinReplicas:                minReplicas,
		MinReplicasStr:             minReplicaStr,
		Namespace:                  namespace,
		TargetCPUUtilization:       targetCPUUtilization,
		TargetCPUUtilizationStr:    targetCPUUtilizationStr,
		TargetMemoryUtilization:    targetMemoryUtilization,
		TargetMemoryUtilizationStr: targetMemoryUtilizationStr,
		Tenant:                     tenant,
	}, nil
}

func FromLabels(labels map[string]string) (Instance, error) {
	tenant := labels[key.Tenant.Label]
	if tenant == "" {
		return emptyInstance, ErrMissingTenantLabel
	}

	namespace := labels[key.Namespace.Label]
	if namespace == "" {
		return emptyInstance, ErrMissingNamespaceLabel
	}

	deployment := labels[key.Deployment.Label]
	if deployment == "" {
		return emptyInstance, ErrMissingDeploymentLabel
	}

	maxReplicaStr := labels[key.MaxReplicas.Label]
	if maxReplicaStr == "" {
		return emptyInstance, ErrMissingMaxReplicasLabel
	}

	maxReplicas, err := strconv.Atoi(maxReplicaStr)
	if err != nil {
		return emptyInstance, ErrInvalidMaxReplicasLabel
	}

	minReplicaStr := labels[key.MinReplicas.Label]
	if minReplicaStr == "" {
		return emptyInstance, ErrMissingMinReplicasLabel
	}

	minReplicas, err := strconv.Atoi(minReplicaStr)
	if err != nil {
		return emptyInstance, ErrInvalidMinReplicasLabel
	}

	targetCPUUtilizationStr := labels[key.TargetCPUUtilization.Label]
	if targetCPUUtilizationStr == "" {
		return emptyInstance, ErrMissingTargetCPUUtilizationLabel
	}

	targetCPUUtilization, err := strconv.Atoi(targetCPUUtilizationStr)
	if err != nil {
		return emptyInstance, ErrInvalidTargetCPUUtilizationLabel
	}

	targetMemoryUtilizationStr := labels[key.TargetMemoryUtilization.Label]
	if targetMemoryUtilizationStr == "" {
		return emptyInstance, ErrMissingTargetMemoryUtilizationLabel
	}

	targetMemoryUtilization, err := strconv.Atoi(targetMemoryUtilizationStr)
	if err != nil {
		return emptyInstance, ErrInvalidTargetMemoryUtilizationLabel
	}

	assignedAtStr := labels[key.AssignedAt.Label]
	if assignedAtStr == "" {
		return emptyInstance, ErrMissingAssignedAtLabel
	}

	assignedAt, err := strconv.ParseInt(assignedAtStr, 10, 64)
	if err != nil || assignedAt == 0 {
		return emptyInstance, ErrInvalidAssignedAtLabel
	}

	var readyAt *time.Time
	if readyAtStr, ok := labels[key.ReadyAt.Label]; ok {
		readyAtInt, err := strconv.ParseInt(readyAtStr, 10, 64)
		if err != nil || readyAtInt == 0 {
			return emptyInstance, ErrInvalidReadyAtLabel
		}
		readyAtTime := time.Unix(readyAtInt, 0)
		readyAt = &readyAtTime
	}

	return Instance{
		AssignedAt: time.Unix(assignedAt, 0),
		ReadyAt:    readyAt,
		Function: Function{
			Deployment:                 deployment,
			MaxReplicas:                maxReplicas,
			MaxReplicasStr:             maxReplicaStr,
			Metadata:                   "", // we don't store metadata in labels because they may contain sensitive information
			MinReplicas:                minReplicas,
			MinReplicasStr:             minReplicaStr,
			Namespace:                  namespace,
			TargetCPUUtilization:       targetCPUUtilization,
			TargetCPUUtilizationStr:    targetCPUUtilizationStr,
			TargetMemoryUtilization:    targetMemoryUtilization,
			TargetMemoryUtilizationStr: targetMemoryUtilizationStr,
			Tenant:                     tenant,
		},
	}, nil
}

func (d Function) RingKey() string {
	return "fusion://" + d.Namespace + "." + d.Deployment + "." + d.Tenant
}

func (d Function) SetHeaders(r *http.Request) {
	r.Header.Set(key.Tenant.Header, d.Tenant)
	r.Header.Set(key.Namespace.Header, d.Namespace)
	r.Header.Set(key.Deployment.Header, d.Deployment)
	r.Header.Set(key.Metadata.Header, d.Metadata)
	r.Header.Set(key.MinReplicas.Header, d.MinReplicasStr)
	r.Header.Set(key.MaxReplicas.Header, d.MaxReplicasStr)
	r.Header.Set(key.TargetCPUUtilization.Header, d.TargetCPUUtilizationStr)
	r.Header.Set(key.TargetMemoryUtilization.Header, d.TargetMemoryUtilizationStr)
}

func (d Function) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("tenant", d.Tenant),
		slog.String("namespace", d.Namespace),
		slog.String("deployment", d.Deployment),
		slog.Int("minReplicas", d.MinReplicas),
		slog.Int("maxReplicas", d.MaxReplicas),
		slog.Int("targetCPUUtilization", d.TargetCPUUtilization),
		slog.Int("targetMemoryUtilization", d.TargetMemoryUtilization),
	)
}

func (d Function) AttributeValue() attribute.Value {
	return attribute.StringValue("fusion://" + d.Namespace + "." + d.Deployment + "." + d.Tenant + "?minReplicas=" + d.MinReplicasStr + "&maxReplicas=" + d.MaxReplicasStr + "&targetCPUUtilization=" + d.TargetCPUUtilizationStr + "&targetMemoryUtilization=" + d.TargetMemoryUtilizationStr)
}
