package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/shoenig/test/must"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetInstances(t *testing.T) {
	testCases := []struct {
		name              string
		setupPods         func(t *testing.T, fn function.Function) []runtime.Object
		expectedInstances int
		err               error
	}{
		{
			name: "one",
			setupPods: func(t *testing.T, fn function.Function) []runtime.Object {
				return []runtime.Object{fixture.NewAssignedPod(t, fn, nil)}
			},
			expectedInstances: 1,
		},
		{
			name: "many",
			setupPods: func(t *testing.T, fn function.Function) []runtime.Object {
				return []runtime.Object{fixture.NewAssignedPod(t, fn, nil), fixture.NewAssignedPod(t, fn, nil)}
			},
			expectedInstances: 2,
		},
		{
			name: "deleted",
			setupPods: func(t *testing.T, fn function.Function) []runtime.Object {
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				return []runtime.Object{pod}
			},
			expectedInstances: 0,
		},
		{
			name: "failed",
			setupPods: func(t *testing.T, fn function.Function) []runtime.Object {
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Status.Phase = v1.PodFailed
				return []runtime.Object{pod}
			},
			expectedInstances: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)

			fn := fixture.NewFunction()
			fixture.SetFlag(t, &function.FlagNamespaces, []string{fn.Namespace})

			c := New(fake.NewClientset(tc.setupPods(t, fn)...), nil)

			err := c.startControllerInformer(ctx)
			must.NoError(t, err)

			err = c.startPodInformers(ctx)
			must.NoError(t, err)

			instances, err := c.getInstances(fn)
			if tc.err != nil {
				must.Error(t, err)
				return
			}

			must.NoError(t, err)
			must.Len(t, tc.expectedInstances, instances)
		})
	}
}
