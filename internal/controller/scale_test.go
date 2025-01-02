package controller

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/shoenig/test/must"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	function.FlagAssignPath.Init()
	function.FlagAssignTimeout.Init()
	function.FlagNamespaces.Init()
	function.FlagPort.Init()

	FlagIP.SetValue(fixture.DefaultControllerIP)
	FlagNamespace.SetValue(fixture.DefaultControllerNamespace)
}

func TestScaleFunction(t *testing.T) {
	testCases := []struct {
		name              string
		availablePods     int
		desiredInstances  int
		expectedInstances int
		err               error
	}{
		{
			name:              "smoke",
			availablePods:     1,
			desiredInstances:  1,
			expectedInstances: 1,
			err:               nil,
		},
		{
			name:              "none",
			availablePods:     0,
			desiredInstances:  1,
			expectedInstances: 0,
			err:               context.DeadlineExceeded,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)

			fn := fixture.NewFunction()
			fixture.SetFlag(t, &function.FlagNamespaces, []string{fn.Namespace})

			objects := []runtime.Object{fixture.NewControllerPod()}
			for i := 0; i < tc.availablePods; i++ {
				objects = append(objects, fixture.NewAvailablePod(t, fn, nil))
			}

			c := New(fake.NewClientset(objects...), nil)

			err := c.startControllerInformer(ctx)
			must.NoError(t, err)

			err = c.startPodInformers(ctx)
			must.NoError(t, err)

			instances, err := c.scaleFunction(ctx, fn, tc.desiredInstances)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
				return
			}

			must.NoError(t, err)
			must.Len(t, tc.expectedInstances, instances)
		})
	}
}

func TestAssignPodToFunction(t *testing.T) {
	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, function.Function) []runtime.Object
		check func(*testing.T, function.Function, *function.Instance, []v1.Pod)
	}{
		{
			name: "smoke",
			setup: func(t *testing.T, fn function.Function) []runtime.Object {
				return []runtime.Object{fixture.NewAvailablePod(t, fn, nil)}
			},
			check: func(t *testing.T, fn function.Function, instance *function.Instance, pods []v1.Pod) {
				must.Len(t, 1, pods)
				pod := pods[0]

				instance.Function.Metadata = fn.Metadata // TODO: remove this line
				must.Eq(t, fn, instance.Function)
				must.Eq(t, instance.Name, pod.Name)
				must.Eq(t, instance.Namespace, pod.Namespace)
				must.Eq(t, instance.Version, fixture.CurrentReplicaSet(fn))

				instanceIP, instancePort, err := net.SplitHostPort(instance.Addr)
				must.NoError(t, err)
				must.Eq(t, instanceIP, pod.Status.PodIP)
				must.Eq(t, instancePort, strconv.Itoa(int(pod.Spec.Containers[0].Ports[0].ContainerPort)))

				must.Eq(t, pod.Labels, map[string]string{
					key.ReplicaSet.Label:              instance.Version,
					key.Status.Label:                  StatusReady,
					key.Tenant.Label:                  fn.Tenant,
					key.Namespace.Label:               fn.Namespace,
					key.Deployment.Label:              fn.Deployment,
					key.MinInstances.Label:            strconv.Itoa(fn.MinInstances),
					key.MaxInstances.Label:            strconv.Itoa(fn.MaxInstances),
					key.TargetCPUUtilization.Label:    strconv.Itoa(fn.TargetCPUUtilization),
					key.TargetMemoryUtilization.Label: strconv.Itoa(fn.TargetMemoryUtilization),
					key.AssignedAt.Label:              strconv.FormatInt(instance.AssignedAt.Unix(), 10),
					key.ReadyAt.Label:                 strconv.FormatInt(instance.ReadyAt.Unix(), 10),
				})
			},
		},
		{
			name: "none",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, fn function.Function) []runtime.Object {
				return []runtime.Object{} // no pods
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)

			fn := fixture.NewFunction()
			fixture.SetFlag(t, &function.FlagNamespaces, []string{fn.Namespace})

			clientset := fake.NewClientset(append(tc.setup(t, fn), fixture.NewControllerPod())...)
			c := New(clientset, nil)

			err := c.startControllerInformer(ctx)
			must.NoError(t, err)

			err = c.startPodInformers(ctx)
			must.NoError(t, err)

			instance, err := c.assignPodToFunction(ctx, fn)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
				return
			}

			must.NoError(t, err)

			pods, err := clientset.CoreV1().Pods(fn.Namespace).List(ctx, metav1.ListOptions{})
			must.NoError(t, err)

			tc.check(t, fn, instance, pods.Items)
		})
	}
}
