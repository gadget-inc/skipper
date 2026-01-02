package key

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

func TestErrorKey_Nil(t *testing.T) {
	attr := Error.Slog(nil)

	// Nil error returns empty attr
	assert.Equal(t, attr.Key, "")
}

func TestErrorKey_NonNil(t *testing.T) {
	err := errors.New("something went wrong")
	attr := Error.Slog(err)

	assert.Equal(t, attr.Key, "error")
	assert.Equal(t, attr.Value.Any().(error).Error(), "something went wrong")
}

func TestResponseKey_Nil(t *testing.T) {
	attr := Response.Slog(nil)

	// Nil response returns empty attr
	assert.Equal(t, attr.Key, "")
}

func TestResponseKey_NonNil(t *testing.T) {
	resp := &http.Response{
		StatusCode:    200,
		Status:        "200 OK",
		ContentLength: 1234,
	}
	attr := Response.Slog(resp)

	assert.Equal(t, attr.Key, "http.response")
	// It's a group with status_code, body.size, and 2 conditional header attrs
	assert.Equal(t, len(attr.Value.Group()), 4)
}

func TestMapStringStringKey_Nil(t *testing.T) {
	attr := Labels.Slog(nil)

	// Nil map returns empty attr
	assert.Equal(t, attr.Key, "")
}

func TestMapStringStringKey_NonNil(t *testing.T) {
	labels := map[string]string{"app": "skipper", "env": "prod"}
	attr := Labels.Slog(labels)

	assert.Equal(t, attr.Key, "labels")
	assert.Equal(t, len(attr.Value.Group()), 2)
}

func TestPodKey_Nil(t *testing.T) {
	attr := Pod.Slog(nil)

	// Nil pod returns empty attr
	assert.Equal(t, attr.Key, "")
}

func TestPodKey_NonNil(t *testing.T) {
	pod := &v1.Pod{}
	pod.Name = "test-pod"
	pod.Namespace = "default"
	attr := Pod.Slog(pod)

	assert.Equal(t, attr.Key, "k8s.pod")
}

func TestK8sReplicaSetKey_Nil(t *testing.T) {
	attr := K8sReplicaSet.Slog(nil)

	// Nil replica set returns empty attr
	assert.Equal(t, attr.Key, "")
}

func TestK8sReplicaSetKey_NonNil(t *testing.T) {
	rs := &appsv1.ReplicaSet{}
	rs.Name = "test-rs"
	rs.Namespace = "default"
	attr := K8sReplicaSet.Slog(rs)

	assert.Equal(t, attr.Key, "k8s.replicaset")
}

func TestDurationKey(t *testing.T) {
	attr := Duration.Slog(1500 * time.Millisecond)

	// Duration key uses _ms suffix
	assert.Equal(t, attr.Key, "duration_ms")
	assert.Equal(t, attr.Value.Int64(), int64(1500))
}

func TestStringSliceKey(t *testing.T) {
	attr := ExcludeInstanceNames.Slog([]string{"a", "b", "c"})

	assert.Equal(t, attr.Key, "exclude_instance_names")
}
