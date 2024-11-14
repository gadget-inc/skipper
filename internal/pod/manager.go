package pod

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/destination"
	"github.com/gadget-inc/fusion/internal/timer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
)

type Manager struct {
	clientset      *kubernetes.Clientset
	podListers     sync.Map
	assignmentLock sync.Map
}

func NewManager(clientset *kubernetes.Clientset) *Manager {
	return &Manager{clientset: clientset}
}

func (pm *Manager) Start(ctx context.Context, namespaces []string) error {
	slog.InfoContext(ctx, "starting managed pod informers", slog.Any("namespaces", namespaces))

	for _, namespace := range namespaces {
		informerFactory := informers.NewSharedInformerFactoryWithOptions(
			pm.clientset,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = "app.kubernetes.io/managed-by=fusion"
			}),
		)

		podInformer := informerFactory.Core().V1().Pods()
		podLister := podInformer.Lister()

		_, loaded := pm.podListers.LoadOrStore(namespace, podLister)
		if !loaded {
			informerFactory.Start(ctx.Done())
		}

		syncResults := informerFactory.WaitForCacheSync(ctx.Done())
		for informer, synced := range syncResults {
			if !synced {
				return fmt.Errorf("failed to sync managed pod informer cache: %v", informer)
			}
		}
	}

	return nil
}

func (pm *Manager) GetAssigned(dest destination.Destination) ([]*Pod, error) {
	return pm.listPods(dest.Namespace, labels.SelectorFromSet(labels.Set{
		labelTenant:     dest.Tenant,
		labelDeployment: dest.Deployment,
		labelStatus:     "ready",
	}))
}

func (pm *Manager) GetAvailable(dest destination.Destination) ([]*Pod, error) {
	noTenant, err := labels.NewRequirement(labelTenant, selection.DoesNotExist, nil)
	if err != nil {
		return nil, err
	}

	equalDeploymentName, err := labels.NewRequirement(labelDeployment, selection.Equals, []string{dest.Deployment})
	if err != nil {
		return nil, err
	}

	pods, err := pm.listPods(dest.Namespace, labels.NewSelector().Add(*noTenant, *equalDeploymentName))
	if err != nil {
		return nil, err
	}

	var availablePods []*Pod
	for _, pod := range pods {
		if pod.Status.PodIP != "" {
			availablePods = append(availablePods, pod)
		}
	}

	return availablePods, nil
}

func (pm *Manager) Assign(ctx context.Context, pod *Pod, dest destination.Destination) error {
	patchBody := fmt.Sprintf(`[{"op":"add","path":"/metadata/labels/%s","value":"%s"},{"op":"replace","path":"/metadata/labels/%s","value":"pending"}]`, patchLabelTenant, dest.Tenant, patchLabelStatus)
	_, err := pm.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, []byte(patchBody), metav1.PatchOptions{FieldManager: "fusion/serve"})
	if err != nil {
		return fmt.Errorf("failed to assign pod: %w", err)
	}

	assignCtx, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()

	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, "http://"+pod.Status.PodIP+":8080/__fusion/assign", nil)
	if err != nil {
		return fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(destination.HeaderTenant, dest.Tenant)
	req.Header.Set(destination.HeaderNamespace, dest.Namespace)
	req.Header.Set(destination.HeaderDeployment, dest.Deployment)
	req.Header.Set(destination.HeaderAssignment, dest.Assignment)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send assign request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("assign request returned status %d", resp.StatusCode)
	}

	patchBody = fmt.Sprintf(`[{"op":"replace","path":"/metadata/labels/%s","value":"ready"}]`, patchLabelStatus)
	_, err = pm.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, []byte(patchBody), metav1.PatchOptions{FieldManager: "fusion/serve"})
	if err != nil {
		return fmt.Errorf("failed to patch status: %w", err)
	}

	return nil
}

func (pm *Manager) listPods(namespace string, selector labels.Selector) ([]*Pod, error) {
	listerAny, found := pm.podListers.Load(namespace)
	if !found {
		return nil, fmt.Errorf("managed pod lister not started for namespace %s", namespace)
	}

	lister := listerAny.(listerv1.PodLister)
	k8sPods, err := lister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	pods := make([]*Pod, len(k8sPods))
	for i, k8sPod := range k8sPods {
		pods[i] = New(k8sPod)
	}
	return pods, nil
}

func (pm *Manager) GetOrAssignFor(ctx context.Context, dest destination.Destination) (*Pod, error) {
	return timer.Poll(ctx, 100*time.Millisecond, 5*time.Second, func(ctx context.Context) (*Pod, error) {
		assignedPods, err := pm.GetAssigned(dest)
		if err != nil {
			return nil, fmt.Errorf("failed to list assigned pods: %w", err)
		}
		if len(assignedPods) > 0 {
			return assignedPods[rand.Intn(len(assignedPods))], nil
		}

		_, assignmentInProgress := pm.assignmentLock.LoadOrStore(dest.String(), struct{}{})
		if assignmentInProgress {
			// another goroutine is already assigning a pod for this destination
			return nil, nil
		}
		defer pm.assignmentLock.Delete(dest.String())

		availablePods, err := pm.GetAvailable(dest)
		if err != nil {
			return nil, fmt.Errorf("failed to list available pods: %w", err)
		}
		if len(availablePods) == 0 {
			slog.WarnContext(ctx, "no available pods", slog.Any("destination", dest))
			return nil, nil
		}

		for _, pod := range availablePods {
			err := pm.Assign(ctx, pod, dest)
			if err != nil {
				slog.ErrorContext(ctx, "failed to assign pod", slog.Any("error", err), slog.Any("destination", dest))
				continue
			}
			return pod, nil
		}

		return nil, nil
	})
}
