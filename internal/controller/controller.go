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
	"time"

	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/hashring"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/gadget-inc/skipper/internal/timer"
	"github.com/go-json-experiment/json"
	"github.com/puzpuzpuz/xsync/v4"
	appsv1 "k8s.io/api/apps/v1"
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

type Controller struct {
	config            *Config
	startedAt         time.Time
	ring              *hashring.HashRing
	supervisors       *xsync.Map[function.Hash, *Supervisor]
	newClientFunc     NewClientFunc
	controllerClients *xsync.Map[string, Client]
	kubernetes        kubernetes.Interface
	kubernetesMetrics kubernetesmetrics.Interface
	namespaceListers  map[string]namespaceLister
}

func New(cfg *Config, newClientFunc NewClientFunc, kubernetes kubernetes.Interface, kubernetesMetrics kubernetesmetrics.Interface) *Controller {
	return &Controller{
		config:            cfg,
		ring:              hashring.New(hashring.WithWaitTime(cfg.HashRingWaitTime)),
		supervisors:       xsync.NewMap[function.Hash, *Supervisor](),
		newClientFunc:     newClientFunc,
		controllerClients: xsync.NewMap[string, Client](),
		kubernetes:        kubernetes,
		kubernetesMetrics: kubernetesMetrics,
		namespaceListers:  make(map[string]namespaceLister, len(cfg.FunctionNamespaces)),
	}
}

func (ctrl *Controller) Start(ctx context.Context) error {
	err := ctrl.startInformers(ctx)
	if err != nil {
		return fmt.Errorf("failed to start informers: %w", err)
	}

	for _, namespace := range ctrl.config.FunctionNamespaces {
		go timer.Loop(ctx, ctrl.config.ScaleInterval, func(ctx context.Context) error {
			defer func() {
				if r := recover(); r != nil {
					log.Error(ctx, "panic in scaleNamespace", key.Error.Slog(fmt.Errorf("%v", r)))
				}
			}()

			err := ctrl.convergeNamespace(ctx, namespace)
			if err != nil {
				log.Error(ctx, "failed to scale namespace", key.Error.Slog(err), key.Namespace.Slog(namespace))
			}
			return nil
		})
	}

	ctrl.startedAt = time.Now()

	return nil
}

func (ctrl *Controller) getControllerClient(ip string) Client {
	controllerClient, _ := ctrl.controllerClients.LoadOrCompute(ip, func() (Client, bool) { return ctrl.newClientFunc(ip, ctrl.config.Port), false })
	return controllerClient
}

func (ctrl *Controller) startInformers(ctx context.Context) error {
	ctx, span := telemetry.Trace(ctx, "controller.start_informers")
	defer span.End()

	controllerPodInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		ctrl.kubernetes,
		5*time.Minute,
		informers.WithNamespace(ctrl.config.Namespace),
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
			log.Debug(ctx, "removed controller from ring", key.Pod.Slog(pod), slog.String("ring", strings.Join(ctrl.ring.List(), ",")))
		}
	}

	updateRing := func(pod *v1.Pod) {
		if isPodReady(pod) {
			ctrlPodNameToIP[pod.Name] = pod.Status.PodIP
			ctrl.ring.Add(pod.Status.PodIP)
			log.Debug(ctx, "added controller to ring", key.Pod.Slog(pod), slog.String("ring", strings.Join(ctrl.ring.List(), ",")))
		} else {
			removeFromRing(pod)
		}
	}

	controllerPodInformer := controllerPodInformerFactory.Core().V1().Pods().Informer()
	err := controllerPodInformer.SetWatchErrorHandler(ctrl.watchErrorHandler(ctx,
		slog.String("watch_resource", "controller_pods"),
		key.Namespace.Slog(ctrl.config.Namespace),
	))
	if err != nil {
		return fmt.Errorf("failed to set controller pod watch error handler: %w", err)
	}

	controllerPodHandler, err := controllerPodInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			RecordInformerEvent("controller_pods", "add", pod)
			updateRing(pod)
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*v1.Pod)
			RecordInformerEvent("controller_pods", "update", pod)
			updateRing(pod)
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			RecordInformerEvent("controller_pods", "delete", pod)
			removeFromRing(pod)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add controller pod event handler: %w", err)
	}

	controllerPodInformerFactory.Start(ctx.Done())
	synced := cache.WaitForCacheSync(ctx.Done(), controllerPodHandler.HasSynced)
	if !synced {
		return errors.New("failed to sync controller pod informer")
	}

	for _, namespace := range ctrl.config.FunctionNamespaces {
		_, err := ctrl.kubernetes.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			if apierrors.IsForbidden(err) && ctrl.config.SkipForbiddenNamespaces {
				log.Warn(ctx, "skipping namespace", key.Namespace.Slog(namespace), key.Error.Slog(err))
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
		_, err = podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				RecordInformerEvent("pods", "add", obj.(*v1.Pod))
			},
			UpdateFunc: func(_, newObj any) {
				RecordInformerEvent("pods", "update", newObj.(*v1.Pod))
			},
			DeleteFunc: func(obj any) {
				if deleted, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					RecordInformerEvent("pods", "delete", deleted)
				} else {
					RecordInformerEvent("pods", "delete", obj.(*v1.Pod))
				}
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add pod event handler: %w", err)
		}
		err = podInformer.Informer().SetWatchErrorHandler(ctrl.watchErrorHandler(ctx,
			slog.String("watch_resource", "pods"),
			key.Namespace.Slog(namespace),
		))
		if err != nil {
			return fmt.Errorf("failed to set pod watch error handler: %w", err)
		}

		replicaSetInformer := informerFactory.Apps().V1().ReplicaSets()
		_, err = replicaSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				RecordInformerEvent("replicasets", "add", obj.(*appsv1.ReplicaSet))
			},
			UpdateFunc: func(_, newObj any) {
				RecordInformerEvent("replicasets", "update", newObj.(*appsv1.ReplicaSet))
			},
			DeleteFunc: func(obj any) {
				if deleted, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					RecordInformerEvent("replicasets", "delete", deleted)
				} else {
					RecordInformerEvent("replicasets", "delete", obj.(*appsv1.ReplicaSet))
				}
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add replica set event handler: %w", err)
		}
		err = replicaSetInformer.Informer().SetWatchErrorHandler(ctrl.watchErrorHandler(ctx,
			slog.String("watch_resource", "replicasets"),
			key.Namespace.Slog(namespace),
		))
		if err != nil {
			return fmt.Errorf("failed to set replica set watch error handler: %w", err)
		}
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

func (ctrl *Controller) watchErrorHandler(ctx context.Context, attrs ...slog.Attr) cache.WatchErrorHandler {
	ctx = log.With(ctx, attrs...)

	return func(reflector *cache.Reflector, err error) {
		if err == nil {
			return
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.Debug(ctx, "informer watch error", key.Error.Slog(err))
			return
		}

		var statusErr *apierrors.StatusError
		if errors.As(err, &statusErr) && statusErr.ErrStatus.Reason == metav1.StatusReasonExpired {
			log.Debug(ctx, "informer watch error", key.Error.Slog(err))
			return
		}

		log.Warn(ctx, "informer watch error", key.Error.Slog(err))
	}
}

func (ctrl *Controller) getReadyInstances(fn *function.Function) ([]*function.Instance, error) {
	instances, err := ctrl.getInstances(fn)
	if err != nil {
		return nil, err
	}

	// filter out instances that are unready
	return slices.DeleteFunc(instances, func(instance *function.Instance) bool { return instance.ReadyAt.IsZero() }), nil
}

func (ctrl *Controller) getInstances(fn *function.Function) ([]*function.Instance, error) {
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

		if !instance.Function.Equal(fn) {
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
		log.Warn(ctx, "managed pod lister not started for namespace", key.Namespace.Slog(pod.Namespace))
		return
	}

	err := namespaceLister.podIndexer.Update(pod)
	if err != nil {
		log.Warn(ctx, "failed to update pod cache", key.Error.Slog(err), key.Pod.Slog(pod))
	}
}

func instanceFromPod(pod *v1.Pod) (*function.Instance, error) {
	if pod == nil {
		return nil, errors.New("pod is nil")
	}

	instance := &function.Instance{
		Function: &function.Function{},
		Name:     pod.Name,
	}

	if fnJson, ok := pod.Annotations[key.Function.Annotation]; ok {
		err := json.Unmarshal([]byte(fnJson), instance.Function)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal function from pod annotation: %w", err)
		}
		if err := instance.Validate(); err != nil {
			return nil, fmt.Errorf("invalid function in pod annotation: %w", err)
		}
	} else {
		return nil, errors.New("missing function annotation")
	}

	instance.ReplicaSet = pod.Annotations[key.ReplicaSet.Annotation]
	if instance.ReplicaSet == "" {
		return nil, errors.New("missing replica set annotation")
	}

	var err error
	instance.AssignedAt, err = time.Parse(time.RFC3339, pod.Annotations[key.AssignedAt.Annotation])
	if err != nil {
		return nil, fmt.Errorf("failed to parse assigned at annotation: %w", err)
	}

	if readyAtStr, ok := pod.Annotations[key.ReadyAt.Annotation]; ok && isPodReady(pod) {
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
	port := pod.Annotations[key.Port.Annotation]
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

func (ctrl *Controller) supervisor(fn *function.Function) *Supervisor {
	supervisor, _ := ctrl.supervisors.LoadOrCompute(fn.Hash(), func() (*Supervisor, bool) {
		return &Supervisor{fn: fn, ctrl: ctrl, routerHeartbeats: xsync.NewMap[string, *function.Heartbeat]()}, false
	})
	return supervisor
}
