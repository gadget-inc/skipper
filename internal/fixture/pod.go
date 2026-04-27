package fixture

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
	"github.com/google/uuid"
	"github.com/puzpuzpuz/xsync/v4"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

// replicaSetNames tracks the current replicaset name per function (keyed by function hash).
// This allows each function to have its own replicaset lineage without global counters.
var replicaSetNames = xsync.NewMap[skipper.FunctionHash, string]()

func NewAvailablePod(t *testing.T, fn *skipper.Function, handler http.Handler) *v1.Pod {
	t.Helper()

	if handler == nil {
		handler = defaultAvailablePodHandler(t, fn)
	}

	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	ip, portStr, err := net.SplitHostPort(testServer.Listener.Addr().String())
	assert.NilError(t, err)

	port, err := strconv.Atoi(portStr)
	assert.NilError(t, err)

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.GetDeployment() + "-" + uuid.NewString()[:8],
			Namespace: fn.GetNamespace(),
			Labels: map[string]string{
				key.Deployment.Label: fn.GetDeployment(),
			},
			Annotations: map[string]string{
				key.Port.Label: "http",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "ReplicaSet",
					Name: CurrentReplicaSetName(fn),
				},
			},
		},
		Status: v1.PodStatus{
			PodIP: ip,
			Phase: v1.PodRunning,
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "main",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	}
}

func defaultAvailablePodHandler(t *testing.T, fn *skipper.Function) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		assert.Assert(t, req.Method == http.MethodPost)
		assert.Assert(t, req.URL.Path == "/__skipper/assign")

		assignedFn, err := skipper.FunctionFromHeader(req)
		assert.NilError(t, err)
		assert.Assert(t, proto.Equal(assignedFn, fn))

		parser := paseto.NewParserForValidNow()
		parser.AddRule(paseto.Subject(fn.GetTenant()))
		_, err = parser.ParseV2Public(ControllerPasetoPublicKey, req.Header.Get(key.Token.Header))
		assert.NilError(t, err)

		rw.WriteHeader(http.StatusOK)
	}
}

func NewAssignedPod(t *testing.T, fn *skipper.Function, handler http.Handler) *v1.Pod {
	t.Helper()

	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	ip, portStr, err := net.SplitHostPort(testServer.Listener.Addr().String())
	assert.NilError(t, err)

	port, err := strconv.Atoi(portStr)
	assert.NilError(t, err)

	fnJSON, err := json.Marshal(fn)
	assert.NilError(t, err)

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.GetDeployment() + "-" + uuid.NewString()[:8],
			Namespace: fn.GetNamespace(),
			Labels: map[string]string{
				key.Deployment.Label: fn.GetDeployment(),
				key.Tenant.Label:     fn.GetTenant(),
			},
			Annotations: map[string]string{
				skipper.FunctionKey.Label: string(fnJSON),
				key.ReplicaSet.Label:      CurrentReplicaSetName(fn),
				key.AssignedAt.Label:      time.Now().UTC().Format(time.RFC3339),
				key.ReadyAt.Label:         time.Now().UTC().Format(time.RFC3339),
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
				{
					Name:    "main",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
					Ports:   []v1.ContainerPort{{ContainerPort: int32(port)}},
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(strconv.FormatUint(uint64(fn.GetScale().GetTargetCpuUsageMilli()), 10) + "m"),
							v1.ResourceMemory: resource.MustParse(strconv.FormatUint(uint64(fn.GetScale().GetTargetMemoryUsageMib()), 10) + "Mi"),
						},
					},
				},
			},
		},
	}
}

// CurrentReplicaSetName returns the current replicaset name for the given skipper.
// If no replicaset has been created yet for this function, it generates one.
func CurrentReplicaSetName(fn *skipper.Function) string {
	name, _ := replicaSetNames.LoadOrCompute(fn.Hash(), func() (string, bool) {
		return fn.GetDeployment() + "-rs-" + uuid.NewString()[:8], false
	})
	return name
}

func CurrentReplicaSet(t *testing.T, fn *skipper.Function) *appsv1.ReplicaSet {
	t.Helper()
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CurrentReplicaSetName(fn),
			Namespace: fn.GetNamespace(),
			Labels: map[string]string{
				key.Deployment.Label: fn.GetDeployment(),
			},
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:          1,
			AvailableReplicas: 1,
		},
	}
}

// NewReplicaSet creates a new replicaset for the function, replacing any existing one.
func NewReplicaSet(t *testing.T, fn *skipper.Function) *appsv1.ReplicaSet {
	t.Helper()
	// Generate a new replicaset name for this function
	replicaSetNames.Store(fn.Hash(), fn.GetDeployment()+"-rs-"+uuid.NewString()[:8])
	return CurrentReplicaSet(t, fn)
}

// NewPodMetrics returns PodMetrics for the given pod.
//
// This returns 4 values because it is meant to be used with
// metricsClientset.Tracker().Create() rather than
// metricsClientset.Tracker().Add(). This is because Add() has to guess
// the schema.GroupVersionResource and gets it wrong for PodMetrics,
// making the pod metrics not show up in future
// metricsClientset.MetricsV1beta1().PodMetricses() calls.
func NewPodMetrics(t *testing.T, pod *v1.Pod, cpu, memory string) (schema.GroupVersionResource, *metricsv1beta1.PodMetrics, string, metav1.CreateOptions) {
	t.Helper()
	return metricsv1beta1.SchemeGroupVersion.WithResource("pods"),
		&metricsv1beta1.PodMetrics{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				Labels:    pod.Labels,
			},
			Containers: []metricsv1beta1.ContainerMetrics{
				{
					Name: pod.Spec.Containers[0].Name,
					Usage: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse(cpu),
						v1.ResourceMemory: resource.MustParse(memory),
					},
				},
			},
		},
		pod.Namespace,
		metav1.CreateOptions{}
}
