package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/hashring"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/go-json-experiment/json"
	"github.com/puzpuzpuz/xsync/v4"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerappsv1 "k8s.io/client-go/listers/apps/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	kubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned"
)

type namespaceLister struct {
	podIndexer        cache.Indexer
	podLister         listerv1.PodLister
	replicaSetIndexer cache.Indexer
	replicaSetLister  listerappsv1.ReplicaSetLister
}

// TODO: combine these map[function.Function] data structures into a single struct that handles a single function
type Controller struct {
	startedAt            time.Time
	ring                 *hashring.HashRing
	newClientFunc        NewClientFunc
	controllerClients    *xsync.Map[string, Client]
	kubernetes           kubernetes.Interface
	kubernetesMetrics    kubernetesmetrics.Interface
	namespaceListers     map[string]namespaceLister
	scaleMu              *xsync.Map[function.Function, *sync.Mutex]
	routerHeartbeats     *xsync.Map[function.Function, RouterHeartbeats]
	stabilizationWindows *xsync.Map[function.Function, *StabilizationWindow]
}

func New(newClientFunc NewClientFunc, kubernetes kubernetes.Interface, kubernetesMetrics kubernetesmetrics.Interface) *Controller {
	return &Controller{
		ring:                 hashring.New(hashring.WithWaitTime(FlagHashRingWaitTime.Value())),
		newClientFunc:        newClientFunc,
		controllerClients:    xsync.NewMap[string, Client](),
		kubernetes:           kubernetes,
		kubernetesMetrics:    kubernetesMetrics,
		namespaceListers:     make(map[string]namespaceLister, len(function.FlagNamespaces.Value())),
		scaleMu:              xsync.NewMap[function.Function, *sync.Mutex](),
		routerHeartbeats:     xsync.NewMap[function.Function, RouterHeartbeats](),
		stabilizationWindows: xsync.NewMap[function.Function, *StabilizationWindow](),
	}
}

func (ctrl *Controller) Start(ctx context.Context) error {
	err := ctrl.startInformers(ctx)
	if err != nil {
		return fmt.Errorf("failed to start informers: %w", err)
	}

	for _, namespace := range function.FlagNamespaces.Value() {
		go timer.Loop(ctx, FlagScaleInterval.Value(), func(ctx context.Context) error {
			defer func() {
				if r := recover(); r != nil {
					log.Error(ctx, "panic in scaleNamespace", key.Error.Field(fmt.Errorf("%v", r)))
				}
			}()

			err := ctrl.scaleNamespace(ctx, namespace)
			if err != nil {
				log.Error(ctx, "failed to scale namespace", key.Error.Field(err), key.Namespace.Field(namespace))
			}
			return nil
		})
	}

	ctrl.startedAt = time.Now()

	return nil
}

func (ctrl *Controller) getControllerClient(ip string) Client {
	controllerClient, _ := ctrl.controllerClients.LoadOrCompute(ip, func() (Client, bool) { return ctrl.newClientFunc(ip, FlagPort.Value()), false })
	return controllerClient
}

func (ctrl *Controller) startInformers(ctx context.Context) error {
	ctx, span := telemetry.Trace(ctx, "controller.start_informers")
	defer span.End()

	controllerPodInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		ctrl.kubernetes,
		5*time.Minute,
		informers.WithNamespace(FlagNamespace.Value()),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=skipper,app.kubernetes.io/component=controller"
		}),
	)

	// keep track of the controller pod name to IP address so we can remove the controller from the ring if/when its IP disappears
	ctrlPodNameToIP := make(map[string]string)

	removeFromRing := func(pod *v1.Pod) {
		ctrlIP := ctrlPodNameToIP[pod.Name]
		if ctrlIP != "" {
			delete(ctrlPodNameToIP, pod.Name)
			ctrl.ring.Remove(ctrlIP)
			log.Debug(ctx, "removed controller from ring", key.Pod.Field(pod), slog.String("ring", strings.Join(ctrl.ring.List(), ",")))
		}
	}

	updateRing := func(pod *v1.Pod) {
		if isPodReady(pod) {
			ctrlPodNameToIP[pod.Name] = pod.Status.PodIP
			ctrl.ring.Add(pod.Status.PodIP)
			log.Debug(ctx, "added controller to ring", key.Pod.Field(pod), slog.String("ring", strings.Join(ctrl.ring.List(), ",")))
		} else {
			removeFromRing(pod)
		}
	}

	controllerPodHandler, err := controllerPodInformerFactory.Core().V1().Pods().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { updateRing(obj.(*v1.Pod)) },
		UpdateFunc: func(_, newObj any) { updateRing(newObj.(*v1.Pod)) },
		DeleteFunc: func(obj any) { removeFromRing(obj.(*v1.Pod)) },
	})
	if err != nil {
		return fmt.Errorf("failed to add controller pod event handler: %w", err)
	}

	controllerPodInformerFactory.Start(ctx.Done())
	synced := cache.WaitForCacheSync(ctx.Done(), controllerPodHandler.HasSynced)
	if !synced {
		return errors.New("failed to sync controller pod informer")
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
	instances, err := ctrl._getInstances(fn)
	if err != nil {
		return nil, err
	}

	// filter out instances that are not ready
	return slices.DeleteFunc(instances, func(instance *function.Instance) bool { return instance.ReadyAt.IsZero() }), nil
}

func (ctrl *Controller) _getInstances(fn function.Function) ([]*function.Instance, error) {
	assignedPods, err := ctrl.listPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to list assigned pods: %w", err)
	}

	instances := make([]*function.Instance, 0, len(assignedPods))
	for _, pod := range assignedPods {
		instance, err := instanceFromPod(pod)
		if err != nil {
			return nil, fmt.Errorf("failed to get instance from pod: %w", err)
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

	// filter out pods that are not running
	return slices.DeleteFunc(listedPods, func(pod *v1.Pod) bool { return !isPodRunning(pod) }), nil
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

func instanceFromPod(pod *v1.Pod) (*function.Instance, error) {
	if pod == nil {
		return nil, errors.New("pod is nil")
	}

	instance := &function.Instance{
		Name: pod.Name,
	}

	if fnJson, ok := pod.Annotations[key.Function.Label]; ok {
		err := json.Unmarshal([]byte(fnJson), &instance.Function)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal function from pod annotation: %w", err)
		}
	} else {
		return nil, errors.New("missing function annotation")
	}

	instance.ReplicaSet = pod.Annotations[key.ReplicaSet.Label]
	if instance.ReplicaSet == "" {
		return nil, errors.New("missing replica set annotation")
	}

	var err error
	instance.AssignedAt, err = time.Parse(time.RFC3339, pod.Annotations[key.AssignedAt.Label])
	if err != nil {
		return nil, fmt.Errorf("failed to parse assigned at annotation: %w", err)
	}

	if readyAtStr, ok := pod.Annotations[key.ReadyAt.Label]; ok && isPodReady(pod) {
		instance.ReadyAt, err = time.Parse(time.RFC3339, readyAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ready at annotation: %w", err)
		}
	}

	port, err := portFromPod(pod)
	if err != nil {
		return nil, err
	}

	instance.Addr = net.JoinHostPort(pod.Status.PodIP, port)

	return instance, nil
}

func portFromPod(pod *v1.Pod) (string, error) {
	port := pod.Annotations[key.Port.Label]
	if port == "" {
		// no port annotation, grab the first port from the first container
		if len(pod.Spec.Containers) > 0 && len(pod.Spec.Containers[0].Ports) > 0 {
			port = strconv.Itoa(int(pod.Spec.Containers[0].Ports[0].ContainerPort))
		}
	} else {
		// assume the port annotation is a named port and try to find
		// the actual port. if we don't find a matching named port,
		// we'll use the port annotation as the actual port
		for _, container := range pod.Spec.Containers {
			for _, containerPort := range container.Ports {
				if containerPort.Name == port {
					port = strconv.Itoa(int(containerPort.ContainerPort))
					break
				}
			}
		}
	}
	if port == "" {
		return "", fmt.Errorf("failed to get port for pod %s", pod.Name)
	}
	return port, nil
}
