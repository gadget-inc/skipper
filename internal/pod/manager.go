package pod

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/destination"
	"github.com/gadget-inc/fusion/internal/timer"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

type Manager struct {
	clientset        kubernetes.Interface
	metricsClientset metricsclientset.Interface
	podListers       sync.Map
	assignmentLock   sync.Map
}

func NewManager(clientset *kubernetes.Clientset, metricsClientset *metricsclientset.Clientset) *Manager {
	return &Manager{clientset: clientset, metricsClientset: metricsClientset}
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

		go func() {
			timer.Loop(ctx, 15*time.Second, func(ctx context.Context) error {
				podMetricList, err := pm.metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/managed-by=fusion," + labelTenant,
				})
				if err != nil {
					slog.WarnContext(ctx, "failed to list pod metrics", slog.Any("error", err))
				}

				podMetricsByTenant := make(map[string][]*v1beta1.PodMetrics)
				for _, podMetric := range podMetricList.Items {
					tenant := podMetric.Labels[labelTenant]
					deployment := podMetric.Labels[labelDeployment]
					key := deployment + "/" + tenant
					podMetricsByTenant[key] = append(podMetricsByTenant[key], &podMetric)
				}

				for key, podMetrics := range podMetricsByTenant {
					var totalCPUUsageMilli int64
					var totalMemoryUsageBytes int64
					for _, podMetric := range podMetrics {
						for _, containerMetric := range podMetric.Containers {
							totalCPUUsageMilli += containerMetric.Usage.Cpu().MilliValue()
							totalMemoryUsageBytes += containerMetric.Usage.Memory().Value()
						}
					}

					averageCPUUsageMilli := totalCPUUsageMilli / int64(len(podMetrics))
					averageMemoryUsageBytes := totalMemoryUsageBytes / int64(len(podMetrics))

					split := strings.Split(key, "/")
					slog.InfoContext(ctx, "pod metrics",
						slog.String("deployment", split[0]), slog.String("tenant", split[1]),
						slog.Int64("total_cpu", totalCPUUsageMilli), slog.Int64("total_memory", totalMemoryUsageBytes),
						slog.Int64("average_cpu", averageCPUUsageMilli), slog.Int64("average_memory", averageMemoryUsageBytes))
				}

				return nil
			})
		}()
	}

	return nil
}

func (pm *Manager) GetAssigned(dest destination.Destination) ([]*v1.Pod, error) {
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
		if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
			availablePods = append(availablePods, New(pod))
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
