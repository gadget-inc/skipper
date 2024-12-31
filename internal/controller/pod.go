package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/timer"
	appsv1 "k8s.io/api/apps/v1"
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

func (c *Controller) startControllerInformer(ctx context.Context) error {
	log.Info(ctx, "starting controller informer", key.Namespace.Field(FlagNamespace.Value()))

	controllerPodInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		10*time.Minute,
		informers.WithNamespace(FlagNamespace.Value()),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=fusion,app.kubernetes.io/component=controller"
		}),
	)

	controllerPodInformer := controllerPodInformerFactory.Core().V1().Pods().Informer()

	controllerPodHandler, err := controllerPodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				c.ring.Add(pod.Status.PodIP)
				log.Trace(ctx, "added controller", key.Pod.Field(pod))
			}
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				c.ring.Add(pod.Status.PodIP)
				log.Trace(ctx, "updated controller", key.Pod.Field(pod))
			} else {
				c.ring.Remove(pod.Status.PodIP)
				log.Trace(ctx, "removed updated controller", key.Pod.Field(pod))
			}
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			c.ring.Remove(pod.Status.PodIP)
			log.Trace(ctx, "removed deleted controller", key.Pod.Field(pod))
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	controllerPodInformerFactory.Start(ctx.Done())

	synced := cache.WaitForCacheSync(ctx.Done(), controllerPodHandler.HasSynced)
	if !synced {
		return fmt.Errorf("failed to sync controller pod informer")
	}

	go func() {
		timer.Loop(ctx, 10*time.Second, func(ctx context.Context) error {
			log.Trace(ctx, "controller ips", key.ControllerIPs.Field(c.ring.List()))
			return nil
		})
	}()

	return nil
}

func (c *Controller) startReplicaSetInformer(ctx context.Context) error {
	log.Info(ctx, "starting managed replica set informers", key.Namespaces.Field(function.FlagNamespaces.Value()))

	for _, namespace := range function.FlagNamespaces.Value() {
		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			c.clientset,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = key.Deployment.Label
			}),
		)

		replicaSetInformer := informerFactory.Apps().V1().ReplicaSets()
		replicaSetLister := replicaSetInformer.Lister()

		_, err := replicaSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				replicaSet := obj.(*appsv1.ReplicaSet)
				log.Trace(ctx, "replica set added", key.ReplicaSet.Field(replicaSet))
			},
			UpdateFunc: func(_, newObj any) {
				replicaSet := newObj.(*appsv1.ReplicaSet)
				log.Trace(ctx, "replica set updated", key.ReplicaSet.Field(replicaSet))
			},
			DeleteFunc: func(obj any) {
				replicaSet := obj.(*appsv1.ReplicaSet)
				log.Trace(ctx, "replica set deleted", key.ReplicaSet.Field(replicaSet))
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add replica set event handler: %w", err)
		}

		informerFactory.Start(ctx.Done())

		syncResults := informerFactory.WaitForCacheSync(ctx.Done())
		for informer, synced := range syncResults {
			if !synced {
				return fmt.Errorf("failed to sync managed deployment informer cache: %v", informer)
			}
		}

		go timer.Loop(ctx, 10*time.Second, func(ctx context.Context) error {
			pods, err := c.listPods(namespace, hasTenantSelector)
			if err != nil {
				log.Warn(ctx, "failed to get all assigned pods for replica set check", key.Error.Field(err))
				return nil
			}

			var defunctInstances []*function.Instance
			for _, pod := range pods {
				instance, err := function.FromPod(pod)
				if err != nil {
					log.Warn(ctx, "failed to get function from pod", key.Error.Field(err), key.Pod.Field(pod))
					continue
				}

				replicaSet, err := replicaSetLister.ReplicaSets(pod.Namespace).Get(instance.Version)
				if err != nil {
					log.Warn(ctx, "failed to get replica set for pod", key.Error.Field(err), key.Pod.Field(pod))
					continue
				}

				if replicaSet.Spec.Replicas == nil || *replicaSet.Spec.Replicas == 0 {
					defunctInstances = append(defunctInstances, instance)
				}
			}

			for _, instance := range defunctInstances {
				func() {
					scaleMu, _ := c.scaleMu.LoadOrCompute(instance.Function, func() *sync.Mutex { return new(sync.Mutex) })
					scaleMu.Lock()
					defer scaleMu.Unlock()

					log.Debug(ctx, "terminating defunct function", key.Instance.Field(instance))
					err = c.clientset.CoreV1().Pods(instance.Namespace).Delete(ctx, instance.Name, metav1.DeleteOptions{})
					if err != nil {
						log.Warn(ctx, "failed to terminate instance", key.Error.Field(err), key.Instance.Field(instance))
					}
				}()

				time.Sleep(1 * time.Second)
			}

			return nil
		})
	}

	return nil
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

func (c *Controller) getInstances(fn function.Function) ([]*function.Instance, error) {
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
