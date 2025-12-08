package controller

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
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

func init() {
	_ = function.FlagNamespaces.SetValue([]string{fixture.FunctionNamespace})
	function.FlagAssignPath.Init()
	function.FlagAssignTimeout.Init()

	FlagAvailableReplicaDivisor.Init()
	FlagHPADownscaleStabilization.Init()
	FlagHPAInitialReadinessDelay.Init()
	FlagHPATolerance.Init()
	FlagHeartbeatTimeout.Init()
	FlagPort.Init()
	_ = FlagNamespace.SetValue(fixture.ControllerNamespace)
	_ = FlagPasetoPrivateKey.SetValue(fixture.ControllerPasetoSecretKey)
	_ = FlagPodIP.SetValue(fixture.ControllerIP)
}

func ensureInstanceIsAssignedToPod(t *testing.T, instance *function.Instance, pod v1.Pod) {
	assert.Assert(t, instance.Deployment == pod.Labels[key.Deployment.Label])
	assert.Assert(t, instance.Tenant == pod.Labels[key.Tenant.Label])

	fnJSON, err := json.Marshal(instance.Function)
	assert.NilError(t, err)
	assert.Assert(t, string(fnJSON) == pod.Annotations[key.Function.Label])

	port, err := portFromPod(&pod)
	assert.NilError(t, err)

	assert.Assert(t, instance.Name == pod.Name)
	assert.Assert(t, instance.Addr == net.JoinHostPort(pod.Status.PodIP, port))
	assert.Assert(t, instance.ReplicaSet == pod.Annotations[key.ReplicaSet.Label])
	assert.Assert(t, instance.AssignedAt.Format(time.RFC3339) == pod.Annotations[key.AssignedAt.Label])
	assert.Assert(t, instance.ReadyAt.Format(time.RFC3339) == pod.Annotations[key.ReadyAt.Label])
}

func ensurePodIsAssignedToFunction(t *testing.T, pod v1.Pod, fn function.Function) {
	fnJSON, err := json.Marshal(fn)
	assert.NilError(t, err)

	assert.Assert(t, fn.Deployment == pod.Labels[key.Deployment.Label])
	assert.Assert(t, fn.Tenant == pod.Labels[key.Tenant.Label])
	assert.Assert(t, string(fnJSON) == pod.Annotations[key.Function.Label])
	assert.Assert(t, pod.Annotations[key.ReplicaSet.Label] != "")
	assert.Assert(t, pod.Annotations[key.AssignedAt.Label] != "")
	assert.Assert(t, pod.Annotations[key.ReadyAt.Label] != "")
}

func ensurePodIsNotAssignedToFunction(t *testing.T, pod v1.Pod) {
	assert.Assert(t, pod.Labels[key.Tenant.Label] == "")
	assert.Assert(t, pod.Annotations[key.Function.Label] == "")
	assert.Assert(t, pod.Annotations[key.ReplicaSet.Label] == "")
	assert.Assert(t, pod.Annotations[key.AssignedAt.Label] == "")
	assert.Assert(t, pod.Annotations[key.ReadyAt.Label] == "")
}

func countReadyAndUnreadyPods(pods []v1.Pod) (ready, unready int) {
	for _, pod := range pods {
		if isPodReady(&pod) {
			ready++
		} else if isPodRunning(&pod) {
			unready++
		}
	}
	return
}

func TestScaleNamespace(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(*testing.T, *Controller, *fake.Clientset, *fakekubernetesmetrics.Clientset) function.Function
		check func(*testing.T, *Controller, *fake.Clientset, *fakekubernetesmetrics.Clientset, function.Function)
	}{
		{
			name: "scale up because of cpu usage",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				// seed kubernetes with a current replica set and an available pod
				fn := fixture.NewFunction()
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now()}})
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				// seed kubernetes with an assigned pod
				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				// seed kubernetes with pod metrics for the assigned pod at 2x target CPU usage and 1x target memory usage
				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli*2) + "m"    // 2x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB) + "Mi" // 1x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure both pods are now assigned to the function because we scaled up to 2 instances
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], fn)
			},
		},
		{
			name: "scale up because of memory usage",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				// seed kubernetes with a current replica set and an available pod
				fn := fixture.NewFunction()
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now()}})
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				// seed kubernetes with an assigned pod
				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				// seed kubernetes metrics with 1x target CPU usage and 2x target memory usage for the assigned pod
				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli) + "m"        // 1x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB*2) + "Mi" // 2x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure both pods are now assigned to the function because we scaled up to 2 instances
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], fn)
			},
		},
		{
			name: "scale up because of in-flight requests",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				// seed kubernetes with a current replica set and an available pod
				fn := fixture.NewFunction()
				// seed the controller with 2 heartbeats across 2 routers with 1x target in-flight requests each (2x target total)
				c.routerHeartbeats.Store(fn, RouterHeartbeats{
					fixture.RouterIP:  {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests},
					fixture.RouterIP2: {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests},
				})
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				// seed kubernetes with an assigned pod
				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				// seed kubernetes metrics with 1x target CPU usage and 1x target memory usage for the assigned pod
				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli) + "m"      // 1x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB) + "Mi" // 1x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure both pods are now assigned to the function because we scaled up to 2 instances
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], fn)
			},
		},
		{
			name: "scale down due to all metrics below target",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				// seed kubernetes with a current replica set
				fn := fixture.NewFunction()
				// seed the controller with a recent heartbeat that has 0.5x target in-flight requests
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests / 2}})
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				// seed kubernetes with 2 assigned pods
				assignedPod1 := fixture.NewAssignedPod(t, fn, nil)
				assignedPod1.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod1.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod1)

				assignedPod2 := fixture.NewAssignedPod(t, fn, nil)
				assignedPod2.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod2.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod2)

				// seed kubernetes metrics with 0.5x target CPU usage and 0.5x target memory usage for the assigned pods
				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli/2) + "m"      // 0.5x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB/2) + "Mi" // 0.5x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod1, cpuUsage, memoryUsage))
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod2, cpuUsage, memoryUsage))

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure only 1 pod is assigned to the function because we scaled down to 1 instance
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
			},
		},
		{
			name: "no scale due to metrics at target",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				// seed kubernetes with a current replica set
				fn := fixture.NewFunction()
				fn.Scale.TargetMemoryUsageMiB = 0 // don't scale on memory
				// seed the controller with 2 heartbeats across 2 routers with 0.5x target in-flight requests each (1x target total)
				c.routerHeartbeats.Store(fn, RouterHeartbeats{
					fixture.RouterIP:  {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests / 2},
					fixture.RouterIP2: {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests / 2},
				})
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				// seed kubernetes with an assigned pod and an available pod
				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				// seed kubernetes metrics with 1x target CPU usage and 2x target memory usage for the assigned pod
				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli) + "m"
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB*2) + "Mi" // should be ignored because target memory usage is 0
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure there are still 2 pods because we didn't scale
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)

				// ensure only 1 pod is assigned to the function because we didn't scale
				var assignedPod v1.Pod
				var unassignedPod v1.Pod

				if pods.Items[0].Annotations[key.Function.Label] == "" {
					unassignedPod = pods.Items[0]
					assignedPod = pods.Items[1]
				} else {
					unassignedPod = pods.Items[1]
					assignedPod = pods.Items[0]
				}

				ensurePodIsAssignedToFunction(t, assignedPod, fn)
				ensurePodIsNotAssignedToFunction(t, unassignedPod)
			},
		},
		{
			name: "scale to 0 due to no heartbeat",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				// seed kubernetes with a current replica set
				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				// seed kubernetes with an assigned pod that has been assigned longer than a heartbeat timeout
				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure the assigned pod was deleted because it has been assigned longer than a heartbeat timeout and doesn't have an associated heartbeat
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)

				// ensure the function state was cleaned up because it doesn't have an associated heartbeat
				_, ok := c.routerHeartbeats.Load(fn)
				assert.Assert(t, !ok)

				_, ok = c.scaleMu.Load(fn)
				assert.Assert(t, !ok)

				_, ok = c.stabilizationWindows.Load(fn)
				assert.Assert(t, !ok)
			},
		},
		{
			name: "scale to 0 due to heartbeat timeout",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				// seed kubernetes with a current replica set
				fn := fixture.NewFunction()
				// seed the controller with a heartbeat that has expired
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now().Add(-FlagHeartbeatTimeout.Value())}})
				c.scaleMu.Store(fn, new(sync.Mutex))
				c.stabilizationWindows.Store(fn, new(StabilizationWindow))
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				// seed kubernetes with an assigned pod that has been assigned longer than a heartbeat timeout
				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure the assigned pod was deleted because it has been assigned longer than a heartbeat timeout and its heartbeat has expired
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)

				// ensure the function state was cleaned up because it doesn't have an associated heartbeat that hasn't expired
				_, ok := c.routerHeartbeats.Load(fn)
				assert.Assert(t, !ok)

				_, ok = c.scaleMu.Load(fn)
				assert.Assert(t, !ok)

				_, ok = c.stabilizationWindows.Load(fn)
				assert.Assert(t, !ok)
			},
		},
		{
			name: "no heartbeat but controller just started",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				// set the controller startedAt time to now
				c.startedAt = time.Now()

				// seed kubernetes with a current replica set
				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				// seed kubernetes with an assigned pod that has been assigned longer than a heartbeat timeout
				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure the assigned pod is still assigned hasn't been deleted because the controller just started and hasn't had a chance to receive heartbeats from routers
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
			},
		},
		{
			name: "stale instance",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now()}})

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				fakeKubernetes.Tracker().Add(assignedPod)

				currentReplicaSet := fixture.CurrentReplicaSet(t, fn)
				currentReplicaSet.Status.Replicas = 0 // simulate a stale replica set
				fakeKubernetes.Tracker().Add(currentReplicaSet)
				fakeKubernetes.Tracker().Add(fixture.NewReplicaSet(t, fn)) // add a new replica set with available replicas

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
			},
		},
		{
			name: "stale instance without enough available pods",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now()}})

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				fakeKubernetes.Tracker().Add(assignedPod)

				currentReplicaSet := fixture.CurrentReplicaSet(t, fn)
				currentReplicaSet.Status.Replicas = 0 // simulate a stale replica set
				fakeKubernetes.Tracker().Add(currentReplicaSet)

				newReplicaSet := fixture.NewReplicaSet(t, fn)
				newReplicaSet.Status.Replicas = 10
				newReplicaSet.Status.AvailableReplicas = 2 // simulate a new replica set with less than 1/4 available replicas
				fakeKubernetes.Tracker().Add(newReplicaSet)

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
			},
		},
		{
			name: "different controller pod",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				// seed the controller with a heartbeat that has expired
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now().Add(-FlagHeartbeatTimeout.Value())}})

				// seed kubernetes with an assigned pod that needs to be terminated
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))

				// add a bunch of controller pods so that we're unlikely to be responsible for the assigned pod
				for i := range 10 {
					ctrlPod := fixture.NewControllerPod()
					ctrlPod.Status.PodIP = "127.0.0." + strconv.Itoa(i+2)
					fakeKubernetes.Tracker().Add(ctrlPod)
				}

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn) // the assigned pod should still be around because we're not responsible for it
			},
		},
		{
			name: "extra ready instance",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now()}})

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				// add an extra ready instance
				for range fn.Scale.MaxInstances + 1 {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure the extra ready instance was deleted
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == fn.Scale.MaxInstances)
				for _, pod := range pods.Items {
					ensurePodIsAssignedToFunction(t, pod, fn)
				}
			},
		},
		{
			name: "extra unready instance",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.routerHeartbeats.Store(fn, RouterHeartbeats{fixture.RouterIP: {Function: fn, Timestamp: time.Now()}})

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				// add max ready instances
				for range fn.Scale.MaxInstances {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add an extra unready instance
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				fakeKubernetes.Tracker().Add(pod)

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				// ensure the extra unready instance was not deleted
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == fn.Scale.MaxInstances+1)
				for _, pod := range pods.Items {
					ensurePodIsAssignedToFunction(t, pod, fn)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			fakeKubernetesMetrics := fakekubernetesmetrics.NewSimpleClientset()
			ctrl := New(nil, fakeKubernetes, fakeKubernetesMetrics)

			fn := tc.setup(t, ctrl, fakeKubernetes, fakeKubernetesMetrics)

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			if ctrl.startedAt.IsZero() {
				// if the test doesn't set the startedAt time, assume the test is testing behavior that happens after the controller has been running for a while
				ctrl.startedAt = time.Now().Add(-(FlagHPADownscaleStabilization.Value() + time.Second))
			}

			err = ctrl.scaleNamespace(ctx, fn.Namespace)
			assert.NilError(t, err)
			tc.check(t, ctrl, fakeKubernetes, fakeKubernetesMetrics, fn)
		})
	}
}

func TestAssignPod(t *testing.T) {
	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *fake.Clientset, function.Function)
		check func(*testing.T, *fake.Clientset, function.Function, *function.Instance)
	}{
		{
			// Basic assignment: an available pod exists and should be assigned to the function
			name: "assigns available pod to function",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			// No pods available: should timeout waiting for a pod to become available
			name: "times out when no pods available",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// intentionally empty - no pods available
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				assert.Assert(t, instance == nil)
			},
		},
		{
			// Delayed availability: pod becomes available after a short delay, should still succeed
			name: "waits for pod to become available",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				go func() {
					time.Sleep(100 * time.Millisecond)
					fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				}()
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			// Assignment timeout: pod's assign endpoint is too slow to respond
			name: "times out when pod assign endpoint is slow",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fixture.SetFlag(t, &function.FlagAssignTimeout, time.Millisecond)

				// create a pod with a slow assign handler
				slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(10 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				})
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, slowHandler))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				assert.Assert(t, instance == nil)
			},
		},
		{
			// Multiple ports: pod has multiple container ports, should use the one specified by port annotation
			name: "uses port annotation when pod has multiple ports",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				pod := fixture.NewAvailablePod(t, fn, nil)

				// verify default port setup
				assert.Assert(t, pod.Annotations[key.Port.Label] == "http")
				assert.Assert(t, pod.Spec.Containers[0].Ports[0].Name == "http")

				// add another port and make the http port the second port on the container
				pod.Spec.Containers[0].Ports = []v1.ContainerPort{
					{ContainerPort: 8080, Name: "other"},
					pod.Spec.Containers[0].Ports[0],
				}
				fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			// Missing port annotation: should fall back to first container port
			name: "uses first container port when port annotation is empty",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				pod := fixture.NewAvailablePod(t, fn, nil)
				pod.Annotations[key.Port.Label] = ""
				fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			// Already assigned: pod already has a tenant label, should not be re-assigned
			// The patch should fail because the pod is already assigned, retries until timeout
			name: "rejects pod already assigned to another tenant",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// make getAvailablePods return assigned pods (normally filtered out)
				originalDoesNotHaveTenantSelector := doesNotHaveTenantSelector
				t.Cleanup(func() {
					doesNotHaveTenantSelector = originalDoesNotHaveTenantSelector
				})
				doesNotHaveTenantSelector = hasTenantSelector

				// add a pod that already has a tenant label
				pod := fixture.NewAvailablePod(t, fn, nil)
				pod.Labels[key.Tenant.Label] = "other"
				fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
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
			t.Cleanup(cancel)

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			fn := fixture.NewFunction()
			tc.setup(t, fakeKubernetes, fn)

			ctrl := New(nil, fakeKubernetes, nil)

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			instance, err := ctrl.assignPod(ctx, fn)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, fakeKubernetes, fn, instance)
		})
	}
}

func TestScale(t *testing.T) {
	testCases := []struct {
		name             string
		desiredInstances int
		err              error
		setup            func(*testing.T, *fake.Clientset, function.Function)
		check            func(*testing.T, *fake.Clientset, []*function.Instance)
	}{
		// ==================== Basic scaling operations ====================
		{
			// Basic scale up: assign an available pod to reach desired instance count
			name:             "scales up by assigning available pod",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)
			},
		},
		{
			// Scale up with surplus pods: only assigns needed pods, leaves extras unassigned
			name:             "only assigns needed pods when extras available",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				for range 5 {
					fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)

				// verify 4 pods remain unassigned
				instance := instances[0]
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{
					LabelSelector: doesNotHaveTenantSelector.String(),
				})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 4)
			},
		},
		{
			// No pods available: should timeout waiting for pods
			name:             "times out when no pods available",
			desiredInstances: 1,
			err:              context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// intentionally empty - no pods available
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 0)
			},
		},
		{
			// Metadata mismatch: assigned pod has different metadata, can't be reused
			name:             "ignores pods with different metadata",
			desiredInstances: 1,
			err:              context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fn.Metadata = "different"
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 0)
			},
		},
		{
			// Already at desired count: no scaling needed, returns existing instances
			name:             "returns existing instances when already at desired count",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)
			},
		},

		// ==================== Scale down operations ====================
		{
			// Scale down: keeps most recently assigned instance (likely has warmest cache)
			name:             "keeps most recently assigned instance when scaling down",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add max - 1 instances with older assignment times
				for range fn.Scale.MaxInstances - 1 {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add one instance with the most recent assignment time
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Name = "most-recent-assigned-at"
				pod.Annotations[key.AssignedAt.Label] = time.Now().Add(time.Second).UTC().Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)
				assert.Assert(t, instances[0].Name == "most-recent-assigned-at")
			},
		},

		// ==================== Ready/unready instance interactions ====================
		{
			// Scale to max with one unready: assigns new pod to reach max ready instances
			// Unready instances are preserved during scale up (they might become ready soon)
			name:             "scales to max ready instances while preserving unready",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				assert.Assert(t, fn.Scale.MaxInstances == 5)

				// add max - 1 ready instances
				for range fn.Scale.MaxInstances - 1 {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add 1 unready instance
				unreadyPod := fixture.NewAssignedPod(t, fn, nil)
				unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				fakeKubernetes.Tracker().Add(unreadyPod)

				// add 1 available pod for scaling
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				fn := instances[0].Function
				assert.Assert(t, len(instances) == fn.Scale.MaxInstances)

				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == fn.Scale.MaxInstances+1)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == fn.Scale.MaxInstances)
				assert.Assert(t, unreadyCount == 1) // unready instance preserved
			},
		},
		{
			// All unready, over max: can't scale up when too many total instances exist
			name:             "blocks scale up when total instances exceed max",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				assert.Assert(t, fn.Scale.MaxInstances == 5)

				// add max + 1 unready instances (exceeds total instance limit)
				for range fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}

				// available pod exists but shouldn't be used
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// no ready instances returned because total count exceeds max
				assert.Assert(t, len(instances) == 0)
			},
		},
		{
			// Some ready, many unready: returns ready instances but can't scale up further
			name:             "returns existing ready instances when blocked by total count",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				assert.Assert(t, fn.Scale.MaxInstances == 5)

				// add 2 ready instances
				for range 2 {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add max + 1 unready instances
				for range fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// only 2 ready instances returned (can't scale up due to total count)
				assert.Assert(t, len(instances) == 2)

				fn := instances[0].Function
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == fn.Scale.MaxInstances+3)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == 2)
				assert.Assert(t, unreadyCount == fn.Scale.MaxInstances+1) // unready preserved during scale up
			},
		},
		{
			// Scale to 0 cleans up all unready instances
			name:             "deletes all unready instances when scaling to zero",
			desiredInstances: 0,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add max + 1 unready instances
				for range fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 0)

				// all unready instances should be deleted
				pods, err := fakeKubernetes.CoreV1().Pods(fixture.FunctionNamespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
			},
		},
		{
			// Scale down deletes all unready instances when maintaining one ready
			name:             "deletes all unready instances when scaling down to one",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add 1 ready instance
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))

				// add max unready instances
				for range fn.Scale.MaxInstances {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)

				// all unready instances should be deleted
				pods, err := fakeKubernetes.CoreV1().Pods(fixture.FunctionNamespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
			},
		},
		{
			// Can't scale up when total instances at limit (preserves unready during scale up)
			name:             "preserves unready instances during blocked scale up",
			desiredInstances: 2,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add 1 ready instance
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))

				// add max unready instances (1 ready + max unready = max+1 total)
				for range fn.Scale.MaxInstances {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// only 1 ready instance returned (can't scale up, already over max total)
				assert.Assert(t, len(instances) == 1)

				fn := instances[0].Function
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == fn.Scale.MaxInstances+1)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == 1)
				assert.Assert(t, unreadyCount == fn.Scale.MaxInstances) // unready preserved
			},
		},
		{
			// Scale down from max ready + 1 unready: deletes excess ready and unready
			name:             "deletes excess ready and unready instances when scaling down",
			desiredInstances: 2,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add max ready instances
				for range fn.Scale.MaxInstances {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add 1 unready instance
				unreadyPod := fixture.NewAssignedPod(t, fn, nil)
				unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				fakeKubernetes.Tracker().Add(unreadyPod)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 2)

				fn := instances[0].Function
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == 2)
				assert.Assert(t, unreadyCount == 0) // unready always deleted during scale down
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			fn := fixture.NewFunction()

			tc.setup(t, fakeKubernetes, fn)

			ctrl := New(nil, fakeKubernetes, nil)

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			instances, err := ctrl.scale(ctx, fn, ScalingDecision{
				DesiredInstances: tc.desiredInstances,
				Reason:           "test",
			})
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, fakeKubernetes, instances)
		})
	}
}

// TestScaleForwarding verifies that scale requests are forwarded to the responsible
// controller when the current controller is not responsible for the function.
// This happens in multi-controller deployments where functions are sharded across controllers.
func TestScaleForwarding(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	// Create a controller pod with a different IP so this controller won't be responsible
	ctrlPod := fixture.NewControllerPod()
	ctrlPod.Status.PodIP = "127.0.0.2"
	fakeKubernetes := fake.NewClientset(ctrlPod)

	fn := fixture.NewFunction()

	// Set up mock client to handle forwarded scale request
	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleScale(func(ctx context.Context, fn function.Function, desiredInstances int, reason string) ([]*function.Instance, error) {
		return []*function.Instance{fixture.NewInstance(t, fn, nil)}, nil
	})

	ctrl := New(func(host string, port int) Client { return mcc }, fakeKubernetes, nil)

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Scale should succeed via forwarding to mock client
	instances, err := ctrl.scale(ctx, fn, ScalingDecision{
		DesiredInstances: 1,
		Reason:           "test",
	})
	assert.NilError(t, err)
	assert.Assert(t, len(instances) == 1)
}

// TestCalculateDesiredInstancesForMetric tests the HPA-style algorithm that determines
// the desired number of instances based on resource utilization metrics.
// The algorithm follows Kubernetes HPA behavior: desiredInstances = ceil(currentInstances * (currentUsage / targetUsage))
func TestCalculateDesiredInstancesForMetric(t *testing.T) {
	// Instances must be ready past the initial readiness delay to be included in scaling decisions
	readyAt := time.Now().Add(-FlagHPAInitialReadinessDelay.Value())

	testCases := []struct {
		name              string
		metricName        Metric
		podMetrics        []*function.Instance
		targetUsage       int
		expectedInstances int
	}{
		// ==================== Basic scaling decisions ====================
		{
			// Usage at 2x target triggers scale up to 2 instances
			// Formula: ceil(1 * (200/100)) = 2
			name:        "scales up when usage exceeds target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 200},
			},
			expectedInstances: 2,
		},
		{
			// Usage at 0.5x target triggers scale down to 1 instance
			// Formula: ceil(2 * (50/100)) = 1
			name:        "scales down when usage below target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 50},
				{ReadyAt: readyAt, CPUUsageMilli: 50},
			},
			expectedInstances: 1,
		},
		{
			// Usage exactly at target means no scaling needed
			name:        "no scaling when usage equals target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 100},
			},
			expectedInstances: 1,
		},

		// ==================== Tolerance and edge cases ====================
		{
			// Usage slightly above target (within tolerance) doesn't trigger scale up
			// This prevents thrashing from small fluctuations
			name:        "no scaling when usage within tolerance",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 110},
			},
			expectedInstances: 1,
		},
		{
			// When one instance is missing metrics, the algorithm assumes target usage for that instance
			// This prevents incorrect scale-up decisions when metrics are temporarily unavailable
			// Instance 1: 150m (would scale up), Instance 2: 0m (missing metric)
			// Adjusted average: 75m (would scale down) -> no scaling
			name:        "missing metric assumes target usage to prevent incorrect scale up",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 150},
				{ReadyAt: readyAt, CPUUsageMilli: 0}, // missing metric
			},
			expectedInstances: 2,
		},
		{
			// Recently started instances (within initial readiness delay) are excluded from scaling decisions
			// This gives new instances time to stabilize before affecting scaling
			name:        "excludes recently started instances from scaling decision",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 150},    // included in calculation
				{ReadyAt: time.Now(), CPUUsageMilli: 150}, // excluded (too new)
			},
			expectedInstances: 2,
		},
		{
			// When no metrics are available for any instance, maintain current count
			// This prevents scaling decisions based on incomplete data
			name:        "maintains current count when no metrics available",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 0},
				{ReadyAt: readyAt, CPUUsageMilli: 0},
			},
			expectedInstances: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Configure target usage for each instance
			for _, pm := range tc.podMetrics {
				switch tc.metricName {
				case MetricCPU:
					pm.Scale.TargetCPUUsageMilli = tc.targetUsage
				case MetricMemory:
					pm.Scale.TargetMemoryUsageMiB = tc.targetUsage
				}
			}

			instances, _ := calculateDesiredInstancesForMetric(t.Context(), tc.metricName, tc.podMetrics)
			assert.Assert(t, instances == tc.expectedInstances)
		})
	}
}
