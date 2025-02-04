package fixture

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/goccy/go-json"
	"github.com/shoenig/test/must"
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
	must.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	must.NoError(t, err)

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
			Annotations: map[string]string{
				"": "", // needed to avoid "add operation does not apply: doc is missing path: /metadata/annotations/...: missing value"
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
		must.Eq(t, http.MethodPost, req.Method)
		must.Eq(t, function.FlagAssignPath.Value(), req.URL.Path)

		assignedFn, err := function.FromHeader(req)
		must.NoError(t, err)
		must.Eq(t, fn, assignedFn)

		bytes, err := io.ReadAll(req.Body)
		must.NoError(t, err)

		parser := paseto.NewParserForValidNow()
		parser.AddRule(paseto.Subject(fn.Tenant))
		_, err = parser.ParseV2Public(DefaultControllerPasetoPublicKey, string(bytes))
		must.NoError(t, err)

		rw.WriteHeader(http.StatusOK)
	}
}

func NewAssignedPod(t *testing.T, fn function.Function, handler http.Handler) *v1.Pod {
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	ip, portStr, err := net.SplitHostPort(testServer.Listener.Addr().String())
	must.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	must.NoError(t, err)

	fnJSON, err := json.Marshal(fn)
	must.NoError(t, err)

	counterMu.Lock()
	defer counterMu.Unlock()

	podCounter++

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.Deployment + "-" + strconv.Itoa(podCounter),
			Namespace: fn.Namespace,
			Labels: map[string]string{
				key.Deployment.Label: fn.Deployment,
				key.Tenant.Label:     fn.Tenant,
			},
			Annotations: map[string]string{
				key.Function.Label:   string(fnJSON),
				key.ReplicaSet.Label: fn.Deployment + "-replicaset-" + strconv.Itoa(replicaSetCounter),
				key.AssignedAt.Label: time.Now().UTC().Format(time.RFC3339),
				key.ReadyAt.Label:    time.Now().UTC().Format(time.RFC3339),
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

func CurrentReplicaSet(fn function.Function) string {
	counterMu.Lock()
	defer counterMu.Unlock()
	return fn.Deployment + "-replicaset-" + strconv.Itoa(replicaSetCounter)
}
