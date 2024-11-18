package pod

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/timer"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
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
	clientset      kubernetes.Interface
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

		podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod := obj.(*v1.Pod)
				slog.DebugContext(ctx, "pod added", slog.String("pod", pod.Name), slog.String("status", string(pod.Status.Phase)), slog.String("ip", pod.Status.PodIP))
			},
			UpdateFunc: func(_, newObj interface{}) {
				pod := newObj.(*v1.Pod)
				slog.DebugContext(ctx, "pod updated", slog.String("pod", pod.Name), slog.String("status", string(pod.Status.Phase)), slog.String("ip", pod.Status.PodIP))
			},
			DeleteFunc: func(obj interface{}) {
				pod := obj.(*v1.Pod)
				slog.DebugContext(ctx, "pod deleted", slog.String("pod", pod.Name))
			},
		})

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

func (pm *Manager) GetAssigned(fn function.Function) ([]*v1.Pod, error) {
	assignedPods, err := pm.listPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
		key.Status.Label:     StatusReady,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to list assigned pods: %w", err)
	}

	var readyPods []*v1.Pod
	for _, pod := range assignedPods {
		if pod.Status.Phase != v1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}
		readyPods = append(readyPods, pod)
	}
	return readyPods, nil
}

func (pm *Manager) GetAssignedAndPending(fn function.Function) ([]*v1.Pod, error) {
	return pm.listPods(fn.Namespace, labels.SelectorFromSet(labels.Set{
		key.Tenant.Label:     fn.Tenant,
		key.Deployment.Label: fn.Deployment,
	}))
}

func (pm *Manager) GetAvailable(fn function.Function) ([]*v1.Pod, error) {
	noTenant, err := labels.NewRequirement(key.Tenant.Label, selection.DoesNotExist, nil)
	if err != nil {
		return nil, err
	}

	equalDeploymentName, err := labels.NewRequirement(key.Deployment.Label, selection.Equals, []string{fn.Deployment})
	if err != nil {
		return nil, err
	}

	pods, err := pm.listPods(fn.Namespace, labels.NewSelector().Add(*noTenant, *equalDeploymentName))
	if err != nil {
		return nil, err
	}

	var availablePods []*v1.Pod
	for _, pod := range pods {
		if pod.Status.PodIP != "" {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
					availablePods = append(availablePods, pod)
				}
			}
		}
	}

	return availablePods, nil
}

func (pm *Manager) Assign(ctx context.Context, fn function.Function) (*v1.Pod, error) {
	pod, err := timer.Poll(ctx, 250*time.Millisecond, 10*time.Second, func(ctx context.Context) (*v1.Pod, error) {
		availablePods, err := pm.GetAvailable(fn)
		if err != nil {
			return nil, fmt.Errorf("failed to list available pods: %w", err)
		}
		if len(availablePods) == 0 {
			slog.WarnContext(ctx, "no available pods", key.Function.Field(fn))
			return nil, nil
		}

		return availablePods[rand.Intn(len(availablePods))], nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to poll for available pod: %w", err)
	}

	slog.InfoContext(ctx, "assigning pod", slog.Any("pod", pod.Name), key.Function.Field(fn))

	assignPatches := []patchOperation{
		{Op: "replace", Path: key.Status.PatchLabel, Value: StatusPending},
		{Op: "add", Path: key.Tenant.PatchLabel, Value: fn.Tenant},
		{Op: "add", Path: key.Namespace.PatchLabel, Value: fn.Namespace},
		{Op: "add", Path: key.Deployment.PatchLabel, Value: fn.Deployment},
		{Op: "add", Path: key.MinReplicas.PatchLabel, Value: fn.MinReplicasStr},
		{Op: "add", Path: key.MaxReplicas.PatchLabel, Value: fn.MaxReplicasStr},
		{Op: "add", Path: key.TargetCPUUtilization.PatchLabel, Value: fn.TargetCPUUtilizationStr},
		{Op: "add", Path: key.TargetMemoryUtilization.PatchLabel, Value: fn.TargetMemoryUtilizationStr},
		{Op: "add", Path: key.AssignedAt.PatchLabel, Value: strconv.FormatInt(time.Now().Unix(), 10)},
	}

	patchBody, err := json.Marshal(assignPatches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal assign patch: %w", err)
	}

	_, err = pm.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patchBody, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		return nil, fmt.Errorf("failed to assign pod: %w", err)
	}

	assignCtx, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()

	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, "http://"+pod.Status.PodIP+":8080/__fusion/assign", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(key.Tenant.Header, fn.Tenant)
	req.Header.Set(key.Metadata.Header, fn.Metadata)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send assign request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("assign request returned status %d", resp.StatusCode)
	}

	setReadyPatches := []patchOperation{
		{Op: "replace", Path: key.Status.PatchLabel, Value: StatusReady},
		{Op: "add", Path: key.ReadyAt.PatchLabel, Value: strconv.FormatInt(time.Now().Unix(), 10)},
	}

	patchBody, err = json.Marshal(setReadyPatches)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal set ready patch: %w", err)
	}

	pod, err = pm.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patchBody, metav1.PatchOptions{FieldManager: key.Controller.Label})
	if err != nil {
		return nil, fmt.Errorf("failed to patch status: %w", err)
	}

	return pod, nil
}

func (pm *Manager) Terminate(ctx context.Context, fn function.Function, pod *v1.Pod) error {
	return pm.clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
}

func (pm *Manager) GetAllAssignedPods(namespace string) ([]*v1.Pod, error) {
	hasTenant, err := labels.NewRequirement(key.Tenant.Label, selection.Exists, nil)
	if err != nil {
		return nil, err
	}
	return pm.listPods(namespace, labels.NewSelector().Add(*hasTenant))
}

func (pm *Manager) listPods(namespace string, selector labels.Selector) ([]*v1.Pod, error) {
	listerAny, found := pm.podListers.Load(namespace)
	if !found {
		return nil, fmt.Errorf("managed pod lister not started for namespace %s", namespace)
	}

	lister := listerAny.(listerv1.PodLister)
	k8sPods, err := lister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return k8sPods, nil
}

type patchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}
