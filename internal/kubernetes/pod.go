package kubernetes

import (
	"net/http/httputil"
	"net/url"

	v1 "k8s.io/api/core/v1"
)

type Pod struct {
	*v1.Pod
	*httputil.ReverseProxy
}

func NewPod(pod *v1.Pod) *Pod {
	return &Pod{
		Pod: pod,
		ReverseProxy: &httputil.ReverseProxy{
			Rewrite: func(req *httputil.ProxyRequest) {
				req.SetURL(&url.URL{
					Scheme: "http",
					Host:   pod.Status.PodIP + ":8080",
				})
			},
		},
	}
}
