package fixture

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	counterMu         = new(sync.Mutex)
	podCounter        = 0
	replicaSetCounter = 0
	tenantCounter     = 0
)

func NewAvailablePod(t *testing.T, fn function.Function, handler http.Handler) *v1.Pod {
	if handler == nil {
		handler = defaultAvailablePodHandler(t, fn)
	}

	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	ip, portStr, err := net.SplitHostPort(testServer.Listener.Addr().String())
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	counterMu.Lock()
	defer counterMu.Unlock()

	podCounter++

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.Deployment + "-" + strconv.Itoa(podCounter),
			Namespace: fn.Namespace,
			Labels: map[string]string{
				key.Deployment.Label: fn.Deployment,
			},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: fn.Deployment + "-replicaset-" + strconv.Itoa(replicaSetCounter)},
			},
		},
		Status: v1.PodStatus{
			PodIP: ip,
			Phase: v1.PodRunning,
			Conditions: []v1.PodCondition{
				{Type: v1.PodReady, Status: v1.ConditionTrue},
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Ports: []v1.ContainerPort{{ContainerPort: int32(port)}}},
			},
		},
	}
}

func defaultAvailablePodHandler(t *testing.T, fn function.Function) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, function.FlagAssignPath.Value(), req.URL.Path)

		assignedFn, err := function.FromHeaders(req)
		require.NoError(t, err)
		require.Equal(t, fn, assignedFn)

		rw.WriteHeader(http.StatusOK)
	}
}

func NewAssignedPod(t *testing.T, fn function.Function, handler http.Handler) *v1.Pod {
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	ip, portStr, err := net.SplitHostPort(testServer.Listener.Addr().String())
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	counterMu.Lock()
	defer counterMu.Unlock()

	podCounter++

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.Deployment + "-" + strconv.Itoa(podCounter),
			Namespace: fn.Namespace,
			Labels: map[string]string{
				key.ReplicaSet.Label:              fn.Deployment + "-replicaset-" + strconv.Itoa(replicaSetCounter),
				key.Status.Label:                  "ready", // controller.StatusReady,
				key.Tenant.Label:                  fn.Tenant,
				key.Namespace.Label:               fn.Namespace,
				key.Deployment.Label:              fn.Deployment,
				key.Metadata.Label:                fn.Metadata,
				key.MinInstances.Label:            strconv.Itoa(fn.MinInstances),
				key.MaxInstances.Label:            strconv.Itoa(fn.MaxInstances),
				key.TargetCPUUtilization.Label:    strconv.Itoa(fn.TargetCPUUtilization),
				key.TargetMemoryUtilization.Label: strconv.Itoa(fn.TargetMemoryUtilization),
				key.AssignedAt.Label:              strconv.FormatInt(time.Now().Unix(), 10),
			},
		},
		Status: v1.PodStatus{
			PodIP: ip,
			Phase: v1.PodRunning,
			Conditions: []v1.PodCondition{
				{Type: v1.PodReady, Status: v1.ConditionTrue},
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Ports: []v1.ContainerPort{{ContainerPort: int32(port)}}},
			},
		},
	}
}
