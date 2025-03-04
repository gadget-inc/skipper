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
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	_ = function.FlagNamespaces.SetValue([]string{fixture.DefaultFunctionNamespace})
}

func TestGetInstances(t *testing.T) {
	testCases := []struct {
		name  string
		err   error
		setup func(*testing.T, *fake.Clientset, function.Function)
		check func(*testing.T, *fake.Clientset, []*function.Instance)
	}{
		{
			name: "one",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 1, instances)
			},
		},
		{
			name: "many",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)

				err = fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 2, instances)
			},
		},
		{
			name: "deleted",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)
			},
		},
		{
			name: "failed",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Status.Phase = v1.PodFailed
				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)
			},
		},
		{
			name: "different metadata",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fn.Metadata = "different"
				err := fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)
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

			instances, err := ctrl.getInstances(fn)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, fakeKubernetes, instances)
		})
	}
}
