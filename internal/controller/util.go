package controller

import (
	"io"
	"net/http"
	"strings"

	v1 "k8s.io/api/core/v1"
)

func panicIf(err error) {
	if err != nil {
		panic(err)
	}
}

func unwrap[V any](v V, err error) V {
	panicIf(err)
	return v
}

func getResponseBody(res *http.Response) string {
	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return "failed to read body"
	}
	return strings.TrimSpace(string(bytes))
}

// isPodRunning returns true if the pod is running, has a pod IP, and is not being deleted
func isPodRunning(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" && pod.DeletionTimestamp == nil
}

// isPodReady returns true if the pod is running and is ready to serve traffic (readiness probe is passing)
func isPodReady(pod *v1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
			return isPodRunning(pod)
		}
	}
	return false
}
