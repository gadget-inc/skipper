package controller

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	fakekubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func ensureInstanceIsAssignedToPod(t *testing.T, instance *function.Instance, pod v1.Pod) {
	assert.Assert(t, instance.Deployment == pod.Labels[key.Deployment.Label])
	assert.Assert(t, instance.Tenant == pod.Labels[key.Tenant.Label])

	fnJSON, err := json.Marshal(instance.Function)
	assert.NilError(t, err)
	assert.Assert(t, string(fnJSON) == pod.Annotations[key.Function.Annotation])

	port, err := portFromPod(&pod)
	assert.NilError(t, err)

	assert.Assert(t, instance.Name == pod.Name)
	assert.Assert(t, instance.Addr == net.JoinHostPort(pod.Status.PodIP, port))
	assert.Assert(t, instance.ReplicaSet == pod.Annotations[key.ReplicaSet.Annotation])
	assert.Assert(t, instance.AssignedAt.Format(time.RFC3339) == pod.Annotations[key.AssignedAt.Annotation])
	assert.Assert(t, instance.ReadyAt.Format(time.RFC3339) == pod.Annotations[key.ReadyAt.Annotation])
}

func ensurePodIsNotAssignedToFunction(t *testing.T, pod v1.Pod) {
	assert.Assert(t, pod.Labels[key.Tenant.Label] == "")
	assert.Assert(t, pod.Annotations[key.Function.Annotation] == "")
	assert.Assert(t, pod.Annotations[key.ReplicaSet.Annotation] == "")
	assert.Assert(t, pod.Annotations[key.AssignedAt.Annotation] == "")
	assert.Assert(t, pod.Annotations[key.ReadyAt.Annotation] == "")
}

func TestConvergeNamespace(t *testing.T) {
	type testState struct {
		fn                    function.Function
		fakeKubernetes        *fake.Clientset
		fakeKubernetesMetrics *fakekubernetesmetrics.Clientset
		ctrl                  *Controller
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "scale up because of cpu usage",
			setup: func(t *testing.T, state *testState) {
				// seed kubernetes with a current replica set and an available pod
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))

				// seed kubernetes with an assigned pod
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				// seed kubernetes with pod metrics for the assigned pod at 2x target CPU usage and 1x target memory usage
				cpuUsage := strconv.Itoa(state.fn.Scale.TargetCPUUsageMilli*2) + "m"    // 2x target
				memoryUsage := strconv.Itoa(state.fn.Scale.TargetMemoryUsageMiB) + "Mi" // 1x target
				state.fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))

				// seed the supervisor with a recent heartbeat
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now()})
			},
			check: func(t *testing.T, state *testState) {
				// ensure both pods are now assigned to the function because we scaled up to 2 instances
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)
				ensurePodIsAssignedToFunction(t, pods.Items[0], state.fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], state.fn)
			},
		},
		{
			name: "scale up because of memory usage",
			setup: func(t *testing.T, state *testState) {
				// seed kubernetes with a current replica set and an available pod
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))

				// seed kubernetes with an assigned pod
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				// seed kubernetes metrics with 1x target CPU usage and 2x target memory usage for the assigned pod
				cpuUsage := strconv.Itoa(state.fn.Scale.TargetCPUUsageMilli) + "m"        // 1x target
				memoryUsage := strconv.Itoa(state.fn.Scale.TargetMemoryUsageMiB*2) + "Mi" // 2x target
				state.fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))

				// seed the supervisor with a recent heartbeat
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now()})
			},
			check: func(t *testing.T, state *testState) {
				// ensure both pods are now assigned to the function because we scaled up to 2 instances
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)
				ensurePodIsAssignedToFunction(t, pods.Items[0], state.fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], state.fn)
			},
		},
		{
			name: "scale up because of in-flight requests",
			setup: func(t *testing.T, state *testState) {
				// seed kubernetes with a current replica set and an available pod
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))

				// seed kubernetes with an assigned pod
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				// seed kubernetes metrics with 1x target CPU usage and 1x target memory usage for the assigned pod
				cpuUsage := strconv.Itoa(state.fn.Scale.TargetCPUUsageMilli) + "m"      // 1x target
				memoryUsage := strconv.Itoa(state.fn.Scale.TargetMemoryUsageMiB) + "Mi" // 1x target
				state.fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))

				// seed the supervisor with 2 heartbeats across 2 routers with 1x target in-flight requests each
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now(), InFlightRequests: state.fn.Scale.TargetInFlightRequests})
				supervisor.routerHeartbeats.Store(fixture.RouterIP2, function.Heartbeat{Function: state.fn, Timestamp: time.Now(), InFlightRequests: state.fn.Scale.TargetInFlightRequests})
			},
			check: func(t *testing.T, state *testState) {
				// ensure both pods are now assigned to the function because we scaled up to 2 instances
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)
				ensurePodIsAssignedToFunction(t, pods.Items[0], state.fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], state.fn)
			},
		},
		{
			name: "scale down due to all metrics below target",
			setup: func(t *testing.T, state *testState) {
				// seed kubernetes with a current replica set
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// seed kubernetes with 2 assigned pods
				assignedPod1 := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod1.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				assignedPod1.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod1)

				assignedPod2 := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod2.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				assignedPod2.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod2)

				// seed kubernetes metrics with 0.5x target CPU usage and 0.5x target memory usage for the assigned pods
				cpuUsage := strconv.Itoa(state.fn.Scale.TargetCPUUsageMilli/2) + "m"      // 0.5x target
				memoryUsage := strconv.Itoa(state.fn.Scale.TargetMemoryUsageMiB/2) + "Mi" // 0.5x target
				state.fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod1, cpuUsage, memoryUsage))
				state.fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod2, cpuUsage, memoryUsage))

				// seed the supervisor with a recent heartbeat that has 0.5x target in-flight requests
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now(), InFlightRequests: state.fn.Scale.TargetInFlightRequests / 2})
			},
			check: func(t *testing.T, state *testState) {
				// ensure only 1 pod is assigned to the function because we scaled down to 1 instance
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensurePodIsAssignedToFunction(t, pods.Items[0], state.fn)
			},
		},
		{
			name: "no scale due to metrics at target",
			setup: func(t *testing.T, state *testState) {
				// seed kubernetes with a current replica set
				state.fn.Scale.TargetMemoryUsageMiB = 0 // don't scale on memory
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// seed the supervisor with 2 heartbeats across 2 routers with 0.5x target in-flight requests each
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now(), InFlightRequests: state.fn.Scale.TargetInFlightRequests / 2})
				supervisor.routerHeartbeats.Store(fixture.RouterIP2, function.Heartbeat{Function: state.fn, Timestamp: time.Now(), InFlightRequests: state.fn.Scale.TargetInFlightRequests / 2})

				// seed kubernetes with an assigned pod and an available pod
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HPAInitialReadinessDelay).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod)
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))

				// seed kubernetes metrics with 1x target CPU usage and 2x target memory usage for the assigned pod
				cpuUsage := strconv.Itoa(state.fn.Scale.TargetCPUUsageMilli) + "m"
				memoryUsage := strconv.Itoa(state.fn.Scale.TargetMemoryUsageMiB*2) + "Mi" // should be ignored because target memory usage is 0
				state.fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))
			},
			check: func(t *testing.T, state *testState) {
				// ensure there are still 2 pods because we didn't scale
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)

				// ensure only 1 pod is assigned to the function because we didn't scale
				var assignedPod v1.Pod
				var unassignedPod v1.Pod

				if pods.Items[0].Annotations[key.Function.Annotation] == "" {
					unassignedPod = pods.Items[0]
					assignedPod = pods.Items[1]
				} else {
					unassignedPod = pods.Items[1]
					assignedPod = pods.Items[0]
				}

				ensurePodIsAssignedToFunction(t, assignedPod, state.fn)
				ensurePodIsNotAssignedToFunction(t, unassignedPod)
			},
		},
		{
			name: "scale to 0 due to no heartbeat",
			setup: func(t *testing.T, state *testState) {
				// seed kubernetes with a current replica set
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// seed kubernetes with an assigned pod that has been assigned longer than a heartbeat timeout
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HeartbeatTimeout).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HeartbeatTimeout).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				// seed the supervisor without a heartbeat for the function
				_ = state.ctrl.supervisor(state.fn)
			},
			check: func(t *testing.T, state *testState) {
				// ensure the assigned pod was deleted because it has been assigned longer than a heartbeat timeout and doesn't have an associated heartbeat
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)

				// ensure the supervisor was deleted because it doesn't have an associated heartbeat
				assert.Assert(t, state.ctrl.supervisors.Size() == 0)
			},
		},
		{
			name: "scale to 0 due to heartbeat timeout",
			setup: func(t *testing.T, state *testState) {
				// seed kubernetes with a current replica set
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// seed kubernetes with an assigned pod that has been assigned longer than a heartbeat timeout
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HeartbeatTimeout).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HeartbeatTimeout).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				// seed the supervisor with a heartbeat that has expired
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now().Add(-state.ctrl.config.HeartbeatTimeout)})
			},
			check: func(t *testing.T, state *testState) {
				// ensure the assigned pod was deleted because it has been assigned longer than a heartbeat timeout and its heartbeat has expired
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)

				// ensure the supervisor was deleted because it doesn't have an associated heartbeat that hasn't expired
				assert.Assert(t, state.ctrl.supervisors.Size() == 0)
			},
		},
		{
			name: "no heartbeat but controller just started",
			setup: func(t *testing.T, state *testState) {
				// set the controller startedAt time to now
				state.ctrl.startedAt = time.Now()

				// seed kubernetes with a current replica set
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// seed kubernetes with an assigned pod that has been assigned longer than a heartbeat timeout
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod.Annotations[key.ReadyAt.Annotation] = time.Now().Add(-state.ctrl.config.HeartbeatTimeout).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-state.ctrl.config.HeartbeatTimeout).Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(assignedPod)
			},
			check: func(t *testing.T, state *testState) {
				// ensure the assigned pod is still assigned hasn't been deleted because the controller just started and hasn't had a chance to receive heartbeats from routers
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensurePodIsAssignedToFunction(t, pods.Items[0], state.fn)
			},
		},
		{
			name: "stale instance",
			setup: func(t *testing.T, state *testState) {
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now()})

				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				currentReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				currentReplicaSet.Status.Replicas = 0 // simulate a stale replica set
				state.fakeKubernetes.Tracker().Add(currentReplicaSet)
				state.fakeKubernetes.Tracker().Add(fixture.NewReplicaSet(t, state.fn)) // add a new replica set with available replicas
			},
			check: func(t *testing.T, state *testState) {
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
			},
		},
		{
			name: "stale instance without enough available pods",
			setup: func(t *testing.T, state *testState) {
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now()})

				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				currentReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				currentReplicaSet.Status.Replicas = 0 // simulate a stale replica set
				state.fakeKubernetes.Tracker().Add(currentReplicaSet)

				newReplicaSet := fixture.NewReplicaSet(t, state.fn)
				newReplicaSet.Status.Replicas = 10
				newReplicaSet.Status.AvailableReplicas = 2 // simulate a new replica set with less than 1/4 available replicas
				state.fakeKubernetes.Tracker().Add(newReplicaSet)
			},
			check: func(t *testing.T, state *testState) {
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensurePodIsAssignedToFunction(t, pods.Items[0], state.fn)
			},
		},
		{
			name: "different controller pod",
			setup: func(t *testing.T, state *testState) {
				// seed the supervisor with a heartbeat that has expired
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now().Add(-state.ctrl.config.HeartbeatTimeout)})

				// seed kubernetes with an assigned pod that needs to be terminated
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))

				// add a bunch of controller pods so that we're unlikely to be responsible for the assigned pod
				for i := range 10 {
					ctrlPod := fixture.NewControllerPod()
					ctrlPod.Status.PodIP = "127.0.0." + strconv.Itoa(i+2)
					state.fakeKubernetes.Tracker().Add(ctrlPod)
				}
			},
			check: func(t *testing.T, state *testState) {
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensurePodIsAssignedToFunction(t, pods.Items[0], state.fn) // the assigned pod should still be around because we're not responsible for it
			},
		},
		{
			name: "extra ready instance",
			setup: func(t *testing.T, state *testState) {
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now()})

				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// add an extra ready instance
				for range state.fn.Scale.MaxInstances + 1 {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}
			},
			check: func(t *testing.T, state *testState) {
				// ensure the extra ready instance was deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == state.fn.Scale.MaxInstances)
				for _, pod := range pods.Items {
					ensurePodIsAssignedToFunction(t, pod, state.fn)
				}
			},
		},
		{
			name: "extra unready instance",
			setup: func(t *testing.T, state *testState) {
				supervisor := state.ctrl.supervisor(state.fn)
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{Function: state.fn, Timestamp: time.Now()})

				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// add max ready instances
				for range state.fn.Scale.MaxInstances {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}

				// add an extra unready instance
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				// ensure the extra unready instance was not deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == state.fn.Scale.MaxInstances+1)
				for _, pod := range pods.Items {
					ensurePodIsAssignedToFunction(t, pod, state.fn)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fn:                    fixture.NewFunction(),
				fakeKubernetes:        fake.NewClientset(fixture.NewControllerPod()),
				fakeKubernetesMetrics: fakekubernetesmetrics.NewSimpleClientset(),
			}
			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, state.fakeKubernetesMetrics)

			tc.setup(t, state)

			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			if state.ctrl.startedAt.IsZero() {
				// if the test doesn't set the startedAt time, assume the test is testing behavior that happens after the controller has been running for a while
				state.ctrl.startedAt = time.Now().Add(-(state.ctrl.config.HPADownscaleStabilization + time.Second))
			}

			err = state.ctrl.convergeNamespace(ctx, state.fn.Namespace)
			assert.NilError(t, err)
			tc.check(t, state)
		})
	}
}

func TestAssignPod(t *testing.T) {
	type testState struct {
		fn             function.Function
		cfg            *Config
		fakeKubernetes *fake.Clientset
		instance       *function.Instance
	}

	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			// Basic assignment: an available pod exists and should be assigned to the function
			name: "assigns available pod to function",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.instance.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensureInstanceIsAssignedToPod(t, state.instance, pods.Items[0])
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
					time.Sleep(100 * time.Millisecond)
					state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				}()
			},
			check: func(t *testing.T, state *testState) {
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.instance.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensureInstanceIsAssignedToPod(t, state.instance, pods.Items[0])
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
				assert.Assert(t, pod.Annotations[key.Port.Annotation] == "http")
				assert.Assert(t, pod.Spec.Containers[0].Ports[0].Name == "http")

				// add another port and make the http port the second port on the container
				pod.Spec.Containers[0].Ports = []v1.ContainerPort{
					{ContainerPort: 8080, Name: "other"},
					pod.Spec.Containers[0].Ports[0],
				}
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.instance.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensureInstanceIsAssignedToPod(t, state.instance, pods.Items[0])
			},
		},
		{
			// Missing port annotation: should fall back to first container port
			name: "uses first container port when port annotation is empty",
			setup: func(t *testing.T, state *testState) {
				pod := fixture.NewAvailablePod(t, state.fn, nil)
				pod.Annotations[key.Port.Annotation] = ""
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.instance.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensureInstanceIsAssignedToPod(t, state.instance, pods.Items[0])
			},
		},
		{
			// Already assigned: pod already has a tenant label, should not be re-assigned
			// The patch should fail because the pod is already assigned, retries until timeout
			name: "rejects pod already assigned to another tenant",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, state *testState) {
				// make getAvailablePods return assigned pods (normally filtered out)
				originalDoesNotHaveTenantSelector := doesNotHaveTenantSelector
				t.Cleanup(func() {
					doesNotHaveTenantSelector = originalDoesNotHaveTenantSelector
				})
				doesNotHaveTenantSelector = hasTenantSelector

				// add a pod that already has a tenant label
				pod := fixture.NewAvailablePod(t, state.fn, nil)
				pod.Labels[key.Tenant.Label] = "other"
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)

				// verify pod still has original tenant and wasn't re-assigned
				pod := pods.Items[0]
				assert.Assert(t, pod.Labels[key.Tenant.Label] == "other")
				delete(pod.Labels, key.Tenant.Label)
				ensurePodIsNotAssignedToFunction(t, pod)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(),
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
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, state)
		})
	}
}
