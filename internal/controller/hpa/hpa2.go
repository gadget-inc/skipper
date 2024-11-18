package hpa

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/pod"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

func GetFunctionMetrics(ctx context.Context, podManager *pod.Manager, metricsClientset metricsclientset.Interface, namespace string) (map[function.Function]map[string]PodMetricsInfo, error) {
	pods, err := podManager.GetAllAssignedPods(namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get all assigned pods: %w", err)
	}

	podMetricsList, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=fusion," + key.Tenant.Label,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod metrics: %w", err)
	}

	metricsMap := make(map[string]metricsv1beta1.PodMetrics)
	for _, m := range podMetricsList.Items {
		metricsMap[m.Name] = m
	}

	functionsMap := make(map[function.Function]map[string]PodMetricsInfo)

	for _, pod := range pods {
		fn, err := function.FromLabels(pod.Labels)
		if err != nil {
			slog.WarnContext(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", pod.Name), slog.Any("labels", pod.Labels))
			continue
		}

		info := PodMetricsInfo{
			Pod:               pod,
			Function:          fn,
			Ready:             false,
			AssignedAt:        fn.AssignedAt,
			DeletionTimestamp: pod.DeletionTimestamp,
		}

		if fn.ReadyAt != nil {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
					info.Ready = true
					break
				}
			}
		}

		if m, exists := metricsMap[pod.Name]; exists {
			for _, c := range m.Containers {
				if c.Usage.Cpu() != nil {
					cpuUsage := c.Usage.Cpu().MilliValue()
					if info.CPUUsage == nil {
						info.CPUUsage = new(int64)
					}
					*info.CPUUsage += cpuUsage
				}
				if c.Usage.Memory() != nil {
					memUsage := c.Usage.Memory().Value()
					if info.MemoryUsage == nil {
						info.MemoryUsage = new(int64)
					}
					*info.MemoryUsage += memUsage
				}
			}
		} else {
			// Metrics missing for this pod
			info.CPUUsage = nil
			info.MemoryUsage = nil
		}

		if _, exists := functionsMap[fn.Function]; !exists {
			functionsMap[fn.Function] = make(map[string]PodMetricsInfo)
		}

		functionsMap[fn.Function][pod.Name] = info
	}

	return functionsMap, nil
}

func ScaleFunction(ctx context.Context, podManager *pod.Manager, fn function.Function, desiredReplicas int) error {
	assignedPods, err := podManager.GetAssignedAndPending(fn)
	if err != nil {
		return fmt.Errorf("failed to get assigned pods: %w", err)
	}

	currentReplicas := len(assignedPods)
	if currentReplicas == desiredReplicas {
		return nil
	}

	slog.InfoContext(ctx, "scaling function", slog.Int("currentReplicas", currentReplicas), slog.Int("desiredReplicas", desiredReplicas), key.Function.Field(fn))

	if desiredReplicas > currentReplicas {
		// TODO: lock assigning map
		for i := 0; i < desiredReplicas-currentReplicas; i++ {
			pod, err := podManager.Assign(ctx, fn)
			if err != nil {
				return fmt.Errorf("failed to assign pod: %w", err)
			}
			slog.InfoContext(ctx, "assigned pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
		}
	} else {
		slices.SortFunc(assignedPods, func(a, b *v1.Pod) int {
			if a.DeletionTimestamp != nil && b.DeletionTimestamp == nil {
				return 1
			}

			if a.DeletionTimestamp == nil && b.DeletionTimestamp != nil {
				return -1
			}

			instanceA, err := function.FromLabels(a.Labels)
			if err != nil {
				slog.WarnContext(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", a.Name), slog.Any("labels", a.Labels))
				return -1
			}

			instanceB, err := function.FromLabels(b.Labels)
			if err != nil {
				slog.WarnContext(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", b.Name), slog.Any("labels", b.Labels))
				return 1
			}

			return instanceA.AssignedAt.Compare(instanceB.AssignedAt)
		})

		for i := 0; i < currentReplicas-desiredReplicas; i++ {
			pod := assignedPods[i]
			err := podManager.Terminate(ctx, fn, pod)
			if err != nil {
				return fmt.Errorf("failed to delete pod: %w", err)
			}
			slog.InfoContext(ctx, "deleted pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
		}
	}

	return nil
}
