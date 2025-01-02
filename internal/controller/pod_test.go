package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/shoenig/test/must"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetAssigned(t *testing.T) {
	testCases := []struct {
		name              string
		fn                function.Function
		setupPods         func(t *testing.T, fn function.Function) []runtime.Object
		expectedInstances int
		err               error
	}{
		{
			name: "smoke",
			fn:   fixture.NewFunction(),
			setupPods: func(t *testing.T, fn function.Function) []runtime.Object {
				return []runtime.Object{fixture.NewAssignedPod(t, fn, nil)}
			},
			expectedInstances: 1,
		},
		{
			name: "long metadata",
			fn:   fixture.NewFunction(fixture.WithMetadata(strings.Repeat("a", 1024))),
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

			fixture.SetFlag(t, &function.FlagNamespaces, []string{tc.fn.Namespace})

			c := New(fake.NewClientset(tc.setupPods(t, tc.fn)...), nil)

			err := c.startControllerInformer(ctx)
			must.NoError(t, err)

			err = c.startPodInformers(ctx)
			must.NoError(t, err)

			instances, err := c.getInstances(tc.fn)
			if tc.err != nil {
				must.Error(t, err)
				return
			}

			must.NoError(t, err)
			must.Len(t, tc.expectedInstances, instances)
		})
	}
}
