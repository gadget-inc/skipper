package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/shoenig/test/must"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	fakemetrics "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func init() {
	function.FlagAssignPath.Init()
	function.FlagAssignTimeout.Init()
	function.FlagPort.Init()
	_ = function.FlagNamespaces.SetValue([]string{fixture.DefaultFunctionNamespace})

	FlagPort.Init()
	FlagHeartbeatTimeout.Init()
	_ = FlagIP.SetValue(fixture.DefaultControllerIP)
	_ = FlagNamespace.SetValue(fixture.DefaultControllerNamespace)
	_ = FlagPasetoPrivateKey.SetValue(fixture.DefaultControllerPasetoSecretKey)
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

func TestScaleFunctions(t *testing.T) {
	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *Controller, *fake.Clientset, *fakemetrics.Clientset) function.Function
		check func(*testing.T, *Controller, *fake.Clientset, *fakemetrics.Clientset, function.Function)
	}{
		{
			name: "scale up",
			setup: func(t *testing.T, c *Controller, clientset *fake.Clientset, metricsClientset *fakemetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.heartbeats.Store(fn, time.Now())

				clientset.Tracker().Add(fixture.NewReplicaSet(t, fn))
				clientset.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				clientset.Tracker().Add(assignedPod)

				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli*2) + "m"    // 2x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB) + "Mi" // 1x target
				metricsClientset.Tracker().Create(fixture.NewPodMetrics(t, assignedPod, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, clientset *fake.Clientset, metricsClientset *fakemetrics.Clientset, fn function.Function) {
				pods, err := clientset.CoreV1().Pods(fn.Namespace).List(context.Background(), metav1.ListOptions{})
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
			setup: func(t *testing.T, c *Controller, clientset *fake.Clientset, metricsClientset *fakemetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.heartbeats.Store(fn, time.Now())

				clientset.Tracker().Add(fixture.NewReplicaSet(t, fn))

				assignedPod1 := fixture.NewAssignedPod(t, fn, nil)
				assignedPod1.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod1.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				clientset.Tracker().Add(assignedPod1)

				assignedPod2 := fixture.NewAssignedPod(t, fn, nil)
				assignedPod2.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				assignedPod2.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHPAInitialReadinessDelay.Value()).Format(time.RFC3339)
				clientset.Tracker().Add(assignedPod2)

				cpuUsage := strconv.Itoa(fn.Scale.TargetCPUUsageMilli/2) + "m"    // 0.5x target
				memoryUsage := strconv.Itoa(fn.Scale.TargetMemoryUsageMiB) + "Mi" // 1x target
				metricsClientset.Tracker().Create(fixture.NewPodMetrics(t, assignedPod1, cpuUsage, memoryUsage))
				metricsClientset.Tracker().Create(fixture.NewPodMetrics(t, assignedPod2, cpuUsage, memoryUsage))
				return fn
			},
			check: func(t *testing.T, c *Controller, clientset *fake.Clientset, metricsClientset *fakemetrics.Clientset, fn function.Function) {
				pods, err := clientset.CoreV1().Pods(fn.Namespace).List(context.Background(), metav1.ListOptions{})
				must.NoError(t, err)

				instance1, err := function.FromPod(&pods.Items[0])
				must.NoError(t, err)

				must.Eq(t, fn, instance1.Function)
				must.Len(t, 1, pods.Items)
			},
		},
		{
			name: "heartbeat timeout",
			setup: func(t *testing.T, c *Controller, clientset *fake.Clientset, metricsClientset *fakemetrics.Clientset) function.Function {
				fn := fixture.NewFunction()
				c.heartbeats.Store(fn, time.Now().Add(-FlagHeartbeatTimeout.Value()))
				c.scaleMu.Store(fn, new(sync.Mutex))
				c.stabilizationWindows.Store(fn, new(StabilizationWindow))

				clientset.Tracker().Add(fixture.NewReplicaSet(t, fn))

				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				assignedPod.Annotations[key.ReadyAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				assignedPod.Annotations[key.AssignedAt.Label] = time.Now().Add(-FlagHeartbeatTimeout.Value()).Format(time.RFC3339)
				clientset.Tracker().Add(assignedPod)

				return fn
			},
			check: func(t *testing.T, c *Controller, clientset *fake.Clientset, metricsClientset *fakemetrics.Clientset, fn function.Function) {
				pods, err := clientset.CoreV1().Pods(fn.Namespace).List(context.Background(), metav1.ListOptions{})
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			clientset := fake.NewClientset(fixture.NewControllerPod())
			metricsClientset := fakemetrics.NewSimpleClientset()
			c := New(nil, clientset, metricsClientset)

			fn := tc.setup(t, c, clientset, metricsClientset)

			err := c.startInformers(ctx)
			must.NoError(t, err)

			err = c.scaleFunctions(ctx, fn.Namespace)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, c, clientset, metricsClientset, fn)
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
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				err := clientset.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, clientset *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := clientset.CoreV1().Pods(instance.Namespace).List(context.Background(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			name: "no available pods",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				// no pods
			},
			check: func(t *testing.T, clientset *fake.Clientset, fn function.Function, instance *function.Instance) {
				must.Nil(t, instance)
			},
		},
		{
			name: "eventually available pod",
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				go func() {
					time.Sleep(100 * time.Millisecond)
					err := clientset.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
					must.NoError(t, err)
				}()
			},
			check: func(t *testing.T, clientset *fake.Clientset, fn function.Function, instance *function.Instance) {
				pods, err := clientset.CoreV1().Pods(instance.Namespace).List(context.Background(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensureInstanceIsAssignedToPod(t, instance, pods.Items[0])
			},
		},
		{
			name: "assign timeout",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				fixture.SetFlag(t, &function.FlagAssignTimeout, time.Millisecond)
				err := clientset.Tracker().Add(fixture.NewAvailablePod(t, fn, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(10 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				})))
				must.NoError(t, err)
			},
			check: func(t *testing.T, clientset *fake.Clientset, fn function.Function, instance *function.Instance) {
				must.Nil(t, instance)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)

			clientset := fake.NewClientset(fixture.NewControllerPod())
			fn := fixture.NewFunction()
			tc.setup(t, clientset, fn)

			c := New(nil, clientset, nil)

			err := c.startInformers(ctx)
			must.NoError(t, err)

			instance, err := c.assignPodToFunction(ctx, fn)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, clientset, fn, instance)
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
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				err := clientset.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, clientset *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
			},
		},
		{
			name:             "extra available pods",
			desiredInstances: 1,
			err:              nil,
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				for i := 0; i < 5; i++ {
					err := clientset.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
					must.NoError(t, err)
				}
			},
			check: func(t *testing.T, clientset *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
				instance := instances[0]
				pods, err := clientset.CoreV1().Pods(instance.Namespace).List(context.Background(), metav1.ListOptions{
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
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				// no pods
			},
			check: func(t *testing.T, clientset *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)
			},
		},
		{
			name:             "different metadata",
			desiredInstances: 1,
			err:              context.DeadlineExceeded,
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				fn.Metadata = "different"
				err := clientset.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, clientset *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)
			},
		},
		{
			name:             "already has desired instances",
			desiredInstances: 1,
			err:              nil,
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				err := clientset.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, clientset *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
			},
		},
		{
			name:             "scale down",
			desiredInstances: 1,
			err:              nil,
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				for i := 0; i < 5; i++ {
					err := clientset.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
					must.NoError(t, err)
				}
			},
			check: func(t *testing.T, clientset *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)

			clientset := fake.NewClientset(fixture.NewControllerPod())
			fn := fixture.NewFunction()

			tc.setup(t, clientset, fn)

			c := New(nil, clientset, nil)

			err := c.startInformers(ctx)
			must.NoError(t, err)

			instances, err := c.scaleFunction(ctx, fn, tc.desiredInstances)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, clientset, instances)
		})
	}
}

func TestScaleFunctionForwarding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	ctrlPod := fixture.NewControllerPod()
	ctrlPod.Status.PodIP = "127.0.0.2" // different IP so we can test forwarding
	clientset := fake.NewClientset(ctrlPod)

	fn := fixture.NewFunction()

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleScale(func(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
		return []*function.Instance{fixture.NewInstance(t, fn, nil)}, nil
	})

	c := New(func(host string, port int) Client { return mcc }, clientset, nil)

	err := c.startInformers(ctx)
	must.NoError(t, err)

	instances, err := c.scaleFunction(ctx, fn, 1)
	must.NoError(t, err)
	must.Len(t, 1, instances)
}
