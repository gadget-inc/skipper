package controller

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/goccy/go-json"
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
	function.FlagPort.Init()

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
	must.Eq(t, instance.Function.Deployment, pod.Labels[key.Deployment.Label])
	must.Eq(t, instance.Function.Tenant, pod.Labels[key.Tenant.Label])

	fnJSON, err := json.Marshal(instance.Function)
	must.NoError(t, err)
	must.Eq(t, string(fnJSON), pod.Annotations[key.Function.Label])

	must.Eq(t, instance.Name, pod.Name)
	must.Eq(t, instance.Addr, pod.Status.PodIP+":"+strconv.Itoa(int(pod.Spec.Containers[0].Ports[0].ContainerPort)))
	must.Eq(t, instance.ReplicaSet, pod.Annotations[key.ReplicaSet.Label])
	must.Eq(t, instance.AssignedAt.Format(time.RFC3339), pod.Annotations[key.AssignedAt.Label])
	must.Eq(t, instance.ReadyAt.Format(time.RFC3339), pod.Annotations[key.ReadyAt.Label])
}

func ensurePodIsNotAssignedToFunction(t *testing.T, pod v1.Pod) {
	must.MapNotContainsKey(t, pod.Labels, key.Tenant.Label)
	must.MapNotContainsKey(t, pod.Annotations, key.Function.Label)
	must.MapNotContainsKey(t, pod.Annotations, key.ReplicaSet.Label)
	must.MapNotContainsKey(t, pod.Annotations, key.AssignedAt.Label)
	must.MapNotContainsKey(t, pod.Annotations, key.ReadyAt.Label)
}

func TestScaleFunctions(t *testing.T) {
	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *Controller, *fake.Clientset, *fakekubernetesmetrics.Clientset) function.Function
		check func(*testing.T, *Controller, *fake.Clientset, *fakekubernetesmetrics.Clientset, function.Function)
	}{
		{
			name: "scale up",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.heartbeats.Store(fn, time.Now())

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

				instance1, err := function.FromPod(&pods.Items[0])
				must.NoError(t, err)

				instance2, err := function.FromPod(&pods.Items[1])
				must.NoError(t, err)

				must.Eq(t, fn, instance1.Function)
				must.Eq(t, fn, instance2.Function)
				must.Len(t, 2, pods.Items)
			},
		},
		{
			name: "scale down",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.heartbeats.Store(fn, time.Now())

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

				instance1, err := function.FromPod(&pods.Items[0])
				must.NoError(t, err)

				must.Eq(t, fn, instance1.Function)
				must.Len(t, 1, pods.Items)
			},
		},
		{
			name: "no scale",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				fn.Scale.TargetMemoryUsageMiB = 0 // don't scale on memory
				c.heartbeats.Store(fn, time.Now())

				fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(assignedPod)
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli) + "m"        // 1x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB*2) + "Mi" // 2x target
				fakeKubernetesMetrics.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset, fn function.Function) {
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				must.NoError(t, err)

				instance1, err := function.FromPod(&pods.Items[0])
				must.NoError(t, err)
				must.Eq(t, fn, instance1.Function)

				ensurePodIsNotAssignedToFunction(t, pods.Items[1])
				must.Len(t, 2, pods.Items)
			},
		},
		{
			name: "heartbeat timeout",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.heartbeats.Store(fn, time.Now().Add(-FlagHeartbeatTimeout.Value()))
				c.scaleMu.Store(fn, new(sync.Mutex))
				c.stabilizationWindows.Store(fn, new(StabilizationWindow))

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

				_, ok := c.heartbeats.Load(fn)
				must.False(t, ok)

				_, ok = c.scaleMu.Load(fn)
				must.False(t, ok)

				_, ok = c.stabilizationWindows.Load(fn)
				must.False(t, ok)
			},
		},
		{
			name: "stale instance",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.heartbeats.Store(fn, time.Now())

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
				c.heartbeats.Store(fn, time.Now())

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

				instance, err := function.FromPod(&pods.Items[0])
				must.NoError(t, err)
				must.Eq(t, fn, instance.Function)
				must.Len(t, 1, pods.Items)
			},
		},
		{
			name: "different controller pod",
			setup: func(t *testing.T, c *Controller, fakeKubernetes *fake.Clientset, fakeKubernetesMetrics *fakekubernetesmetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.heartbeats.Store(fn, time.Now().Add(-FlagHeartbeatTimeout.Value())) // heartbeat timeout

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				fakeKubernetes.Tracker().Add(assignedPod) // add an assigned pod that needs to be terminated

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

				// the assigned pod should still be around because we're not responsible for it
				instance, err := function.FromPod(&pods.Items[0])
				must.NoError(t, err)
				must.Eq(t, fn, instance.Function)
				must.Len(t, 1, pods.Items)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			metricsClientset := fakekubernetesmetrics.NewSimpleClientset()
			ctrl := New(nil, fakeKubernetes, metricsClientset)

			fn := tc.setup(t, ctrl, fakeKubernetes, metricsClientset)

			err := ctrl.startInformers(ctx)
			must.NoError(t, err)

			err = ctrl.scaleFunctions(ctx, fn.Namespace)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, ctrl, fakeKubernetes, metricsClientset, fn)
		})
	}
}

func TestAssignPodToFunction(t *testing.T) {
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

			instance, err := ctrl.assignPodToFunction(ctx, fn)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, fakeKubernetes, fn, instance)
		})
	}
}

func TestScaleFunction(t *testing.T) {
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
					LabelSelector: doesNotHaveTenantRequirement.String(),
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
				for range 5 {
					err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
					must.NoError(t, err)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
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

			instances, err := ctrl.scaleFunction(ctx, fn, tc.desiredInstances)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, fakeKubernetes, instances)
		})
	}
}

func TestScaleFunctionForwarding(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	ctrlPod := fixture.NewControllerPod()
	ctrlPod.Status.PodIP = "127.0.0.2" // different IP so we can test forwarding
	fakeKubernetes := fake.NewClientset(ctrlPod)

	fn := fixture.NewFunction()

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleScale(func(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
		return []*function.Instance{fixture.NewInstance(t, fn, nil)}, nil
	})

	ctrl := New(func(host string, port int) Client { return mcc }, fakeKubernetes, nil)

	err := ctrl.startInformers(ctx)
	must.NoError(t, err)

	instances, err := ctrl.scaleFunction(ctx, fn, 1)
	must.NoError(t, err)
	must.Len(t, 1, instances)
}

func ptrInt64(val int64) *int64 {
	return &val
}

func TestCalculateDesiredInstancesForMetric(t *testing.T) {
	readyAt := time.Now().Add(-FlagHPAInitialReadinessDelay.Value())

	testCases := []struct {
		name              string
		metricName        Metric
		podMetrics        []*function.Instance
		targetUsage       int
		expectedInstances int
		expectError       bool
	}{
		{
			name:        "scale up",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsage: ptrInt64(200)},
			},
			expectedInstances: 2,
			expectError:       false,
		},
		{
			name:        "scale down",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsage: ptrInt64(50)},
				{ReadyAt: readyAt, CPUUsage: ptrInt64(50)},
			},
			expectedInstances: 1,
			expectError:       false,
		},
		{
			name:        "no scaling",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsage: ptrInt64(100)},
			},
			expectedInstances: 1,
			expectError:       false,
		},
		{
			name:        "no scaling (within tolerance)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsage: ptrInt64(110)},
			},
			expectedInstances: 1,
			expectError:       false,
		},
		{
			name:        "no scaling (missing metric reverses decision)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsage: ptrInt64(150)}, // causes scale up   (averageUsage = 150, usageRatio = 1.5)
				{ReadyAt: readyAt, CPUUsage: nil},           // causes scale down (adjustedAverageUsage = 75, adjustedUsageRatio = 0.75), therefor no scaling
			},
			expectedInstances: 2,
			expectError:       false,
		},
		{
			name:        "no scaling (within initial readiness delay)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsage: ptrInt64(150)},    // causes scale up   (averageUsage = 150, usageRatio = 1.5)
				{ReadyAt: time.Now(), CPUUsage: ptrInt64(150)}, // causes scale down (adjustedAverageUsage = 75, adjustedUsageRatio = 0.75), therefor no scaling
			},
			expectedInstances: 2,
			expectError:       false,
		},
		{
			name:        "no metrics available",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsage: nil},
				{ReadyAt: readyAt, CPUUsage: nil},
			},
			expectedInstances: 2,
			expectError:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, pm := range tc.podMetrics {
				switch tc.metricName {
				case MetricCPU:
					pm.Function.Scale.TargetCPUUsageMilli = tc.targetUsage
				case MetricMemory:
					pm.Function.Scale.TargetMemoryUsageMiB = tc.targetUsage
				}
			}

			instances, err := calculateDesiredInstancesForMetric(tc.metricName, tc.podMetrics, time.Now())
			if tc.expectError {
				must.Error(t, err)
				return
			}

			must.NoError(t, err)
			must.Eq(t, tc.expectedInstances, instances)
		})
	}
}
