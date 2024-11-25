package function

import (
	"net/http"

	"github.com/gadget-inc/fusion/internal/key"
)

func RemoveHeaders(r *http.Request) {
	delete(r.Header, key.Tenant.Header)
	delete(r.Header, key.Namespace.Header)
	delete(r.Header, key.Deployment.Header)
	delete(r.Header, key.Metadata.Header)
	delete(r.Header, key.MinReplicas.Header)
	delete(r.Header, key.MaxReplicas.Header)
	delete(r.Header, key.TargetCPUUtilization.Header)
	delete(r.Header, key.TargetMemoryUtilization.Header)
}

func (f Function) SetHeaders(r *http.Request) {
	r.Header[key.Tenant.Header] = []string{f.Tenant}
	r.Header[key.Namespace.Header] = []string{f.Namespace}
	r.Header[key.Deployment.Header] = []string{f.Deployment}
	r.Header[key.Metadata.Header] = []string{f.Metadata}
	r.Header[key.MinReplicas.Header] = []string{f.MinReplicasStr}
	r.Header[key.MaxReplicas.Header] = []string{f.MaxReplicasStr}
	r.Header[key.TargetCPUUtilization.Header] = []string{f.TargetCPUUtilizationStr}
	r.Header[key.TargetMemoryUtilization.Header] = []string{f.TargetMemoryUtilizationStr}
}

func FromHeaders(req *http.Request) (Function, error) {
	metadata, ok := req.Header[key.Metadata.Header]
	if !ok || len(metadata) == 0 {
		return emptyFunction, ErrMissingMetadata
	}

	return new(
		getHeaderValue(req, key.Deployment.Header),
		getHeaderValue(req, key.MaxReplicas.Header),
		metadata[0],
		getHeaderValue(req, key.MinReplicas.Header),
		getHeaderValue(req, key.Namespace.Header),
		getHeaderValue(req, key.TargetCPUUtilization.Header),
		getHeaderValue(req, key.TargetMemoryUtilization.Header),
		getHeaderValue(req, key.Tenant.Header),
	)
}

func getHeaderValue(req *http.Request, header string) string {
	if values, ok := req.Header[header]; ok && len(values) > 0 {
		return values[0]
	}
	return ""
}
