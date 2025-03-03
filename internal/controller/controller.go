package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/telemetry"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/puzpuzpuz/xsync/v3"
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

// TODO: combine these map[function.Function] data structures into a single struct that handles a single function
type Controller struct {
	ring                 *hashring.HashRing
	newClientFunc        NewClientFunc
	controllerClients    *xsync.MapOf[string, Client]
	clientset            kubernetes.Interface
	metricsClientset     metricsclientset.Interface
	namespaceListers     map[string]namespaceLister
	scaleMu              *xsync.MapOf[function.Function, *sync.Mutex]
	heartbeats           *xsync.MapOf[function.Function, time.Time]
	stabilizationWindows *xsync.MapOf[function.Function, *StabilizationWindow]
}

func New(newClientFunc NewClientFunc, clientset kubernetes.Interface, metricsClient metricsclientset.Interface) *Controller {
	return &Controller{
		ring:                 hashring.New(),
		newClientFunc:        newClientFunc,
		controllerClients:    xsync.NewMapOf[string, Client](),
		clientset:            clientset,
		metricsClientset:     metricsClient,
		namespaceListers:     make(map[string]namespaceLister, len(function.FlagNamespaces.Value())),
		scaleMu:              xsync.NewMapOf[function.Function, *sync.Mutex](),
		heartbeats:           xsync.NewMapOf[function.Function, time.Time](),
		stabilizationWindows: xsync.NewMapOf[function.Function, *StabilizationWindow](),
	}
}

func (c *Controller) Start(ctx context.Context) error {
	err := c.startInformers(ctx)
	if err != nil {
		return fmt.Errorf("failed to start informers: %w", err)
	}

	go timer.Loop(ctx, FlagScaleInterval.Value(), func(ctx context.Context) error {
		for _, namespace := range function.FlagNamespaces.Value() {
			err := c.scaleFunctions(ctx, namespace)
			if err != nil {
				log.Error(ctx, "failed to scale instances", key.Error.Field(err))
			}
		}
		return nil
	})

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

func (c *Controller) isResponsibleForFunction(fn function.Function) bool {
	controllerIP := c.ring.Get(fn.RingKey())
	return controllerIP == FlagIP.Value()
}
