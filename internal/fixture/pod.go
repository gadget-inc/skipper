package fixture

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
	"github.com/shoenig/test/must"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

var (
	podCounter        = new(atomic.Int64)
	replicaSetCounter = new(atomic.Int64)
	tenantCounter     = new(atomic.Int64)
)

func NewAvailablePod(t *testing.T, fn *function.Function, handler http.Handler) *v1.Pod {
	if handler == nil {
		handler = defaultAvailablePodHandler(t, fn)
	}

	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	ip, portStr, err := net.SplitHostPort(testServer.Listener.Addr().String())
	must.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	must.NoError(t, err)

	podCounter.Add(1)

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.Deployment + "-" + strconv.Itoa(int(podCounter.Load())),
			Namespace: fn.Namespace,
			Labels: map[string]string{
				key.Deployment.Label: fn.Deployment,
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

func defaultAvailablePodHandler(t *testing.T, fn *function.Function) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		must.Eq(t, http.MethodPost, req.Method)
		must.Eq(t, function.FlagAssignPath.Value(), req.URL.Path)

		assignedFn, err := function.FromHeader(req)
		must.NoError(t, err)
		must.Eq(t, fn, assignedFn)

		parser := paseto.NewParserForValidNow()
		parser.AddRule(paseto.Subject(fn.Tenant))
		_, err = parser.ParseV2Public(ControllerPasetoPublicKey, req.Header.Get(key.Token.Header))
		must.NoError(t, err)

		rw.WriteHeader(http.StatusOK)
	}
}

func NewAssignedPod(t *testing.T, fn *function.Function, handler http.Handler) *v1.Pod {
	testServer := httptest.NewServer(handler)
	t.Cleanup(testServer.Close)

	ip, portStr, err := net.SplitHostPort(testServer.Listener.Addr().String())
	must.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	must.NoError(t, err)

	fnJSON, err := json.Marshal(fn)
	must.NoError(t, err)

	podCounter.Add(1)

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.Deployment + "-" + strconv.Itoa(int(podCounter.Load())),
			Namespace: fn.Namespace,
			Labels: map[string]string{
				key.Deployment.Label: fn.Deployment,
				key.Tenant.Label:     fn.Tenant,
			},
			Annotations: map[string]string{
				key.Function.Label:   string(fnJSON),
				key.ReplicaSet.Label: CurrentReplicaSetName(fn),
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
				{
					Name:    "main",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
					Ports:   []v1.ContainerPort{{ContainerPort: int32(port)}},
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse(strconv.Itoa(fn.Scale.TargetCPUUsageMilli) + "m"),
							v1.ResourceMemory: resource.MustParse(strconv.Itoa(fn.Scale.TargetMemoryUsageMiB) + "Mi"),
						},
					},
				},
			},
		},
	}
}

func CurrentReplicaSetName(fn *function.Function) string {
	return fn.Deployment + "-replicaset-" + strconv.Itoa(int(replicaSetCounter.Load()))
}

func CurrentReplicaSet(t *testing.T, fn *function.Function) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CurrentReplicaSetName(fn),
			Namespace: fn.Namespace,
			Labels: map[string]string{
				key.Deployment.Label: fn.Deployment,
			},
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:          1,
			AvailableReplicas: 1,
		},
	}
}

func NewReplicaSet(t *testing.T, fn *function.Function) *appsv1.ReplicaSet {
	replicaSetCounter.Add(1)
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
