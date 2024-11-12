package destination

import (
	"errors"
	"net/http"
)

const (
	HeaderTenant     = "x-fusion-tenant"
	HeaderNamespace  = "x-fusion-namespace"
	HeaderDeployment = "x-fusion-deployment"
	HeaderAssignment = "x-fusion-assignment"
)

var (
	ErrMissingTenantHeader     = errors.New("missing required header: " + HeaderTenant)
	ErrMissingNamespaceHeader  = errors.New("missing required header: " + HeaderNamespace)
	ErrMissingDeploymentHeader = errors.New("missing required header: " + HeaderDeployment)
	ErrMissingAssignmentHeader = errors.New("missing required header: " + HeaderAssignment)
)

type Destination struct {
	Tenant     string
	Namespace  string
	Deployment string
	Assignment string
}

func New(req *http.Request) (Destination, error) {
	tenant := req.Header.Get(HeaderTenant)
	if tenant == "" {
		return Destination{}, ErrMissingTenantHeader
	}

	namespace := req.Header.Get(HeaderNamespace)
	if namespace == "" {
		return Destination{}, ErrMissingNamespaceHeader
	}

	deployment := req.Header.Get(HeaderDeployment)
	if deployment == "" {
		return Destination{}, ErrMissingDeploymentHeader
	}

	assignment := req.Header.Get(HeaderAssignment)
	if assignment == "" {
		return Destination{}, ErrMissingAssignmentHeader
	}

	return Destination{
		Tenant:     tenant,
		Namespace:  namespace,
		Deployment: deployment,
		Assignment: assignment,
	}, nil
}

func (d *Destination) String() string {
	return d.Namespace + "/" + d.Deployment + "/" + d.Tenant
}
