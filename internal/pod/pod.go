package pod

import (
	"net/http/httputil"
	"net/url"

	"github.com/gadget-inc/fusion/internal/buffer"
	v1 "k8s.io/api/core/v1"
)

const (
	labelTenant     = "fusion/tenant"
	labelDeployment = "fusion/deployment"
	labelStatus     = "fusion/status"

	patchLabelTenant = "fusion~1tenant"
	patchLabelStatus = "fusion~1status"
)

type Pod struct {
	*v1.Pod
	*httputil.ReverseProxy
}

func New(pod *v1.Pod) *Pod {
	return &Pod{
		Pod: pod,
		ReverseProxy: &httputil.ReverseProxy{
			BufferPool: buffer.Pool,
			Rewrite: func(req *httputil.ProxyRequest) {
				req.SetURL(&url.URL{
					Scheme: "http",
					Host:   pod.Status.PodIP + ":8080",
				})
			},
		},
	}
}
