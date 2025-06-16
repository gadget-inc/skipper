package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/shoenig/test/must"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	FlagHashRingWaitTime.Init()
	_ = function.FlagNamespaces.SetValue([]string{fixture.FunctionNamespace})
}

func TestGetReadyInstances(t *testing.T) {
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
			name: "no pod IP",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Status.PodIP = ""
				err := fakeKubernetes.Tracker().Add(pod)
				must.NoError(t, err)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				must.Len(t, 0, instances)
			},
		},
		{
			name: "not ready",
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
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

			instances, err := ctrl.getReadyInstances(fn)
			if tc.err != nil {
				must.ErrorIs(t, err, tc.err)
			} else {
				must.NoError(t, err)
			}

			tc.check(t, fakeKubernetes, instances)
		})
	}
}

func TestControllerInformer(t *testing.T) {
	assertCtrlPodInRing := func(t *testing.T, ctrl *Controller) {
		t.Helper()
		ringIps := ctrl.ring.List()
		must.Len(t, 1, ringIps)
		must.Eq(t, ringIps[0], fixture.ControllerIP)
	}

	assertCtrlPodNotInRing := func(t *testing.T, ctrl *Controller) {
		t.Helper()
		ringIps := ctrl.ring.List()
		must.Len(t, 0, ringIps)
	}

	testCases := []struct {
		name   string
		setup  func(*testing.T) *v1.Pod
		change func(*testing.T, *v1.Pod, *fake.Clientset, *Controller)
		check  func(*testing.T, *v1.Pod, *fake.Clientset, *Controller)
	}{
		{
			name: "pod exists",
			setup: func(t *testing.T) *v1.Pod {
				return fixture.NewControllerPod()
			},
			check: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodInRing(t, ctrl)
			},
		},
		{
			name: "pod added",
			setup: func(t *testing.T) *v1.Pod {
				return nil
			},
			change: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				fakeKubernetes.Tracker().Add(fixture.NewControllerPod())
			},
			check: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodInRing(t, ctrl)
			},
		},
		{
			name: "pod deleted",
			setup: func(t *testing.T) *v1.Pod {
				return fixture.NewControllerPod()
			},
			change: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodInRing(t, ctrl)
				fakeKubernetes.Tracker().Delete(v1.SchemeGroupVersion.WithResource("pods"), ctrlPod.Namespace, ctrlPod.Name)
			},
			check: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodNotInRing(t, ctrl)
			},
		},
		{
			name: "pod updated with condition not ready",
			setup: func(t *testing.T) *v1.Pod {
				return fixture.NewControllerPod()
			},
			change: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodInRing(t, ctrl)
				ctrlPod.Status.Conditions = []v1.PodCondition{
					{
						Type:   v1.PodReady,
						Status: v1.ConditionFalse,
					},
				}
				fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), ctrlPod, ctrlPod.Namespace)
			},
			check: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodNotInRing(t, ctrl)
			},
		},
		{
			name: "pod updated with phase succeeded",
			setup: func(t *testing.T) *v1.Pod {
				return fixture.NewControllerPod()
			},
			change: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodInRing(t, ctrl)
				ctrlPod.Status.Phase = v1.PodSucceeded
				fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), ctrlPod, ctrlPod.Namespace)
			},
			check: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodNotInRing(t, ctrl)
			},
		},
		{
			name: "pod updated with phase failed",
			setup: func(t *testing.T) *v1.Pod {
				return fixture.NewControllerPod()
			},
			change: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodInRing(t, ctrl)
				ctrlPod.Status.Phase = v1.PodFailed
				fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), ctrlPod, ctrlPod.Namespace)
			},
			check: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodNotInRing(t, ctrl)
			},
		},
		{
			name: "pod updated with phase unknown",
			setup: func(t *testing.T) *v1.Pod {
				return fixture.NewControllerPod()
			},
			change: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodInRing(t, ctrl)
				ctrlPod.Status.Phase = v1.PodUnknown
				fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), ctrlPod, ctrlPod.Namespace)
			},
			check: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodNotInRing(t, ctrl)
			},
		},
		{
			name: "pod updated with no ip",
			setup: func(t *testing.T) *v1.Pod {
				return fixture.NewControllerPod()
			},
			change: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodInRing(t, ctrl)
				ctrlPod.Status.PodIP = ""
				fakeKubernetes.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), ctrlPod, ctrlPod.Namespace)
			},
			check: func(t *testing.T, ctrlPod *v1.Pod, fakeKubernetes *fake.Clientset, ctrl *Controller) {
				assertCtrlPodNotInRing(t, ctrl)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			ctrlPod := tc.setup(t)
			fakeKubernetes := fake.NewClientset()
			fakeKubernetes.Tracker().Add(ctrlPod)

			ctrl := New(nil, fakeKubernetes, nil)
			err := ctrl.startInformers(ctx)
			must.NoError(t, err)

			if tc.change != nil {
				tc.change(t, ctrlPod, fakeKubernetes, ctrl)
				time.Sleep(100 * time.Millisecond)
			}

			tc.check(t, ctrlPod, fakeKubernetes, ctrl)
		})
	}
}
