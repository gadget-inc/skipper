package destination

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gadget-inc/fusion/internal/key"
)

var (
	ErrMissingTenantHeader     = errors.New("missing required header: " + key.Tenant.Header)
	ErrMissingNamespaceHeader  = errors.New("missing required header: " + key.Namespace.Header)
	ErrMissingDeploymentHeader = errors.New("missing required header: " + key.Deployment.Header)
	ErrMissingAssignmentHeader = errors.New("missing required header: " + key.Assignment.Header)
	ErrorMissingReplicasHeader = errors.New("missing required header: " + key.Replicas.Header)
	ErrorMissingCpuHeader      = errors.New("missing required header: " + key.CpuUtilization.Header)
	ErrorMissingMemoryHeader   = errors.New("missing required header: " + key.MemoryUtilization.Header)

	ErrorInvalidReplicasHeader = errors.New("invalid value for header: " + key.Replicas.Header)
	ErrorInvalidCpuHeader      = errors.New("invalid value for header: " + key.CpuUtilization.Header)
	ErrorInvalidMemoryHeader   = errors.New("invalid value for header: " + key.MemoryUtilization.Header)
)

type Destination struct {
	Tenant            string
	Namespace         string
	Deployment        string
	Assignment        string
	Replicas          int
	CpuUtilization    int
	MemoryUtilization int

	ReplicasStr          string
	CpuUtilizationStr    string
	MemoryUtilizationStr string
}

func New(req *http.Request) (Destination, error) {
	tenant := req.Header.Get(key.Tenant.Header)
	if tenant == "" {
		return Destination{}, ErrMissingTenantHeader
	}

	namespace := req.Header.Get(key.Namespace.Header)
	if namespace == "" {
		return Destination{}, ErrMissingNamespaceHeader
	}

	deployment := req.Header.Get(key.Deployment.Header)
	if deployment == "" {
		return Destination{}, ErrMissingDeploymentHeader
	}

	assignment := req.Header.Get(key.Assignment.Header)
	if assignment == "" {
		return Destination{}, ErrMissingAssignmentHeader
	}

	replicasStr := req.Header.Get(key.Replicas.Header)
	if replicasStr == "" {
		return Destination{}, ErrorMissingReplicasHeader
	}

	replicas, err := strconv.Atoi(replicasStr)
	if err != nil {
		return Destination{}, ErrorInvalidReplicasHeader
	}

	cpuUtilizationStr := req.Header.Get(key.CpuUtilization.Header)
	if cpuUtilizationStr == "" {
		return Destination{}, ErrorMissingCpuHeader
	}

	cpuUtilization, err := strconv.Atoi(cpuUtilizationStr)
	if err != nil {
		return Destination{}, ErrorInvalidCpuHeader
	}

	memoryUtilizationStr := req.Header.Get(key.MemoryUtilization.Header)
	if memoryUtilizationStr == "" {
		return Destination{}, ErrorMissingMemoryHeader
	}

	memoryUtilization, err := strconv.Atoi(memoryUtilizationStr)
	if err != nil {
		return Destination{}, ErrorInvalidMemoryHeader
	}

	return Destination{
		Tenant:            tenant,
		Namespace:         namespace,
		Deployment:        deployment,
		Assignment:        assignment,
		Replicas:          replicas,
		CpuUtilization:    cpuUtilization,
		MemoryUtilization: memoryUtilization,

		ReplicasStr:          replicasStr,
		CpuUtilizationStr:    cpuUtilizationStr,
		MemoryUtilizationStr: memoryUtilizationStr,
	}, nil
}

func (d Destination) String() string {
	return d.Namespace + "/" + d.Deployment + "/" + d.Tenant
}

func (d Destination) SetHeaders(r *http.Request) {
	r.Header.Set(key.Tenant.Header, d.Tenant)
	r.Header.Set(key.Namespace.Header, d.Namespace)
	r.Header.Set(key.Deployment.Header, d.Deployment)
	r.Header.Set(key.Assignment.Header, d.Assignment)
	r.Header.Set(key.Replicas.Header, strconv.Itoa(d.Replicas))
	r.Header.Set(key.CpuUtilization.Header, strconv.Itoa(d.CpuUtilization))
	r.Header.Set(key.MemoryUtilization.Header, strconv.Itoa(d.MemoryUtilization))
}
