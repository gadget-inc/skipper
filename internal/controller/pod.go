package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type podListerEntry struct {
	lister  listerv1.PodLister
	indexer cache.Indexer
}

func (c *Controller) startPodInformers(ctx context.Context) error {
	log.Info(ctx, "starting managed pod informers", key.Namespaces.Field(function.FlagNamespaces.Value()))

	// TODO: test all required permissions before starting informers
	var validNamespaces []string
	for _, namespace := range function.FlagNamespaces.Value() {
		_, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			if apierrors.IsForbidden(err) && function.FlagSkipForbiddenNamespaces.Value() {
				log.Warn(ctx, "skipping namespace", key.Namespace.Field(namespace), key.Error.Field(err))
				continue
			}
			return fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
		}

		validNamespaces = append(validNamespaces, namespace)

		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			c.clientset,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = key.Deployment.Label
			}),
		)

		podInformer := informerFactory.Core().V1().Pods()
		c.podListerMap[namespace] = podListerEntry{
			lister:  podInformer.Lister(),
			indexer: podInformer.Informer().GetIndexer(),
		}

		informerFactory.Start(ctx.Done())

		syncResults := informerFactory.WaitForCacheSync(ctx.Done())
		for informer, synced := range syncResults {
			if !synced {
				return fmt.Errorf("failed to sync managed pod informer cache: %v", informer)
			}
		}
	}

	_ = function.FlagNamespaces.SetValue(validNamespaces)

	return nil
}

func (c *Controller) getAssigned(fn function.Function) ([]*function.Instance, error) {
	assignedPods, err := c.listPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
		key.Status.Label:     StatusReady,
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
			return nil, fmt.Errorf("failed to convert pod to instance: %w", err)
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func (c *Controller) listPods(namespace string, selector labels.Selector) ([]*v1.Pod, error) {
	podListerEntry, found := c.podListerMap[namespace]
	if !found {
		return nil, fmt.Errorf("managed pod lister not started for namespace %s", namespace)
	}

	listedPods, err := podListerEntry.lister.List(selector)
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

func (c *Controller) updatePodCache(pod *v1.Pod) error {
	podListerEntry, found := c.podListerMap[pod.Namespace]
	if !found {
		return fmt.Errorf("managed pod lister not started for namespace %s", pod.Namespace)
	}
	return podListerEntry.indexer.Update(pod)
}
