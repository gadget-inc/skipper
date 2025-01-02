package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/shoenig/test/must"
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
				must.Error(t, err)
				return
			}

			must.NoError(t, err)
			must.Len(t, tc.expectedInstances, instances)
		})
	}
}

func TestAssignPodToFunction(t *testing.T) {
	testCases := []struct {
		name          string
		availablePods int
		err           error
	}{
		{
			name:          "smoke",
			availablePods: 1,
			err:           nil,
		},
		{
			name:          "none",
			availablePods: 0,
			err:           context.DeadlineExceeded,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)

			fn := fixture.NewFunction()
			fixture.SetFlag(t, &function.FlagNamespaces, []string{fn.Namespace})

			k8sObjects := []runtime.Object{fixture.NewControllerPod()}
			for i := 0; i < tc.availablePods; i++ {
				k8sObjects = append(k8sObjects, fixture.NewAvailablePod(t, fn, nil))
			}

			c := New(fake.NewClientset(k8sObjects...), nil)

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
			instance.Function.Metadata = fn.Metadata // TODO: remove this line
			must.Eq(t, fn, instance.Function)
		})
	}
}
