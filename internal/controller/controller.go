package controller

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/telemetry"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerappsv1 "k8s.io/client-go/listers/apps/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

var (
	hasTenantRequirement labels.Requirement
	hasTenantSelector    labels.Selector

	doesNotHaveTenantRequirement labels.Requirement
	doesNotHaveTenantSelector    labels.Selector
)

func init() {
	hasTenant, err := labels.NewRequirement(key.Tenant.Label, selection.Exists, nil)
	if err != nil {
		panic(err)
	}

	hasTenantRequirement = *hasTenant
	hasTenantSelector = labels.NewSelector().Add(hasTenantRequirement)

	doesNotHaveTenant, err := labels.NewRequirement(key.Tenant.Label, selection.DoesNotExist, nil)
	if err != nil {
		panic(err)
	}

	doesNotHaveTenantRequirement = *doesNotHaveTenant
	doesNotHaveTenantSelector = labels.NewSelector().Add(doesNotHaveTenantRequirement)
}

type namespaceLister struct {
	podIndexer        cache.Indexer
	podLister         listerv1.PodLister
	replicaSetIndexer cache.Indexer
	replicaSetLister  listerappsv1.ReplicaSetLister
}

type Controller struct {
	ring              *hashring.HashRing
	newClientFunc     NewClientFunc
	controllerClients *xsync.MapOf[string, Client]
	clientset         kubernetes.Interface
	metricsClientset  metricsclientset.Interface
	namespaceListers  map[string]namespaceLister
	scaleMu           *xsync.MapOf[function.Function, *sync.Mutex]
	heartbeats        map[function.Function]time.Time
	heartbeatsMu      sync.Mutex // guards heartbeats
}

func New(newClientFunc NewClientFunc, clientset kubernetes.Interface, metricsClient metricsclientset.Interface) *Controller {
	return &Controller{
		ring:              hashring.New(),
		newClientFunc:     newClientFunc,
		controllerClients: xsync.NewMapOf[string, Client](),
		clientset:         clientset,
		metricsClientset:  metricsClient,
		namespaceListers:  make(map[string]namespaceLister, len(function.FlagNamespaces.Value())),
		scaleMu:           xsync.NewMapOf[function.Function, *sync.Mutex](),
		heartbeats:        make(map[function.Function]time.Time),
	}
}

func (c *Controller) Start(ctx context.Context) error {
	ctx, span := telemetry.Start(ctx, "controller.start")
	defer span.End()

	err := c.startInformers(ctx)
	if err != nil {
		return fmt.Errorf("failed to start informers: %w", err)
	}

	err = c.startScalingInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to start scaling tenant pods: %w", err)
	}

	return nil
}

func (c *Controller) getControllerClient(ip string) Client {
	controllerClient, _ := c.controllerClients.LoadOrCompute(ip, func() Client { return c.newClientFunc(ip, FlagPort.Value()) })
	return controllerClient
}

func (c *Controller) startInformers(ctx context.Context) error {
	ctx, span := telemetry.Start(ctx, "controller.start_informers")
	defer span.End()

	controllerPodInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset,
		5*time.Minute,
		informers.WithNamespace(FlagNamespace.Value()),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=fusion,app.kubernetes.io/component=controller"
		}),
	)

	controllerPodHandler, err := controllerPodInformerFactory.Core().V1().Pods().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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
		return fmt.Errorf("failed to add controller pod event handler: %w", err)
	}

	controllerPodInformerFactory.Start(ctx.Done())
	synced := cache.WaitForCacheSync(ctx.Done(), controllerPodHandler.HasSynced)
	if !synced {
		return fmt.Errorf("failed to sync controller pod informer")
	}

	for _, namespace := range function.FlagNamespaces.Value() {
		_, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			if apierrors.IsForbidden(err) && function.FlagSkipForbiddenNamespaces.Value() {
				log.Warn(ctx, "skipping namespace", key.Namespace.Field(namespace), key.Error.Field(err))
				continue
			}
			return fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
		}

		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			c.clientset,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = key.Deployment.Label
			}),
		)

		podInformer := informerFactory.Core().V1().Pods()
		replicaSetInformer := informerFactory.Apps().V1().ReplicaSets()
		c.namespaceListers[namespace] = namespaceLister{
			podIndexer:        podInformer.Informer().GetIndexer(),
			podLister:         podInformer.Lister(),
			replicaSetIndexer: replicaSetInformer.Informer().GetIndexer(),
			replicaSetLister:  replicaSetInformer.Lister(),
		}

		informerFactory.Start(ctx.Done())

		syncResults := informerFactory.WaitForCacheSync(ctx.Done())
		for informer, synced := range syncResults {
			if !synced {
				return fmt.Errorf("failed to sync informer cache: %v", informer)
			}
		}
	}

	return nil
}

func (c *Controller) startScalingInstances(ctx context.Context) error {
	ctx, span := telemetry.Start(ctx, "controller.start_scaling_instances")
	defer span.End()

	stabilizationWindows := make(map[function.Function]*StabilizationWindow)

	go timer.Loop(
		ctx,
		15*time.Second,
		func(ctx context.Context) error {
			ctx, span := telemetry.Start(ctx, "controller.scale_instances")
			defer span.End()

			c.heartbeatsMu.Lock()
			heartbeats := maps.Clone(c.heartbeats)
			c.heartbeatsMu.Unlock()

			for _, namespace := range function.FlagNamespaces.Value() {
				functionMetrics, err := c.getFunctionMetrics(ctx, namespace)
				if err != nil {
					log.Error(ctx, "failed to get function metrics", key.Error.Field(err))
					return nil
				}

				now := time.Now()
				for fn, instanceMetrics := range functionMetrics {
					timestamp := heartbeats[fn]
					for _, instanceMetric := range instanceMetrics {
						if instanceMetric.AssignedAt.After(timestamp) {
							timestamp = instanceMetric.AssignedAt
						}
					}

					if time.Since(timestamp) > 90*time.Second {
						delete(stabilizationWindows, fn)

						controllerIP := c.ring.Get(fn.RingKey())
						if controllerIP != FlagIP.Value() {
							log.Trace(ctx, "skipping scaling fn to 0, not assigned to this controller", key.Function.Field(fn), key.ControllerIP.Field(controllerIP), key.IP.Field(FlagIP.Value()))
							continue
						}

						log.Trace(ctx, "scaling function to 0", key.Function.Field(fn), key.Timestamp.Field(timestamp))
						_, err := c.scaleFunction(ctx, fn, 0)
						if err != nil {
							log.Error(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn))
						}
						continue
					}

					currentInstances := len(instanceMetrics)
					desiredInstances, err := calculateDesiredInstances(instanceMetrics, now)
					if err != nil {
						log.Trace(ctx, "failed to calculate desired instances", key.Error.Field(err), key.Function.Field(fn))
						continue
					}

					if desiredInstances < fn.Scale.MinInstances {
						desiredInstances = fn.Scale.MinInstances
					}

					if desiredInstances > fn.Scale.MaxInstances {
						desiredInstances = fn.Scale.MaxInstances
					}

					stabilizationWindow, exists := stabilizationWindows[fn]
					if !exists {
						stabilizationWindow = &StabilizationWindow{}
						stabilizationWindows[fn] = stabilizationWindow
					}

					log.Trace(ctx, "desired instances", key.Function.Field(fn), key.CurrentInstances.Field(currentInstances), key.DesiredInstances.Field(desiredInstances), key.MaxRecommendedInstances.Field(stabilizationWindow.GetMaxRecommendation()))
					stabilizationWindow.RecordRecommendation(desiredInstances, now)

					controllerIP := c.ring.Get(fn.RingKey())
					if controllerIP != FlagIP.Value() {
						log.Trace(ctx, "skipping scaling for function, not assigned to this controller", key.Function.Field(fn), key.Controller.Field(controllerIP), key.IP.Field(FlagIP.Value()))
						continue
					}

					if desiredInstances < currentInstances {
						desiredInstances = min(currentInstances, stabilizationWindow.GetMaxRecommendation())
					}

					if desiredInstances == 0 {
						// we only scale to 0 if the last request was more than 90 seconds ago
						log.Debug(ctx, "skipping scaling function to 0 based on hpa", key.Function.Field(fn))
						continue
					}

					_, err = c.scaleFunction(ctx, fn, desiredInstances)
					if err != nil {
						log.Warn(ctx, "failed to scale function", key.Error.Field(err), key.Function.Field(fn), key.CurrentInstances.Field(currentInstances), key.DesiredInstances.Field(desiredInstances))
					}
				}

				pods, err := c.listPods(namespace, hasTenantSelector)
				if err != nil {
					log.Error(ctx, "failed to get assigned pods", key.Error.Field(err))
					continue
				}

				var staleInstances []*function.Instance
				for _, pod := range pods {
					instance, err := function.FromPod(pod)
					if err != nil {
						log.Warn(ctx, "failed to get instance from pod", key.Error.Field(err), key.Pod.Field(pod))
						err = c.clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
						if err != nil {
							log.Error(ctx, "failed to terminate pod", key.Error.Field(err), key.Pod.Field(pod))
						}
						continue
					}

					if instance.ReadyAt.IsZero() && time.Since(instance.AssignedAt) > function.FlagAssignTimeout.Value()*2 {
						log.Warn(ctx, "terminating instance stuck in assigned state", key.Instance.Field(instance))
						err = c.clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
						if err != nil {
							log.Error(ctx, "failed to terminate instance stuck in assigned state", key.Error.Field(err), key.Pod.Field(pod))
						}
						continue
					}

					replicaSet, err := c.namespaceListers[namespace].replicaSetLister.ReplicaSets(pod.Namespace).Get(instance.Version)
					if err != nil {
						log.Warn(ctx, "failed to get replica set for pod", key.Error.Field(err), key.Pod.Field(pod))
						continue
					}

					if replicaSet.Status.Replicas == 0 {
						staleInstances = append(staleInstances, instance)
					}
				}

				for _, staleInstance := range staleInstances {
					replicaSets, err := c.namespaceListers[namespace].replicaSetLister.List(labels.SelectorFromSet(labels.Set{key.Deployment.Label: staleInstance.Deployment}))
					if err != nil {
						log.Error(ctx, "failed to list replica sets", key.Error.Field(err), key.Instance.Field(staleInstance))
						continue
					}

					var activeReplicaSet *appsv1.ReplicaSet
					for _, replicaSet := range replicaSets {
						if replicaSet.Status.Replicas > 0 {
							activeReplicaSet = replicaSet
							break
						}
					}

					if activeReplicaSet == nil {
						log.Warn(ctx, "no active replica set found", key.Instance.Field(staleInstance))
						continue
					}

					if activeReplicaSet.Status.AvailableReplicas < max(1, activeReplicaSet.Status.Replicas/2) {
						log.Info(ctx, "replica set does not have enough available replicas to terminate stale instance", key.Instance.Field(staleInstance), key.ReplicaSet.Field(activeReplicaSet))
						continue
					}

					scaleMu, _ := c.scaleMu.LoadOrCompute(staleInstance.Function, func() *sync.Mutex { return new(sync.Mutex) })
					scaleMu.Lock()
					log.Info(ctx, "terminating stale instance", key.Instance.Field(staleInstance))
					err = c.clientset.CoreV1().Pods(staleInstance.Namespace).Delete(ctx, staleInstance.Name, metav1.DeleteOptions{})
					if err != nil {
						log.Error(ctx, "failed to terminate stale instance", key.Error.Field(err), key.Instance.Field(staleInstance))
					}
					scaleMu.Unlock()
				}
			}

			return nil
		},
	)

	return nil
}
