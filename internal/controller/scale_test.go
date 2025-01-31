package controller

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/shoenig/test/must"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	function.FlagAssignPath.Init()
	function.FlagAssignTimeout.Init()
	function.FlagPort.Init()
	function.FlagNamespaces.SetValue([]string{fixture.DefaultFunctionNamespace})

	FlagPort.Init()
	FlagIP.SetValue(fixture.DefaultControllerIP)
	FlagNamespace.SetValue(fixture.DefaultControllerNamespace)
}

func TestAssignPodToFunction(t *testing.T) {
	ensureInstanceMatchesPod := func(t *testing.T, instance *function.Instance, pod v1.Pod) {
		must.Eq(t, instance.Function.Deployment, pod.Labels[key.Deployment.Label])
		must.Eq(t, instance.Function.Tenant, pod.Labels[key.Tenant.Label])

		fnJSON, err := json.Marshal(instance.Function)
		must.NoError(t, err)
		must.Eq(t, string(fnJSON), pod.Annotations[key.Function.Label])

		must.Eq(t, instance.Name, pod.Name)
		must.Eq(t, instance.Addr, pod.Status.PodIP+":"+strconv.Itoa(int(pod.Spec.Containers[0].Ports[0].ContainerPort)))
		must.Eq(t, instance.Version, pod.Annotations[key.ReplicaSet.Label])
		must.Eq(t, instance.AssignedAt.Format(time.RFC3339), pod.Annotations[key.AssignedAt.Label])
		must.Eq(t, instance.ReadyAt.Format(time.RFC3339), pod.Annotations[key.ReadyAt.Label])
	}

	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *fake.Clientset, function.Function)
		check func(*testing.T, *fake.Clientset, *function.Instance)
	}{
		{
			name: "smoke",
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				err := clientset.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, clientset *fake.Clientset, instance *function.Instance) {
				pods, err := clientset.CoreV1().Pods(instance.Namespace).List(context.Background(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensureInstanceMatchesPod(t, instance, pods.Items[0])
			},
		},
		{
			name: "no available pods",
			err:  context.DeadlineExceeded,
			setup: func(t *testing.T, clientset *fake.Clientset, fn function.Function) {
				// no pods
			},
			check: func(t *testing.T, clientset *fake.Clientset, instance *function.Instance) {
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

				// initially no pods
			},
			check: func(t *testing.T, clientset *fake.Clientset, instance *function.Instance) {
				pods, err := clientset.CoreV1().Pods(instance.Namespace).List(context.Background(), metav1.ListOptions{})
				must.NoError(t, err)
				must.Len(t, 1, pods.Items)
				ensureInstanceMatchesPod(t, instance, pods.Items[0])
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

			err := c.startControllerInformer(ctx)
			must.NoError(t, err)

			err = c.startPodInformers(ctx)
			must.NoError(t, err)

			instance, err := c.assignPodToFunction(ctx, fn)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, clientset, instance)
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

			err := c.startControllerInformer(ctx)
			must.NoError(t, err)

			err = c.startPodInformers(ctx)
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

	var mccWasCalled bool

	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleScale(fn, func(ctx context.Context, fn function.Function, desiredInstances int) ([]*function.Instance, error) {
		mccWasCalled = true
		return []*function.Instance{fixture.NewInstance(t, fn, nil)}, nil
	})

	c := New(func(host string, port int) Client { return mcc }, clientset, nil)

	err := c.startControllerInformer(ctx)
	must.NoError(t, err)

	err = c.startPodInformers(ctx)
	must.NoError(t, err)

	instances, err := c.scaleFunction(ctx, fn, 1)
	must.NoError(t, err)

	must.True(t, mccWasCalled)
	must.Len(t, 1, instances)
}
