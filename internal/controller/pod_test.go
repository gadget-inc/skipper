package controller

import (
	"context"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/poll"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	fakekubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func ensureInstanceIsAssignedToPod(t *testing.T, instance *skipper.Instance, pod v1.Pod) {
	t.Helper()
	assert.Assert(t, instance.GetFunction().GetDeployment() == pod.Labels[key.Deployment.Label])
	assert.Assert(t, instance.GetFunction().GetTenant() == pod.Labels[key.Tenant.Label])

	fnJSON, err := json.Marshal(instance.GetFunction())
	assert.NilError(t, err)
	assert.Assert(t, string(fnJSON) == pod.Annotations[skipper.FunctionKey.Label])

	ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
	port, err := ctrl.portFromPod(&pod)
	assert.NilError(t, err)

	assert.Assert(t, instance.GetName() == pod.Name)
	assert.Assert(t, instance.GetAddr() == net.JoinHostPort(pod.Status.PodIP, port))
	assert.Assert(t, instance.GetReplicaSet() == pod.Annotations[key.ReplicaSet.Label])
	assert.Assert(t, instance.GetAssignedAt().AsTime().Format(time.RFC3339) == pod.Annotations[key.AssignedAt.Label])
	assert.Assert(t, instance.GetReadyAt().AsTime().Format(time.RFC3339) == pod.Annotations[key.ReadyAt.Label])
}

// listAssignedPods lists pods and waits for the ready-at annotation to be
// persisted — assignPod returns before its async ready-at patch completes,
// so tests that read the pod back from the apiserver need to poll.
func listAssignedPods(t *testing.T, client *fake.Clientset, namespace string, expected int) []v1.Pod {
	t.Helper()
	var items []v1.Pod
	poll.WaitOn(t, func(_ poll.LogT) poll.Result {
		pods, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return poll.Error(err)
		}
		if len(pods.Items) != expected {
			return poll.Continue("expected %d pods, got %d", expected, len(pods.Items))
		}
		for _, pod := range pods.Items {
			if pod.Annotations[key.ReadyAt.Label] == "" {
				return poll.Continue("pod %s missing ready-at annotation", pod.Name)
			}
		}
		items = pods.Items
		return poll.Success()
	}, poll.WithTimeout(2*time.Second), poll.WithDelay(10*time.Millisecond))
	return items
}

func ensurePodIsNotAssignedToFunction(t *testing.T, pod v1.Pod) {
	assert.Assert(t, pod.Labels[key.Tenant.Label] == "")
	assert.Assert(t, pod.Annotations[skipper.FunctionKey.Label] == "")
	assert.Assert(t, pod.Annotations[key.ReplicaSet.Label] == "")
	assert.Assert(t, pod.Annotations[key.AssignedAt.Label] == "")
	assert.Assert(t, pod.Annotations[key.ReadyAt.Label] == "")
}

func TestAssignPod(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		cfg            *Config
		fakeKubernetes *fake.Clientset
		instance       *skipper.Instance
	}

	testCases := []struct {
		name        string
		err         error
		errContains string // if set, checks that error contains this string instead of using errors.Is
		setup       func(*testing.T, *testState)
		check       func(*testing.T, *testState)
	}{
		{
			// Basic assignment: an available pod exists and should be assigned to the function
			name: "assigns available pod to function",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				pods := listAssignedPods(t, state.fakeKubernetes, state.instance.GetFunction().GetNamespace(), 1)
				ensureInstanceIsAssignedToPod(t, state.instance, pods[0])
			},
		},
		{
			// No pods available: should timeout waiting for a pod to become available
			name: "times out when no pods available",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, state *testState) {
				// intentionally empty - no pods available
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.instance == nil)
			},
		},
		{
			// Delayed availability: pod becomes available after a short delay, should still succeed
			name: "waits for pod to become available",
			setup: func(t *testing.T, state *testState) {
				go func() {
					time.Sleep(500 * time.Millisecond)
					state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				}()
			},
			check: func(t *testing.T, state *testState) {
				pods := listAssignedPods(t, state.fakeKubernetes, state.instance.GetFunction().GetNamespace(), 1)
				ensureInstanceIsAssignedToPod(t, state.instance, pods[0])
			},
		},
		{
			// Assignment timeout: pod's assign endpoint is too slow to respond
			name: "times out when pod assign endpoint is slow",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, state *testState) {
				state.cfg = testConfig()
				state.cfg.FunctionAssignTimeout = time.Millisecond

				// create a pod with a slow assign handler
				slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(10 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				})
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, slowHandler))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.instance == nil)
			},
		},
		{
			// Multiple ports: pod has multiple container ports, should use the one specified by port annotation
			name: "uses port annotation when pod has multiple ports",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAvailablePod(t, state.fn, nil)

				// verify default port setup
				assert.Assert(t, pod.Annotations[key.Port.Label] == "http")
				assert.Assert(t, pod.Spec.Containers[0].Ports[0].Name == "http")

				// add another port and make the http port the second port on the container
				pod.Spec.Containers[0].Ports = []v1.ContainerPort{
					{ContainerPort: 8080, Name: "other"},
					pod.Spec.Containers[0].Ports[0],
				}
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				pods := listAssignedPods(t, state.fakeKubernetes, state.instance.GetFunction().GetNamespace(), 1)
				ensureInstanceIsAssignedToPod(t, state.instance, pods[0])
			},
		},
		{
			// Missing port annotation: should fall back to first container port
			name: "uses first container port when port annotation is empty",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAvailablePod(t, state.fn, nil)
				pod.Annotations[key.Port.Label] = ""
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				pods := listAssignedPods(t, state.fakeKubernetes, state.instance.GetFunction().GetNamespace(), 1)
				ensureInstanceIsAssignedToPod(t, state.instance, pods[0])
			},
		},
		{
			// Assign endpoint returns error: pod's assign endpoint returns non-200 status
			// The pod should be deleted and the error should be returned
			name:        "deletes pod when assign endpoint returns error status",
			errContains: "assign request failed",
			setup: func(t *testing.T, state *testState) {
				// create a pod that returns 500 from its assign endpoint
				errorHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte("internal server error"))
				})
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, errorHandler))
			},
			check: func(t *testing.T, state *testState) {
				// ensure the pod was deleted because the assign request failed
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}

			tc.setup(t, state)

			cfg := state.cfg
			if cfg == nil {
				cfg = testConfig()
			}

			ctrl := New(cfg, nil, state.fakeKubernetes, nil)

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			state.instance, err = ctrl.assignPod(ctx, state.fn)
			if tc.errContains != "" {
				assert.ErrorContains(t, err, tc.errContains)
			} else if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, state)
		})
	}
}

// TestAssignPodRejectsMissingReplicaSetOwner asserts that an unassigned pod
// without a ReplicaSet `ownerReferences[0]` is refused before the SSA apply
// runs. The SSA path copies the ReplicaSet name onto the assigned pod as an
// annotation; without a ReplicaSet owner there is nothing to copy and the
// resulting Instance would be unrouteable, so assignPod fails fast.
func TestAssignPodRejectsMissingReplicaSetOwner(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	// Drop the ReplicaSet owner so assignPod hits the missing-owner branch.
	pod := fixture.NewAvailablePod(t, fn, nil)
	pod.OwnerReferences = nil
	fakeKubernetes.Tracker().Add(pod)

	ctrl := New(testConfig(), nil, fakeKubernetes, nil)
	assert.NilError(t, ctrl.startInformers(ctx))

	instance, err := ctrl.assignPod(ctx, fn)
	assert.ErrorContains(t, err, "missing ReplicaSet owner reference")
	assert.Assert(t, instance == nil)
}

// TestAssignPodRejectsAlreadyAssigned asserts that pods already bound to a
// tenant are never picked as assignment candidates. The unassigned-pool
// label selector excludes pods carrying the tenant label, so giving
// assignPod no other candidate forces it to wait out the context rather
// than overwrite the existing assignment.
func TestAssignPodRejectsAlreadyAssigned(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	pod := fixture.NewAvailablePod(t, fn, nil)
	pod.Labels[key.Tenant.Label] = "other"
	fakeKubernetes.Tracker().Add(pod)

	ctrl := New(testConfig(), nil, fakeKubernetes, nil)

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	instance, err := ctrl.assignPod(ctx, fn)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Assert(t, instance == nil)

	pods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
	assert.NilError(t, err)
	assert.Assert(t, len(pods.Items) == 1)

	resultPod := pods.Items[0]
	assert.Assert(t, resultPod.Labels[key.Tenant.Label] == "other")
	delete(resultPod.Labels, key.Tenant.Label)
	ensurePodIsNotAssignedToFunction(t, resultPod)
}

func TestIsPodRunning(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		pod  *v1.Pod
		want bool
	}{
		{
			name: "running pod with IP and no deletion timestamp",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
				},
			},
			want: true,
		},
		{
			name: "pod not in running phase",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodPending,
					PodIP: "10.0.0.1",
				},
			},
			want: false,
		},
		{
			name: "running pod without IP",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "",
				},
			},
			want: false,
		},
		{
			name: "running pod with deletion timestamp",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
				},
			},
			want: false,
		},
		{
			name: "failed pod",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodFailed,
					PodIP: "10.0.0.1",
				},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := isPodRunning(tc.pod)
			assert.Assert(t, got == tc.want, "isPodRunning() = %v, want %v", got, tc.want)
		})
	}
}

func TestIsPodReady(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		pod  *v1.Pod
		want bool
	}{
		{
			name: "ready running pod",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
					Conditions: []v1.PodCondition{
						{Type: v1.PodReady, Status: v1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "ready condition false",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
					Conditions: []v1.PodCondition{
						{Type: v1.PodReady, Status: v1.ConditionFalse},
					},
				},
			},
			want: false,
		},
		{
			name: "no ready condition",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
					Conditions: []v1.PodCondition{
						{Type: v1.PodScheduled, Status: v1.ConditionTrue},
					},
				},
			},
			want: false,
		},
		{
			name: "ready condition true but pod not running",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodPending,
					PodIP: "10.0.0.1",
					Conditions: []v1.PodCondition{
						{Type: v1.PodReady, Status: v1.ConditionTrue},
					},
				},
			},
			want: false,
		},
		{
			name: "ready condition true but pod being deleted",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					PodIP: "10.0.0.1",
					Conditions: []v1.PodCondition{
						{Type: v1.PodReady, Status: v1.ConditionTrue},
					},
				},
			},
			want: false,
		},
		{
			name: "empty conditions",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Phase:      v1.PodRunning,
					PodIP:      "10.0.0.1",
					Conditions: []v1.PodCondition{},
				},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := isPodReady(tc.pod)
			assert.Assert(t, got == tc.want, "isPodReady() = %v, want %v", got, tc.want)
		})
	}
}

func TestPortFromPod(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		pod      *v1.Pod
		wantPort string
		wantErr  bool
	}{
		{
			name: "port annotation matches named port",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						key.Port.Label: "http",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Ports: []v1.ContainerPort{
								{Name: "metrics", ContainerPort: 9090},
								{Name: "http", ContainerPort: 8080},
							},
						},
					},
				},
			},
			wantPort: "8080",
		},
		{
			name: "port annotation is numeric - uses as-is",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						key.Port.Label: "3000",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Ports: []v1.ContainerPort{
								{Name: "http", ContainerPort: 8080},
							},
						},
					},
				},
			},
			wantPort: "3000",
		},
		{
			name: "port annotation doesn't match any port - uses annotation as-is",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						key.Port.Label: "nonexistent",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Ports: []v1.ContainerPort{
								{Name: "http", ContainerPort: 8080},
							},
						},
					},
				},
			},
			wantPort: "nonexistent",
		},
		{
			name: "no port annotation - uses first container port",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Ports: []v1.ContainerPort{
								{Name: "http", ContainerPort: 8080},
								{Name: "metrics", ContainerPort: 9090},
							},
						},
					},
				},
			},
			wantPort: "8080",
		},
		{
			name: "no port annotation and no container ports - error",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Annotations: map[string]string{},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Ports: []v1.ContainerPort{}},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
			port, err := ctrl.portFromPod(tc.pod)
			if tc.wantErr {
				assert.Assert(t, err != nil)
			} else {
				assert.NilError(t, err)
				assert.Assert(t, port == tc.wantPort, "got %s, want %s", port, tc.wantPort)
			}
		})
	}
}

func TestFunctionFromPod(t *testing.T) {
	t.Parallel()

	ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
	fn := fixture.NewFunction(t)
	fnJSON, _ := json.Marshal(fn)

	testCases := []struct {
		name        string
		pod         *v1.Pod
		errContains string
		check       func(*testing.T, *skipper.Function)
	}{
		{
			name: "valid pod with function annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						skipper.FunctionKey.Label: string(fnJSON),
					},
				},
			},
			check: func(t *testing.T, resultFn *skipper.Function) {
				assert.Assert(t, proto.Equal(resultFn, fn))
			},
		},
		{
			name:        "nil pod",
			pod:         nil,
			errContains: "pod is nil",
		},
		{
			name: "missing function annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			errContains: "missing function annotation",
		},
		{
			name: "invalid function JSON",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						skipper.FunctionKey.Label: "not valid json",
					},
				},
			},
			errContains: "failed to unmarshal function from pod annotation",
		},
		{
			name: "invalid function - missing required fields",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						skipper.FunctionKey.Label: `{"namespace":"","deployment":"","tenant":""}`,
					},
				},
			},
			errContains: "invalid function in pod annotation",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resultFn, err := ctrl.functionFromPod(tc.pod)
			if tc.errContains != "" {
				assert.ErrorContains(t, err, tc.errContains)
			} else {
				assert.NilError(t, err)
				if tc.check != nil {
					tc.check(t, resultFn)
				}
			}
		})
	}
}

func TestInstanceFromPod(t *testing.T) {
	t.Parallel()

	ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
	fn := fixture.NewFunction(t)
	fnJSON, _ := json.Marshal(fn)

	testCases := []struct {
		name        string
		pod         *v1.Pod
		errContains string
		check       func(*testing.T, *skipper.Instance)
	}{
		{
			name: "valid pod with all annotations",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: fn.GetNamespace(),
					Labels: map[string]string{
						key.Tenant.Label:     fn.GetTenant(),
						key.Deployment.Label: fn.GetDeployment(),
					},
					Annotations: map[string]string{
						skipper.FunctionKey.Label: string(fnJSON),
						key.ReplicaSet.Label:      "test-replicaset",
						key.AssignedAt.Label:      "2024-01-01T00:00:00Z",
						key.ReadyAt.Label:         "2024-01-01T00:00:01Z",
					},
				},
				Status: v1.PodStatus{
					PodIP: "10.0.0.1",
					Phase: v1.PodRunning,
					Conditions: []v1.PodCondition{
						{Type: v1.PodReady, Status: v1.ConditionTrue},
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Ports: []v1.ContainerPort{{ContainerPort: 8080}}},
					},
				},
			},
			check: func(t *testing.T, instance *skipper.Instance) {
				assert.Assert(t, instance.GetName() == "test-pod")
				assert.Assert(t, instance.GetAddr() == "10.0.0.1:8080")
				assert.Assert(t, instance.GetReplicaSet() == "test-replicaset")
				assert.Assert(t, instance.GetAssignedAt().AsTime().Format(time.RFC3339) == "2024-01-01T00:00:00Z")
				assert.Assert(t, instance.GetReadyAt().AsTime().Format(time.RFC3339) == "2024-01-01T00:00:01Z")
			},
		},
		{
			name:        "nil pod",
			pod:         nil,
			errContains: "pod is nil",
		},
		{
			name: "missing function annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Annotations: map[string]string{},
				},
			},
			errContains: "missing function annotation",
		},
		{
			name: "invalid function JSON",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						skipper.FunctionKey.Label: "not valid json",
					},
				},
			},
			errContains: "failed to unmarshal function",
		},
		{
			name: "invalid function - missing required fields",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						skipper.FunctionKey.Label: `{"namespace":"","deployment":"","tenant":""}`,
					},
				},
			},
			errContains: "invalid function in pod annotation",
		},
		{
			name: "missing replica set annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						skipper.FunctionKey.Label: string(fnJSON),
					},
				},
			},
			errContains: "missing replica set annotation",
		},
		{
			name: "invalid assigned at timestamp",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						skipper.FunctionKey.Label: string(fnJSON),
						key.ReplicaSet.Label:      "test-replicaset",
						key.AssignedAt.Label:      "not-a-timestamp",
					},
				},
			},
			errContains: "failed to parse assigned at annotation",
		},
		{
			name: "invalid ready at timestamp",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						skipper.FunctionKey.Label: string(fnJSON),
						key.ReplicaSet.Label:      "test-replicaset",
						key.AssignedAt.Label:      "2024-01-01T00:00:00Z",
						key.ReadyAt.Label:         "not-a-timestamp",
					},
				},
				Status: v1.PodStatus{
					PodIP: "10.0.0.1",
					Phase: v1.PodRunning,
					Conditions: []v1.PodCondition{
						{Type: v1.PodReady, Status: v1.ConditionTrue},
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Ports: []v1.ContainerPort{{ContainerPort: 8080}}},
					},
				},
			},
			errContains: "failed to parse ready at annotation",
		},
		{
			name: "unready pod with ready annotation - ReadyAt should be zero",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						skipper.FunctionKey.Label: string(fnJSON),
						key.ReplicaSet.Label:      "test-replicaset",
						key.AssignedAt.Label:      "2024-01-01T00:00:00Z",
						key.ReadyAt.Label:         "2024-01-01T00:00:01Z",
					},
				},
				Status: v1.PodStatus{
					PodIP: "10.0.0.1",
					Phase: v1.PodRunning,
					Conditions: []v1.PodCondition{
						{Type: v1.PodReady, Status: v1.ConditionFalse}, // Pod is NOT ready
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Ports: []v1.ContainerPort{{ContainerPort: 8080}}},
					},
				},
			},
			check: func(t *testing.T, instance *skipper.Instance) {
				assert.Assert(t, !instance.HasReadyAt(), "ReadyAt should be zero when pod is not ready")
				assert.Assert(t, instance.HasAssignedAt(), "AssignedAt should still be set")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			instance, err := ctrl.instanceFromPod(tc.pod)
			if tc.errContains != "" {
				assert.ErrorContains(t, err, tc.errContains)
			} else {
				assert.NilError(t, err)
				if tc.check != nil {
					tc.check(t, instance)
				}
			}
		})
	}
}

func TestRefreshMetrics(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
	fakeKubernetesMetrics := fakekubernetesmetrics.NewSimpleClientset() //nolint:staticcheck // NewClientset isn't generated for this package
	ctrl := New(testConfig(), nil, fakeKubernetes, fakeKubernetesMetrics)

	// create multiple assigned pods
	pod1 := fixture.NewAssignedPod(t, fn, nil)
	pod2 := fixture.NewAssignedPod(t, fn, nil)
	err := fakeKubernetes.Tracker().Add(pod1)
	assert.NilError(t, err)
	err = fakeKubernetes.Tracker().Add(pod2)
	assert.NilError(t, err)

	// add metrics for both pods
	fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, pod1, "100m", "128Mi"))
	fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, pod2, "200m", "256Mi"))

	// start informers so the pod lister is populated
	err = ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// verify cache is empty before refresh
	assert.Assert(t, ctrl.podMetrics.Size() == 0, "cache should be empty before refresh")

	// refresh metrics
	ctrl.refreshMetrics(ctx)

	// verify both metrics are in the cache
	metric1, ok := ctrl.podMetrics.Load(fn.GetNamespace() + "/" + pod1.Name)
	assert.Assert(t, ok, "pod1 metrics should be in cache")
	assert.Assert(t, metric1.Containers[0].Usage.Cpu().MilliValue() == 100)

	metric2, ok := ctrl.podMetrics.Load(fn.GetNamespace() + "/" + pod2.Name)
	assert.Assert(t, ok, "pod2 metrics should be in cache")
	assert.Assert(t, metric2.Containers[0].Usage.Cpu().MilliValue() == 200)
}

func TestRefreshMetricsGarbageCollection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
	fakeKubernetesMetrics := fakekubernetesmetrics.NewSimpleClientset() //nolint:staticcheck // NewClientset isn't generated for this package
	ctrl := New(testConfig(), nil, fakeKubernetes, fakeKubernetesMetrics)

	// create an assigned pod
	assignedPod := fixture.NewAssignedPod(t, fn, nil)
	err := fakeKubernetes.Tracker().Add(assignedPod)
	assert.NilError(t, err)

	// add metrics for the assigned pod
	fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, "100m", "128Mi"))

	// start informers so the pod lister is populated
	err = ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// seed a stale entry in the cache for a pod that doesn't exist
	staleKey := fn.GetNamespace() + "/nonexistent-pod"
	ctrl.podMetrics.Store(staleKey, metricsv1beta1.PodMetrics{})

	// verify both entries exist before refresh
	_, staleExists := ctrl.podMetrics.Load(staleKey)
	assert.Assert(t, staleExists, "stale entry should exist before refresh")

	// refresh metrics - this should garbage collect the stale entry
	ctrl.refreshMetrics(ctx)

	// verify the stale entry was removed
	_, staleExists = ctrl.podMetrics.Load(staleKey)
	assert.Assert(t, !staleExists, "stale entry should be garbage collected")

	// verify the valid entry for the existing pod is still there
	validKey := fn.GetNamespace() + "/" + assignedPod.Name
	_, validExists := ctrl.podMetrics.Load(validKey)
	assert.Assert(t, validExists, "valid entry should still exist after refresh")
}

func TestGetInstances(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		instances      []*skipper.Instance
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "returns single valid instance",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Name = "pod-1"
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1, "should have 1 instance")
				assert.Equal(t, "pod-1", state.instances[0].GetName())
				assert.Assert(t, proto.Equal(state.instances[0].GetFunction(), state.fn))
			},
		},
		{
			name: "returns multiple valid instances",
			setup: func(t *testing.T, state *testState) {
				pod1 := fixture.NewAssignedPod(t, state.fn, nil)
				pod1.Name = "pod-1"
				err := state.fakeKubernetes.Tracker().Add(pod1)
				assert.NilError(t, err)

				pod2 := fixture.NewAssignedPod(t, state.fn, nil)
				pod2.Name = "pod-2"
				err = state.fakeKubernetes.Tracker().Add(pod2)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 2, "should have 2 instances")
				names := []string{state.instances[0].GetName(), state.instances[1].GetName()}
				assert.Assert(t, slices.Contains(names, "pod-1"))
				assert.Assert(t, slices.Contains(names, "pod-2"))
			},
		},
		{
			name: "returns empty list when no pods exist",
			setup: func(t *testing.T, state *testState) {
				// No pods added
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0, "should have no instances")
			},
		},
		{
			name: "filters out pods with different function identity",
			setup: func(t *testing.T, state *testState) {
				// Add pod for the function
				pod1 := fixture.NewAssignedPod(t, state.fn, nil)
				pod1.Name = "pod-1"
				err := state.fakeKubernetes.Tracker().Add(pod1)
				assert.NilError(t, err)

				// Add pod for different function identity (different tenant)
				fn2 := proto.Clone(state.fn).(*skipper.Function)
				fn2.SetTenant("different-tenant")
				pod2 := fixture.NewAssignedPod(t, fn2, nil)
				pod2.Name = "pod-2"
				err = state.fakeKubernetes.Tracker().Add(pod2)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1, "should have 1 instance for this function")
				assert.Equal(t, "pod-1", state.instances[0].GetName())
			},
		},
		{
			name: "includes pods with different metadata but same identity",
			setup: func(t *testing.T, state *testState) {
				// Add pod for the function
				pod1 := fixture.NewAssignedPod(t, state.fn, nil)
				pod1.Name = "pod-1"
				err := state.fakeKubernetes.Tracker().Add(pod1)
				assert.NilError(t, err)

				// Add pod with different metadata (same identity)
				fn2 := proto.Clone(state.fn).(*skipper.Function)
				fn2.SetMetadata("different")
				pod2 := fixture.NewAssignedPod(t, fn2, nil)
				pod2.Name = "pod-2"
				err = state.fakeKubernetes.Tracker().Add(pod2)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 2, "should have 2 instances (same identity, different metadata)")
			},
		},
		{
			name: "includes unready instances",
			setup: func(t *testing.T, state *testState) {
				// Add ready pod
				readyPod := fixture.NewAssignedPod(t, state.fn, nil)
				readyPod.Name = "ready-pod"
				err := state.fakeKubernetes.Tracker().Add(readyPod)
				assert.NilError(t, err)

				// Add unready pod (no ReadyAt annotation)
				unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
				unreadyPod.Name = "unready-pod"
				delete(unreadyPod.Annotations, key.ReadyAt.Label)
				unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				err = state.fakeKubernetes.Tracker().Add(unreadyPod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				// getInstances returns both ready and unready instances
				assert.Assert(t, len(state.instances) == 2, "should have 2 instances (ready and unready)")
				names := []string{state.instances[0].GetName(), state.instances[1].GetName()}
				assert.Assert(t, slices.Contains(names, "ready-pod"))
				assert.Assert(t, slices.Contains(names, "unready-pod"))
				// Verify unready instance has zero ReadyAt
				for _, inst := range state.instances {
					if inst.GetName() == "unready-pod" {
						assert.Assert(t, !inst.HasReadyAt(), "unready instance should have zero ReadyAt")
					}
				}
			},
		},
		{
			name: "missing replica set annotation deletes pod and continues",
			setup: func(t *testing.T, state *testState) {
				// Create a pod with missing replica set annotation
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Name = "invalid-pod"
				delete(pod.Annotations, key.ReplicaSet.Label)
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)

				// Add a valid pod to ensure processing continues
				validPod := fixture.NewAssignedPod(t, state.fn, nil)
				validPod.Name = "valid-pod"
				err = state.fakeKubernetes.Tracker().Add(validPod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				// Should return 1 valid instance, invalid pod should be deleted
				assert.Assert(t, len(state.instances) == 1, "should have 1 valid instance")
				assert.Equal(t, "valid-pod", state.instances[0].GetName())

				// Verify invalid pod was deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1, "should have 1 pod remaining")
				assert.Equal(t, "valid-pod", pods.Items[0].Name)
			},
		},
		{
			name: "malformed assigned at timestamp deletes pod and continues",
			setup: func(t *testing.T, state *testState) {
				// Create a pod with malformed assigned at timestamp
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Name = "invalid-pod"
				pod.Annotations[key.AssignedAt.Label] = "not-a-valid-timestamp"
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)

				// Add a valid pod to ensure processing continues
				validPod := fixture.NewAssignedPod(t, state.fn, nil)
				validPod.Name = "valid-pod"
				err = state.fakeKubernetes.Tracker().Add(validPod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				// Should return 1 valid instance, invalid pod should be deleted
				assert.Assert(t, len(state.instances) == 1, "should have 1 valid instance")
				assert.Equal(t, "valid-pod", state.instances[0].GetName())

				// Verify invalid pod was deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1, "should have 1 pod remaining")
				assert.Equal(t, "valid-pod", pods.Items[0].Name)
			},
		},
		{
			name: "malformed ready at timestamp deletes pod and continues",
			setup: func(t *testing.T, state *testState) {
				// Create a pod with malformed ready at timestamp
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Name = "invalid-pod"
				pod.Annotations[key.ReadyAt.Label] = "not-a-valid-timestamp"
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)

				// Add a valid pod to ensure processing continues
				validPod := fixture.NewAssignedPod(t, state.fn, nil)
				validPod.Name = "valid-pod"
				err = state.fakeKubernetes.Tracker().Add(validPod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				// Should return 1 valid instance, invalid pod should be deleted
				assert.Assert(t, len(state.instances) == 1, "should have 1 valid instance")
				assert.Equal(t, "valid-pod", state.instances[0].GetName())

				// Verify invalid pod was deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1, "should have 1 pod remaining")
				assert.Equal(t, "valid-pod", pods.Items[0].Name)
			},
		},
		{
			name: "multiple invalid pods are all deleted",
			setup: func(t *testing.T, state *testState) {
				// Add multiple invalid pods
				invalidPod1 := fixture.NewAssignedPod(t, state.fn, nil)
				invalidPod1.Name = "invalid-pod-1"
				delete(invalidPod1.Annotations, key.ReplicaSet.Label)
				err := state.fakeKubernetes.Tracker().Add(invalidPod1)
				assert.NilError(t, err)

				invalidPod2 := fixture.NewAssignedPod(t, state.fn, nil)
				invalidPod2.Name = "invalid-pod-2"
				invalidPod2.Annotations[key.AssignedAt.Label] = "invalid"
				err = state.fakeKubernetes.Tracker().Add(invalidPod2)
				assert.NilError(t, err)

				// Add a valid pod
				validPod := fixture.NewAssignedPod(t, state.fn, nil)
				validPod.Name = "valid-pod"
				err = state.fakeKubernetes.Tracker().Add(validPod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				// Should return 1 valid instance, all invalid pods should be deleted
				assert.Assert(t, len(state.instances) == 1, "should have 1 valid instance")
				assert.Equal(t, "valid-pod", state.instances[0].GetName())

				// Verify all invalid pods were deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1, "should have 1 pod remaining")
				assert.Equal(t, "valid-pod", pods.Items[0].Name)
			},
		},
		{
			name: "no error returned when invalid pods are deleted",
			setup: func(t *testing.T, state *testState) {
				// Create an invalid pod - should not cause getInstances to return an error
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				delete(pod.Annotations, key.ReplicaSet.Label)
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				// Should return no instances but no error
				assert.Assert(t, len(state.instances) == 0, "should have no instances")
				// Verify pod was deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0, "invalid pod should be deleted")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}

			tc.setup(t, state)

			ctrl := New(testConfig(), nil, state.fakeKubernetes, nil)
			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			// Test getInstances directly (not getReadyInstances)
			state.instances, err = ctrl.getInstances(ctx, state.fn)
			assert.NilError(t, err, "getInstances should not return error when invalid pods are deleted")

			tc.check(t, state)
		})
	}
}

func TestGetReadyInstances(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		instances      []*skipper.Instance
	}

	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "one",
			setup: func(t *testing.T, state *testState) {
				err := state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1)
			},
		},
		{
			name: "many",
			setup: func(t *testing.T, state *testState) {
				err := state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				assert.NilError(t, err)

				err = state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 2)
			},
		},
		{
			name: "deleted",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			name: "failed",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Status.Phase = v1.PodFailed
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			name: "no pod IP",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Status.PodIP = ""
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			name: "unready",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				err := state.fakeKubernetes.Tracker().Add(pod)
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			name: "different metadata same identity",
			setup: func(t *testing.T, state *testState) {
				// create a pod with different metadata than state.fn (same identity)
				fn := proto.Clone(state.fn).(*skipper.Function)
				fn.SetMetadata("different")
				err := state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1)
			},
		},
		{
			name: "different tenant",
			setup: func(t *testing.T, state *testState) {
				// create a pod with different tenant (different identity)
				fn := proto.Clone(state.fn).(*skipper.Function)
				fn.SetTenant("different-tenant")
				err := state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				assert.NilError(t, err)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}

			tc.setup(t, state)

			ctrl := New(testConfig(), nil, state.fakeKubernetes, nil)
			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			state.instances, err = ctrl.getReadyInstances(ctx, state.fn)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, state)
		})
	}
}

func TestPatchPod(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
		pod            *v1.Pod
	}

	testCases := []struct {
		name        string
		err         error
		errContains string
		patches     []byte
		setup       func(*testing.T, *testState)
		check       func(*testing.T, *testState)
	}{
		{
			name:    "successfully patches pod and updates cache",
			patches: []byte(`[{ "op": "add", "path": "/metadata/annotations/test-annotation", "value": "test-value" }]`),
			setup: func(t *testing.T, state *testState) {
				state.pod = fixture.NewAvailablePod(t, state.fn, nil)
				state.pod.Name = "test-pod"
				state.fakeKubernetes.Tracker().Add(state.pod)
			},
			check: func(t *testing.T, state *testState) {
				// Verify pod was patched
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				patchedPod := pods.Items[0]

				// Verify patch was applied (we'll patch with a new annotation)
				assert.Assert(t, patchedPod.Annotations["test-annotation"] == "test-value")

				// Verify pod is in cache by checking it can be retrieved
				// Pod should not be in instances (it's not assigned yet), but should be in cache
				allPods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(allPods.Items) == 1)
			},
		},
		{
			name:        "returns error when pod not found",
			errContains: "not found",
			patches:     []byte(`[{ "op": "add", "path": "/metadata/annotations/test-annotation", "value": "test-value" }]`),
			setup: func(t *testing.T, state *testState) {
				// Don't add any pods
			},
			check: func(t *testing.T, state *testState) {
				// No pod should exist
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
			},
		},
		{
			name:        "returns error when patch is invalid",
			errContains: "invalid",
			patches:     []byte(`[{ "op": "invalid", "path": "/invalid", "value": "test" }]`),
			setup: func(t *testing.T, state *testState) {
				state.pod = fixture.NewAvailablePod(t, state.fn, nil)
				state.pod.Name = "test-pod"
				state.fakeKubernetes.Tracker().Add(state.pod)
			},
			check: func(t *testing.T, state *testState) {
				// Pod should still exist (invalid patch shouldn't delete it)
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}

			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)
			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			tc.setup(t, state)

			pod, err := state.ctrl.patchPod(ctx, state.fn.GetNamespace(), "test-pod", types.JSONPatchType, tc.patches, metav1.PatchOptions{FieldManager: key.Controller.Label})
			if tc.errContains != "" {
				assert.ErrorContains(t, err, tc.errContains)
				assert.Assert(t, pod == nil)
			} else if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				assert.Assert(t, pod == nil)
			} else {
				assert.NilError(t, err)
				assert.Assert(t, pod != nil)
			}

			tc.check(t, state)
		})
	}
}

func TestDeletePod(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
		pod            *v1.Pod
	}

	testCases := []struct {
		name        string
		err         error
		errContains string
		setup       func(*testing.T, *testState)
		check       func(*testing.T, *testState)
	}{
		{
			name: "successfully deletes pod and removes from cache",
			setup: func(t *testing.T, state *testState) {
				state.pod = fixture.NewAssignedPod(t, state.fn, nil)
				state.pod.Name = "test-pod"
				state.fakeKubernetes.Tracker().Add(state.pod)
			},
			check: func(t *testing.T, state *testState) {
				// Verify pod was deleted from API
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0, "pod should be deleted from API")

				// Verify pod is not in cache (should not appear in getInstances)
				instances, err := state.ctrl.getInstances(t.Context(), state.fn)
				assert.NilError(t, err)
				assert.Assert(t, len(instances) == 0, "pod should be removed from cache")
			},
		},
		{
			name:        "returns error when pod not found",
			errContains: "not found",
			setup: func(t *testing.T, state *testState) {
				// Don't add any pods
			},
			check: func(t *testing.T, state *testState) {
				// No pod should exist
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
			},
		},
		{
			name: "deletes pod even when namespace lister not found",
			setup: func(t *testing.T, state *testState) {
				// Create a pod in the function namespace but delete it before informers sync
				// This simulates the case where namespace lister might not be ready
				state.pod = fixture.NewAssignedPod(t, state.fn, nil)
				state.pod.Name = "test-pod"
				state.fakeKubernetes.Tracker().Add(state.pod)
			},
			check: func(t *testing.T, state *testState) {
				// Pod should be deleted from API
				// The deletePod function will succeed in deleting from API
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0, "pod should be deleted from API")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}

			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			tc.setup(t, state)

			// If a pod was set up, wait for it to appear in the cache before deleting it.
			// This ensures the pod is in cache when deletePod is called, preventing
			// a race condition where the informer might process the ADD event after
			// the DELETE event, causing the pod to reappear in cache.
			if state.pod != nil {
				poll.WaitOn(t, func(t poll.LogT) poll.Result {
					instances, err := state.ctrl.getInstances(ctx, state.fn)
					if err != nil {
						return poll.Error(err)
					}
					if len(instances) == 0 {
						return poll.Continue("pod not yet in cache")
					}
					// Found the pod in cache
					return poll.Success()
				}, poll.WithTimeout(2*time.Second), poll.WithDelay(50*time.Millisecond))
			}

			err = state.ctrl.deletePod(ctx, state.fn.GetNamespace(), "test-pod", metav1.DeleteOptions{})
			if tc.errContains != "" {
				assert.ErrorContains(t, err, tc.errContains)
			} else if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, state)
		})
	}
}

func TestDeletePodWhenNotInCache(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
	ctrl := New(testConfig(), nil, fakeKubernetes, nil)

	// Add pod directly to tracker but not to cache
	// Don't start informers, so pod won't be in cache
	pod := fixture.NewAssignedPod(t, fn, nil)
	pod.Name = "test-pod"
	fakeKubernetes.Tracker().Add(pod)

	// Delete pod - should succeed even though it's not in cache
	err := ctrl.deletePod(ctx, fn.GetNamespace(), "test-pod", metav1.DeleteOptions{})
	assert.NilError(t, err)

	// Verify pod was deleted from API
	pods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{})
	assert.NilError(t, err)
	assert.Assert(t, len(pods.Items) == 0, "pod should be deleted from API")
}

func TestDeletePodMultiplePods(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
	ctrl := New(testConfig(), nil, fakeKubernetes, nil)

	// Setup pods before starting informers so they're in the cache when we check
	pod1 := fixture.NewAssignedPod(t, fn, nil)
	pod1.Name = "pod-1"
	fakeKubernetes.Tracker().Add(pod1)

	pod2 := fixture.NewAssignedPod(t, fn, nil)
	pod2.Name = "pod-2"
	fakeKubernetes.Tracker().Add(pod2)

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Verify both pods exist initially
	instances, err := ctrl.getInstances(ctx, fn)
	assert.NilError(t, err)
	assert.Assert(t, len(instances) == 2, "should have 2 instances initially")

	// Delete first pod
	err = ctrl.deletePod(ctx, fn.GetNamespace(), "pod-1", metav1.DeleteOptions{})
	assert.NilError(t, err)

	// Verify only one pod remains
	instances, err = ctrl.getInstances(ctx, fn)
	assert.NilError(t, err)
	assert.Assert(t, len(instances) == 1, "should have 1 instance remaining, got %d", len(instances))
	if len(instances) == 1 {
		assert.Assert(t, instances[0].GetName() == "pod-2", "remaining instance should be pod-2, got %s", instances[0].GetName())
	}
}
