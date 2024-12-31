package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetAssigned(t *testing.T) {
	testCases := []struct {
		name              string
		setupPods         func(t *testing.T, fn function.Function) []runtime.Object
		expectedInstances int
		err               error
	}{
		{
			name: "smoke",
			setupPods: func(t *testing.T, fn function.Function) []runtime.Object {
				return []runtime.Object{fixture.NewAssignedPod(t, fn, nil)}
			},
			expectedInstances: 1,
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
			require.NoError(t, err)

			err = c.startPodInformers(ctx)
			require.NoError(t, err)

			instances, err := c.getInstances(fn)
			if tc.err != nil {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Len(t, instances, tc.expectedInstances)
		})
	}
}
