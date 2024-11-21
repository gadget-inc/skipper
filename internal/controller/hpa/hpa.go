package hpa

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/pod"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Config holds configuration values for the HPA algorithm
type Config struct {
	Tolerance               float64
	InitialReadinessDelay   time.Duration
	CPUInitializationPeriod time.Duration
	DownscaleStabilization  time.Duration
}

var DefaultConfig = Config{
	Tolerance:               0.1,
	InitialReadinessDelay:   30 * time.Second,
	CPUInitializationPeriod: 90 * time.Second,
	DownscaleStabilization:  90 * time.Second,
}

// PodMetricsInfo contains metrics and status information for a pod
type PodMetricsInfo struct {
	*v1.Pod
	CPUUsage          *int64
	MemoryUsage       *int64
	Ready             bool
	AssignedAt        time.Time
	DeletionTimestamp *metav1.Time
}

// Recommendation represents a scaling recommendation
type Recommendation struct {
	Replicas  int
	Timestamp time.Time
}

// StabilizationWindow holds scaling recommendations within a time window
type StabilizationWindow struct {
	Window          time.Duration
	Recommendations []Recommendation
}

// RecordRecommendation adds a new recommendation and prunes old ones
func (sw *StabilizationWindow) RecordRecommendation(replicas int, timestamp time.Time) {
	sw.Recommendations = append(sw.Recommendations, Recommendation{
		Replicas:  replicas,
		Timestamp: timestamp,
	})

	// Remove old recommendations
	cutoff := timestamp.Add(-sw.Window)
	var newRecommendations []Recommendation
	for _, rec := range sw.Recommendations {
		if rec.Timestamp.After(cutoff) {
			newRecommendations = append(newRecommendations, rec)
		}
	}
	sw.Recommendations = newRecommendations
}

// GetMaxRecommendation returns the maximum recommended replicas in the window
func (sw *StabilizationWindow) GetMaxRecommendation() int {
	var maxReplicas int
	for _, rec := range sw.Recommendations {
		if rec.Replicas > maxReplicas {
			maxReplicas = rec.Replicas
		}
	}
	return maxReplicas
}

// calculateDesiredReplicasForMetric computes desired replicas based on a single metric
func calculateDesiredReplicasForMetric(
	currentReplicas int,
	metricName string,
	podMetrics map[string]PodMetricsInfo,
	targetUtilization int64,
	hpaConfig Config,
	timestamp time.Time,
) (int, error) {
	includedPods := []PodMetricsInfo{}
	missingMetricsPods := []PodMetricsInfo{}
	notYetReadyPods := []PodMetricsInfo{}

	ctx := log.With(
		context.Background(),
		slog.String("metric", metricName),
		slog.Int64("targetUtilization", targetUtilization),
		slog.Int("currentReplicas", currentReplicas),
	)

	for _, pod := range podMetrics {
		if pod.DeletionTimestamp != nil && !pod.DeletionTimestamp.IsZero() {
			log.Trace(ctx, "skipping pod with deletion timestamp", key.Pod.Field(pod.Pod), slog.Time("deletionTimestamp", pod.DeletionTimestamp.Time))
			continue
		}

		var usage *int64
		switch metricName {
		case "cpu":
			usage = pod.CPUUsage
		case "memory":
			usage = pod.MemoryUsage
		default:
			return 0, fmt.Errorf("unsupported metric: %s", metricName)
		}

		if metricName == "cpu" && !pod.Ready {
			timeSinceStart := timestamp.Sub(pod.AssignedAt)
			if timeSinceStart < hpaConfig.InitialReadinessDelay {
				notYetReadyPods = append(notYetReadyPods, pod)
				continue
			}
		}

		if usage == nil {
			missingMetricsPods = append(missingMetricsPods, pod)
		} else {
			includedPods = append(includedPods, pod)
		}
	}

	totalUsage := int64(0)
	for _, pod := range includedPods {
		var usage int64
		switch metricName {
		case "cpu":
			usage = *pod.CPUUsage
		case "memory":
			// Convert memory usage from bytes to MB
			usage = *pod.MemoryUsage / 1024 / 1024
		}
		totalUsage += usage
	}

	numIncludedPods := len(includedPods)
	if numIncludedPods == 0 {
		return currentReplicas, fmt.Errorf("no metrics available for metric %s", metricName)
	}

	currentAverageUsage := float64(totalUsage) / float64(numIncludedPods)
	usageRatio := currentAverageUsage / float64(targetUtilization)

	ctx = log.With(
		ctx,
		slog.Int("includedPods", len(includedPods)),
		slog.Int("missingMetricsPods", len(missingMetricsPods)),
		slog.Int("notYetReadyPods", len(notYetReadyPods)),
		slog.Int64("totalUsage", totalUsage),
		slog.Float64("currentAverageUsage", currentAverageUsage),
		slog.Float64("usageRatio", usageRatio),
	)

	log.Trace(ctx, "calculated usage ratio")

	if math.Abs(1.0-usageRatio) <= hpaConfig.Tolerance {
		log.Trace(ctx, "usage ratio within tolerance of target utilization")
		return currentReplicas, nil
	}

	desiredReplicas := int(math.Ceil(float64(currentReplicas) * usageRatio))
	log.Trace(ctx, "calculated desired replicas", slog.Int("desiredReplicas", desiredReplicas))

	if len(missingMetricsPods) > 0 || len(notYetReadyPods) > 0 {
		totalPods := numIncludedPods + len(missingMetricsPods) + len(notYetReadyPods)
		adjustedTotalUsage := totalUsage

		ctx = log.With(ctx, slog.Int("totalPods", totalPods))

		if desiredReplicas < currentReplicas {
			adjustedTotalUsage += int64(len(missingMetricsPods)) * targetUtilization
			log.Trace(ctx, "adjusted total usage for missing metrics", slog.Int64("adjustedTotalUsage", adjustedTotalUsage))
		}

		adjustedAverageUsage := float64(adjustedTotalUsage) / float64(totalPods)
		adjustedUsageRatio := adjustedAverageUsage / float64(targetUtilization)

		ctx = log.With(ctx,
			slog.Int64("adjustedTotalUsage", adjustedTotalUsage),
			slog.Float64("adjustedAverageUsage", adjustedAverageUsage),
			slog.Float64("adjustedUsageRatio", adjustedUsageRatio),
		)

		log.Trace(ctx, "calculated adjusted usage ratio")

		if (adjustedUsageRatio > 1.0 && usageRatio < 1.0) ||
			(adjustedUsageRatio < 1.0 && usageRatio > 1.0) ||
			math.Abs(1.0-adjustedUsageRatio) <= hpaConfig.Tolerance {
			// If the adjusted usage ratio is within tolerance of the target utilization, return the current replicas
			log.Trace(ctx, "adjusted usage ratio within tolerance of target utilization")
			return currentReplicas, nil
		}

		desiredReplicas = int(math.Ceil(float64(currentReplicas) * adjustedUsageRatio))
		log.Trace(ctx, "calculated adjusted desired replicas", slog.Int("desiredReplicas", desiredReplicas))
	}

	return desiredReplicas, nil
}

// CalculateDesiredReplicas computes desired replicas based on multiple metrics
func CalculateDesiredReplicas(
	currentReplicas int,
	podMetrics map[string]PodMetricsInfo,
	targetCPUUtilization int64,
	targetMemoryUtilization int64,
	hpaConfig Config,
	timestamp time.Time,
) (int, error) {
	maxDesiredReplicas := 0
	metricsToCalculate := []struct {
		name              string
		targetUtilization int64
	}{
		{"cpu", targetCPUUtilization},
		// {"memory", targetMemoryUtilization},
	}

	scaleDownErrors := 0
	scaleDownSuggested := false

	for _, metric := range metricsToCalculate {
		desiredReplicas, err := calculateDesiredReplicasForMetric(
			currentReplicas,
			metric.name,
			podMetrics,
			metric.targetUtilization,
			hpaConfig,
			timestamp,
		)
		if err != nil {
			log.Warn(context.TODO(), "failed to calculate desired replicas for metric", key.Error.Field(err), slog.String("metric", metric.name))
			if desiredReplicas < currentReplicas {
				scaleDownErrors++
			}
			continue
		}

		if desiredReplicas > maxDesiredReplicas {
			maxDesiredReplicas = desiredReplicas
		}

		if desiredReplicas < currentReplicas {
			scaleDownSuggested = true
		}
	}

	if maxDesiredReplicas == 0 {
		return currentReplicas, fmt.Errorf("no metrics available")
	}

	if scaleDownSuggested && scaleDownErrors > 0 {
		return currentReplicas, nil
	}

	return maxDesiredReplicas, nil
}

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
		fn, err := function.FromPod(pod)
		if err != nil {
			log.Warn(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", pod.Name), slog.Any("labels", pod.Labels))
			continue
		}

		info := PodMetricsInfo{
			Pod:               pod,
			Ready:             false,
			AssignedAt:        fn.AssignedAt,
			DeletionTimestamp: pod.DeletionTimestamp,
		}

		if !fn.ReadyAt.IsZero() {
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

	log.Info(ctx, "scaling function", slog.Int("currentReplicas", currentReplicas), slog.Int("desiredReplicas", desiredReplicas), key.Function.Field(fn))

	if desiredReplicas > currentReplicas {
		// TODO: lock assigning map
		for i := 0; i < desiredReplicas-currentReplicas; i++ {
			pod, err := podManager.Assign(ctx, fn)
			if err != nil {
				return fmt.Errorf("failed to assign pod: %w", err)
			}
			log.Info(ctx, "assigned pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
		}
	} else {
		slices.SortFunc(assignedPods, func(a, b *v1.Pod) int {
			if a.DeletionTimestamp != nil && b.DeletionTimestamp == nil {
				return 1
			}

			if a.DeletionTimestamp == nil && b.DeletionTimestamp != nil {
				return -1
			}

			instanceA, err := function.FromPod(a)
			if err != nil {
				log.Warn(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", a.Name), slog.Any("labels", a.Labels))
				return -1
			}

			instanceB, err := function.FromPod(b)
			if err != nil {
				log.Warn(ctx, "failed to get function from labels", key.Error.Field(err), slog.String("pod", b.Name), slog.Any("labels", b.Labels))
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
			log.Info(ctx, "deleted pod", slog.Any("pod", pod.Name), key.Function.Field(fn))
		}
	}

	return nil
}
