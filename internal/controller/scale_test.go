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
	"github.com/shoenig/test/must"
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
	must.Eq(t, instance.Deployment, pod.Labels[key.Deployment.Label])
	must.Eq(t, instance.Tenant, pod.Labels[key.Tenant.Label])

	fnJSON, err := json.Marshal(instance.Function)
	must.NoError(t, err)
	must.Eq(t, string(fnJSON), pod.Annotations[key.Function.Label])

	port, err := portFromPod(&pod)
	must.NoError(t, err)

	must.Eq(t, instance.Name, pod.Name)
	must.Eq(t, instance.Addr, net.JoinHostPort(pod.Status.PodIP, port))
	must.Eq(t, instance.ReplicaSet, pod.Annotations[key.ReplicaSet.Label])
	must.Eq(t, instance.AssignedAt.Format(time.RFC3339), pod.Annotations[key.AssignedAt.Label])
	must.Eq(t, instance.ReadyAt.Format(time.RFC3339), pod.Annotations[key.ReadyAt.Label])
}

func ensurePodIsAssignedToFunction(t *testing.T, pod v1.Pod, fn function.Function) {
	fnJSON, err := json.Marshal(fn)
	must.NoError(t, err)

	must.Eq(t, fn.Deployment, pod.Labels[key.Deployment.Label])
	must.Eq(t, fn.Tenant, pod.Labels[key.Tenant.Label])
	must.Eq(t, string(fnJSON), pod.Annotations[key.Function.Label])
	must.MapContainsKey(t, pod.Annotations, key.ReplicaSet.Label)
	must.MapContainsKey(t, pod.Annotations, key.AssignedAt.Label)
	must.MapContainsKey(t, pod.Annotations, key.ReadyAt.Label)
}

func ensurePodIsNotAssignedToFunction(t *testing.T, pod v1.Pod) {
	must.MapNotContainsKey(t, pod.Labels, key.Tenant.Label)
	must.MapNotContainsKey(t, pod.Annotations, key.Function.Label)
	must.MapNotContainsKey(t, pod.Annotations, key.ReplicaSet.Label)
	must.MapNotContainsKey(t, pod.Annotations, key.AssignedAt.Label)
	must.MapNotContainsKey(t, pod.Annotations, key.ReadyAt.Label)
}

func TestScaleNamespace(t *testing.T) {
	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *Controller, *fake.Clientset, *fakekubernetesmetrics.Clientset) function.Function
		check func(*testing.T, *Controller, *fake.Clientset, *fakekubernetesmetrics.Clientset, function.Function)
	}{
		{
			name: "scale up cpu",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now()},
				}})

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli*2) + "m"    // 2x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB) + "Mi" // 1x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 2, pods.Items)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], fn)
			},
		},
		{
			name: "scale up memory",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now()},
				}})

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli) + "m"        // 1x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB*2) + "Mi" // 2x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 2, pods.Items)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], fn)
			},
		},
		{
			name: "scale up in-flight requests",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP:  {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests}, // 2x target across 2 routers
					fixture.RouterIP2: {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests},
				}})

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli) + "m"      // 1x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB) + "Mi" // 1x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 2, pods.Items)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
				ensurePodIsAssignedToFunction(t, pods.Items[1], fn)
			},
		},
		{
			name: "scale down",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now()},
				}})

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				assignedPod1 := fixture.NewAssignedPod(t, fn, nil)
				assignedPod1.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod1.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod1)

				assignedPod2 := fixture.NewAssignedPod(t, fn, nil)
				assignedPod2.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod2.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod2)

				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli/2) + "m"      // 0.5x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB/2) + "Mi" // 0.5x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod1, cpuUsage, memoryUsage))
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod2, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
			},
		},
		{
			name: "no scale",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				fn.Scale.TargetMemoryUsageMiB = 0 // don't scale on memory
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP:  {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests / 2}, // 1x target across 2 routers
					fixture.RouterIP2: {Function: fn, Timestamp: time.Now(), InFlightRequests: fn.Scale.TargetInFlightRequests / 2},
				}})

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli) + "m"        // 1x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB*2) + "Mi" // 2x target, but ignored because memory is 0
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 2, pods.Items)

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
			name: "no heartbeat",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 0, pods.Items)

				_, ok := c.supervisors.Load(fn)
				must.False(t, ok)
			},
		},
		{
			name: "heartbeat timeout",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now().Add(-FlagHeartbeatTimeout.Value())},
				}})

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 0, pods.Items)

				_, ok := c.supervisors.Load(fn)
				must.False(t, ok)
			},
		},
		{
			name: "heartbeat timeout but just started",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				c.startedAt = time.Now()

				fn := fixture.NewFunction()
				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)

				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
			},
		},
		{
			name: "stale instance",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now()},
				}})

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
				must.NoError(t, err)
				must.Len(t, 0, pods.Items)
			},
		},
		{
			name: "stale instance without enough available pods",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now()},
				}})

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
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn)
			},
		},
		{
			name: "different controller pod",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now().Add(-FlagHeartbeatTimeout.Value())}, // heartbeat timeout
				}})

				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil)) // add an assigned pod that needs to be terminated

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
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensurePodIsAssignedToFunction(t, pods.Items[0], fn) // the assigned pod should still be around because we're not responsible for it
			},
		},
		{
			name: "extra ready instance",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now()},
				}})

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
				must.NoError(t, err)
				must.Len(t, fn.Scale.MaxInstances, pods.Items)
				for _, pod := range pods.Items {
					ensurePodIsAssignedToFunction(t, pod, fn)
				}
			},
		},
		{
			name: "extra unready instance",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.supervisors.Store(fn, &Supervisor{fn: fn, ctrl: c, routerHeartbeats: map[string]function.Heartbeat{
					fixture.RouterIP: {Function: fn, Timestamp: time.Now()},
				}})

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
				must.NoError(t, err)
				must.Len(t, fn.Scale.MaxInstances+1, pods.Items)
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
			must.NoError(t, err)

			if ctrl.startedAt.IsZero() {
				// if the test doesn't set the startedAt time, assume the test is testing behavior that happens after the controller has been running for a while
				ctrl.startedAt = time.Now().Add(-(FlagHPADownscaleStabilization.Value() + time.Second))
			}

			err = ctrl.scaleNamespace(ctx, fn.Namespace)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

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
			name: "smoke",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				err := fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			name: "no available pods",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// no pods
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				must.Nil(t, instance)
			},
		},
		{
			name: "eventually available pod",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				go func() {
					time.Sleep(100 * time.Millisecond)
					err := fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
					must.NoError(t, err)
				}()
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			name: "assign timeout",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fixture.SetFlag(t, &function.FlagAssignTimeout, time.Millisecond)
				err := fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(10 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				})))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				must.Nil(t, instance)
			},
		},
		{
			name: "multiple ports",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// ensure the pod has a port annotation
				pod := fixture.NewAvailablePod(t, fn, nil)
				must.Eq(t, "http", pod.Annotations[key.Port.Label])
				must.Eq(t, "http", pod.Spec.Containers[0].Ports[0].Name)

				// add another port and make the http port the second port on the container
				pod.Spec.Containers[0].Ports = []v1.ContainerPort{
					{ContainerPort: 8080, Name: "other"},
					pod.Spec.Containers[0].Ports[0],
				}

				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			name: "no port annotation",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				pod := fixture.NewAvailablePod(t, fn, nil)
				pod.Annotations[key.Port.Label] = ""
				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			name: "has tenant",
			err:  context.DeadlineExceeded, // the patch should fail because the pod is already assigned, but we should retry until the test times out
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// make getAvailablePods return assigned pods
				originalDoesNotHaveTenantSelector := doesNotHaveTenantSelector
				t.Cleanup(func() {
					doesNotHaveTenantSelector = originalDoesNotHaveTenantSelector
				})
				doesNotHaveTenantSelector = hasTenantSelector

				// add a pod that has a tenant label
				pod := fixture.NewAvailablePod(t, fn, nil)
				pod.Labels[key.Tenant.Label] = "other"
				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)

				pod := pods.Items[0]
				must.Eq(t, "other", pod.Labels[key.Tenant.Label])
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
			must.NoError(t, err)

			instance, err := ctrl.assignPod(ctx, fn)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
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
		{
			name:             "smoke",
			desiredInstances: 1,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				err := fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
			},
		},
		{
			name:             "extra available pods",
			desiredInstances: 1,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				for range 5 {
					err := fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
					must.NoError(t, err)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
				instance := instances[0]
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{
					LabelSelector: doesNotHaveTenantSelector.String(),
				})
				must.NoError(t, err)
				must.Len(t, 4, pods.Items)
			},
		},
		{
			name:             "no available pods",
			desiredInstances: 1,
			err:              context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// no pods
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)
			},
		},
		{
			name:             "different metadata",
			desiredInstances: 1,
			err:              context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fn.Metadata = "different"
				err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)
			},
		},
		{
			name:             "already has desired instances",
			desiredInstances: 1,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
			},
		},
		{
			name:             "scale down",
			desiredInstances: 1,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				for range fn.Scale.MaxInstances - 1 {
					err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
					must.NoError(t, err)
				}

				// make the last instance known and have the most recent assigned at
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Name = "most-recent-assigned-at"
				pod.Annotations[key.AssignedAt.Label] = time.Now().Add(time.Second).UTC().Format(time.RFC3339)
				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)

				// ensure the most recent assigned at instance was kept
				must.Eq(t, "most-recent-assigned-at", instances[0].Name)
			},
		},
		{
			name:             "scale to max with ready instances = max-1, unready instances = 1",
			desiredInstances: 5,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// ensure desired instances is equal to max instances
				must.Eq(t, 5, fn.Scale.MaxInstances)

				// add max - 1 ready instances
				for range fn.Scale.MaxInstances - 1 {
					err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
					must.NoError(t, err)
				}

				// add 1 unready instance
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)

				// add 1 unassigned pod
				err = fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				fn := instances[0].Function

				// ensure max instances were returned
				must.Len(t, fn.Scale.MaxInstances, instances)

				// ensure there are max + 1 pods
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, fn.Scale.MaxInstances+1, pods.Items)

				readyInstances := 0
				unreadyInstances := 0
				for _, pod := range pods.Items {
					if isPodReady(&pod) {
						readyInstances++
						continue
					}
					if isPodRunning(&pod) {
						unreadyInstances++
					}
				}

				// ensure there are still max ready instances
				must.Eq(t, fn.Scale.MaxInstances, readyInstances)

				// ensure there is still 1 unready instance because we only delete unready instances during scale down
				must.Eq(t, 1, unreadyInstances)
			},
		},
		{
			name:             "scale to max with ready instances = 0, unready instances = max+1",
			desiredInstances: 5,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// ensure desired instances is equal to max instances
				must.Eq(t, 5, fn.Scale.MaxInstances)

				// add max + 1 unready instances
				for range fn.Scale.MaxInstances + 1 {
					pod := fixture.NewAssignedPod(t, fn, nil)
					pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					err := fakeKubernetes.Tracker().Add(pod)
					must.NoError(t, err)
				}

				// add 1 unassigned pod
				err := fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// ensure no instances were returned because this function already has too many instances in total
				must.Len(t, 0, instances)
			},
		},
		{
			name:             "scale to max with ready instances = 2, unready instances = max+1",
			desiredInstances: 5,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// ensure desired instances is equal to max instances
				must.Eq(t, 5, fn.Scale.MaxInstances)

				// add 2 ready instances
				for range 2 {
					err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
					must.NoError(t, err)
				}

				// add max + 1 unready instances
				for range fn.Scale.MaxInstances + 1 {
					pod := fixture.NewAssignedPod(t, fn, nil)
					pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					err := fakeKubernetes.Tracker().Add(pod)
					must.NoError(t, err)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// ensure 2 instances were returned because we already had 2 ready instances but couldn't scale up because we have too many instances in total
				must.Len(t, 2, instances)

				fn := instances[0].Function

				// ensure there are max + 3 pods
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, fn.Scale.MaxInstances+3, pods.Items)

				readyInstances := 0
				unreadyInstances := 0
				for _, pod := range pods.Items {
					if isPodReady(&pod) {
						readyInstances++
						continue
					}
					if isPodRunning(&pod) {
						unreadyInstances++
					}
				}

				// ensure there are 2 ready instances (matches what was returned)
				must.Eq(t, 2, readyInstances)

				// ensure there are still max+1 unready instances because we only delete unready instances during scale down
				must.Eq(t, fn.Scale.MaxInstances+1, unreadyInstances)
			},
		},
		{
			name:             "scale to 0 with ready instances = 0, unready instances = max+1",
			desiredInstances: 0,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add max + 1 unready instances
				for range fn.Scale.MaxInstances + 1 {
					pod := fixture.NewAssignedPod(t, fn, nil)
					pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					err := fakeKubernetes.Tracker().Add(pod)
					must.NoError(t, err)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)

				// ensure all unready instances were deleted
				pods, err := fakeKubernetes.CoreV1().Pods(fixture.FunctionNamespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 0, pods.Items)
			},
		},
		{
			name:             "scale to 1 with ready instances = 1, unready instances = max",
			desiredInstances: 1,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add 1 ready instance
				err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)

				// add max unready instances
				for range fn.Scale.MaxInstances {
					pod := fixture.NewAssignedPod(t, fn, nil)
					pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					err := fakeKubernetes.Tracker().Add(pod)
					must.NoError(t, err)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// ensure 1 instance was returned
				must.Len(t, 1, instances)

				// ensure all unready instances were deleted
				pods, err := fakeKubernetes.CoreV1().Pods(fixture.FunctionNamespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
			},
		},
		{
			name:             "scale to 2 with ready instances = 1, unready instances = max",
			desiredInstances: 2,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add 1 ready instance
				err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)

				// add max unready instances
				for range fn.Scale.MaxInstances {
					pod := fixture.NewAssignedPod(t, fn, nil)
					pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					err := fakeKubernetes.Tracker().Add(pod)
					must.NoError(t, err)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// ensure only 1 instance was returned because we already have max+1 instances in total
				must.Len(t, 1, instances)

				fn := instances[0].Function

				// ensure there are max + 1 pods
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, fn.Scale.MaxInstances+1, pods.Items)

				readyInstances := 0
				unreadyInstances := 0
				for _, pod := range pods.Items {
					if isPodReady(&pod) {
						readyInstances++
						continue
					}
					if isPodRunning(&pod) {
						unreadyInstances++
					}
				}

				// ensure there is 1 ready instance (matches what was returned)
				must.Eq(t, 1, readyInstances)

				// ensure there are still max unready instances because we only delete unready instances during scale down
				must.Eq(t, fn.Scale.MaxInstances, unreadyInstances)
			},
		},
		{
			name:             "scale to 2 with ready instances = max, unready instances = 1",
			desiredInstances: 2,
			err:              nil,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add max ready instances
				for range fn.Scale.MaxInstances {
					err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
					must.NoError(t, err)
				}

				// add 1 unready instance
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// ensure 2 instances were returned
				must.Len(t, 2, instances)

				fn := instances[0].Function

				// ensure there are 2 pods
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 2, pods.Items)

				readyInstances := 0
				unreadyInstances := 0
				for _, pod := range pods.Items {
					if isPodReady(&pod) {
						readyInstances++
						continue
					}
					if isPodRunning(&pod) {
						unreadyInstances++
					}
				}

				// ensure there are 2 ready instances (matches what was returned)
				must.Eq(t, 2, readyInstances)

				// ensure there are 0 unready instances because we always delete unready instances during scale down
				must.Eq(t, 0, unreadyInstances)
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
			must.NoError(t, err)

			instances, err := ctrl.supervisor(fn).scale(ctx, ScalingDecision{
				DesiredInstances: tc.desiredInstances,
				Reason:           "test",
			})
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, fakeKubernetes, instances)
		})
	}
}

func TestScaleForwarding(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	ctrlPod := fixture.NewControllerPod()
	ctrlPod.Status.PodIP = "127.0.0.2" // different IP so we can test forwarding
	fakeKubernetes := fake.NewClientset(ctrlPod)

	fn := fixture.NewFunction()

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleScale(func(ctx context.Context, fn function.Function, desiredInstances int, reason string) ([]*function.Instance, error) {
		return []*function.Instance{fixture.NewInstance(t, fn, nil)}, nil
	})

	ctrl := New(func(host string, port int) Client { return mcc }, fakeKubernetes, nil)

	err := ctrl.startInformers(ctx)
	must.NoError(t, err)

	instances, err := ctrl.supervisor(fn).scale(ctx, ScalingDecision{
		DesiredInstances: 1,
		Reason:           "test",
	})
	must.NoError(t, err)
	must.Len(t, 1, instances)
}

func TestCalculateDesiredInstancesForMetric(t *testing.T) {
	readyAt := time.Now().Add(-FlagHPAInitialReadinessDelay.Value())

	testCases := []struct {
		name              string
		metricName        Metric
		podMetrics        []*function.Instance
		targetUsage       int
		expectedInstances int
	}{
		{
			name:        "scale up",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 200},
			},
			expectedInstances: 2,
		},
		{
			name:        "scale down",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 50},
				{ReadyAt: readyAt, CPUUsageMilli: 50},
			},
			expectedInstances: 1,
		},
		{
			name:        "no scaling",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 100},
			},
			expectedInstances: 1,
		},
		{
			name:        "no scaling (within tolerance)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 110},
			},
			expectedInstances: 1,
		},
		{
			name:        "no scaling (missing metric reverses decision)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 150}, // causes scale up   (averageUsage = 150, usageRatio = 15)
				{ReadyAt: readyAt, CPUUsageMilli: 0},   // causes scale down (adjustedAverageUsage = 75, adjustedUsageRatio = 0.75), therefor no scaling
			},
			expectedInstances: 2,
		},
		{
			name:        "no scaling (within initial readiness delay)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 150},    // causes scale up   (averageUsage = 150, usageRatio = 15)
				{ReadyAt: time.Now(), CPUUsageMilli: 150}, // causes scale down (adjustedAverageUsage = 75, adjustedUsageRatio = 0.75), therefor no scalng
			},
			expectedInstances: 2,
		},
		{
			name:        "no metrics available",
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
			for _, pm := range tc.podMetrics {
				switch tc.metricName {
				case MetricCPU:
					pm.Scale.TargetCPUUsageMilli = tc.targetUsage
				case MetricMemory:
					pm.Scale.TargetMemoryUsageMiB = tc.targetUsage
				}
			}

			instances, _ := calculateDesiredInstancesForMetric(t.Context(), tc.metricName, tc.podMetrics)
			must.Eq(t, tc.expectedInstances, instances)
		})
	}
}
