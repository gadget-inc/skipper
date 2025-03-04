package controller

import (
	"context"
	"fmt"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (ctrl *Controller) getInstances(fn function.Function) ([]*function.Instance, error) {
	assignedPods, err := ctrl.listPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to list assigned pods: %w", err)
	}

	var instances []*function.Instance
	for _, pod := range assignedPods {
		if pod.Status.Phase != v1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}

		instance, err := function.FromPod(pod)
		if err != nil {
			return nil, fmt.Errorf("failed to get function from pod: %w", err)
		}

		if instance.ReadyAt.IsZero() {
			// pod is still being assigned
			continue
		}

		if instance.Function != fn {
			// pod is assigned to a different function
			continue
		}

		instances = append(instances, instance)
	}
	return instances, nil
}

func (ctrl *Controller) listPods(namespace string, selector labels.Selector) ([]*v1.Pod, error) {
	podListerEntry, found := ctrl.namespaceListers[namespace]
	if !found {
		return nil, fmt.Errorf("managed pod lister not started for namespace %s", namespace)
	}

	listedPods, err := podListerEntry.podLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var pods []*v1.Pod
	for _, pod := range listedPods {
		if pod.Status.Phase != v1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func (ctrl *Controller) updatePodCache(ctx context.Context, pod *v1.Pod) {
	namespaceLister, found := ctrl.namespaceListers[pod.Namespace]
	if !found {
		log.Warn(ctx, "managed pod lister not started for namespace", key.Namespace.Field(pod.Namespace))
		return
	}

	err := namespaceLister.podIndexer.Update(pod)
	if err != nil {
		log.Warn(ctx, "failed to update pod cache", key.Error.Field(err), key.Pod.Field(pod))
	}
}
