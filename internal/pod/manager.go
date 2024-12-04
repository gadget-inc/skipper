package pod

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	StatusPending    = "pending"
	StatusReady      = "ready"
	StatusUnassigned = "unassigned"
)

type Manager struct {
	clientset  kubernetes.Interface
	podListers map[string]listerv1.PodLister
}

func NewManager(clientset kubernetes.Interface) *Manager {
	return &Manager{clientset: clientset, podListers: make(map[string]listerv1.PodLister, len(function.FlagNamespaces.Value))}
}

func (pm *Manager) Start(ctx context.Context) error {
	log.Info(ctx, "starting managed pod informers", slog.Any("namespaces", function.FlagNamespaces.Value))

	// TODO: test all required permissions before starting informers
	var validNamespaces []string
	for _, namespace := range function.FlagNamespaces.Value {
		_, err := pm.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			if apierrors.IsForbidden(err) && function.FlagSkipForbiddenNamespaces.Value {
				log.Warn(ctx, "skipping namespace", slog.String("namespace", namespace), key.Error.Field(err))
				continue
			}
			return fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
		}

		validNamespaces = append(validNamespaces, namespace)

		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			pm.clientset,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = key.Deployment.Label
			}),
		)

		podInformer := informerFactory.Core().V1().Pods()
		podLister := podInformer.Lister()

		_, err = podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				pod := obj.(*v1.Pod)
				log.Trace(ctx, "pod added", key.Pod.Field(pod))
			},
			UpdateFunc: func(_, newObj any) {
				pod := newObj.(*v1.Pod)
				log.Trace(ctx, "pod updated", key.Pod.Field(pod))
			},
			DeleteFunc: func(obj any) {
				pod := obj.(*v1.Pod)
				log.Trace(ctx, "pod deleted", key.Pod.Field(pod))
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add pod informer event handler: %w", err)
		}

		informerFactory.Start(ctx.Done())
		pm.podListers[namespace] = podLister

		syncResults := informerFactory.WaitForCacheSync(ctx.Done())
		for informer, synced := range syncResults {
			if !synced {
				return fmt.Errorf("failed to sync managed pod informer cache: %v", informer)
			}
		}
	}

	function.FlagNamespaces.Value = validNamespaces

	return nil
}

func (pm *Manager) GetAssigned(fn function.Function) ([]function.Instance, error) {
	assignedPods, err := pm.ListPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
		key.Status.Label:     StatusReady,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to list assigned pods: %w", err)
	}

	var instances []function.Instance
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

func (pm *Manager) ListPods(namespace string, selector labels.Selector) ([]*v1.Pod, error) {
	lister, found := pm.podListers[namespace]
	if !found {
		return nil, fmt.Errorf("managed pod lister not started for namespace %s", namespace)
	}

	listedPods, err := lister.List(selector)
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
