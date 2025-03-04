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
	kubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned"
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
	kubernetes           kubernetes.Interface
	kubernetesMetrics    kubernetesmetrics.Interface
	namespaceListers     map[string]namespaceLister
	scaleMu              *xsync.MapOf[function.Function, *sync.Mutex]
	heartbeats           *xsync.MapOf[function.Function, time.Time]
	stabilizationWindows *xsync.MapOf[function.Function, *StabilizationWindow]
}

func New(newClientFunc NewClientFunc, kubernetes kubernetes.Interface, kubernetesMetrics kubernetesmetrics.Interface) *Controller {
	return &Controller{
		ring:                 hashring.New(),
		newClientFunc:        newClientFunc,
		controllerClients:    xsync.NewMapOf[string, Client](),
		kubernetes:           kubernetes,
		kubernetesMetrics:    kubernetesMetrics,
		namespaceListers:     make(map[string]namespaceLister, len(function.FlagNamespaces.Value())),
		scaleMu:              xsync.NewMapOf[function.Function, *sync.Mutex](),
		heartbeats:           xsync.NewMapOf[function.Function, time.Time](),
		stabilizationWindows: xsync.NewMapOf[function.Function, *StabilizationWindow](),
	}
}

func (ctrl *Controller) Start(ctx context.Context) error {
	err := ctrl.startInformers(ctx)
	if err != nil {
		return fmt.Errorf("failed to start informers: %w", err)
	}

	go timer.Loop(ctx, FlagScaleInterval.Value(), func(ctx context.Context) error {
		for _, namespace := range function.FlagNamespaces.Value() {
			err := ctrl.scaleFunctions(ctx, namespace)
			if err != nil {
				log.Error(ctx, "failed to scale functions", key.Error.Field(err))
			}
		}
		return nil
	})

	return nil
}

func (ctrl *Controller) isResponsibleForFunction(fn function.Function) bool {
	ctrlIP := ctrl.ring.Get(fn.RingKey())
	return ctrlIP == FlagPodIP.Value()
}

func (ctrl *Controller) getControllerClient(ip string) Client {
	controllerClient, _ := ctrl.controllerClients.LoadOrCompute(ip, func() Client { return ctrl.newClientFunc(ip, FlagPort.Value()) })
	return controllerClient
}

func (ctrl *Controller) startInformers(ctx context.Context) error {
	ctx, span := telemetry.Start(ctx, "controller.start_informers")
	defer span.End()

	controllerPodInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		ctrl.kubernetes,
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
				ctrl.ring.Add(pod.Status.PodIP)
				log.Trace(ctx, "added controller", key.Pod.Field(pod))
			}
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				ctrl.ring.Add(pod.Status.PodIP)
				log.Trace(ctx, "updated controller", key.Pod.Field(pod))
			} else {
				ctrl.ring.Remove(pod.Status.PodIP)
				log.Trace(ctx, "removed updated controller", key.Pod.Field(pod))
			}
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			ctrl.ring.Remove(pod.Status.PodIP)
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
		_, err := ctrl.kubernetes.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			if apierrors.IsForbidden(err) && function.FlagSkipForbiddenNamespaces.Value() {
				log.Warn(ctx, "skipping namespace", key.Namespace.Field(namespace), key.Error.Field(err))
				continue
			}
			return fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
		}

		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			ctrl.kubernetes,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = key.Deployment.Label
			}),
		)

		podInformer := informerFactory.Core().V1().Pods()
		replicaSetInformer := informerFactory.Apps().V1().ReplicaSets()
		ctrl.namespaceListers[namespace] = namespaceLister{
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
