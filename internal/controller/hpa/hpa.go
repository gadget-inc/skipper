package hpa

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
	Function          function.Instance
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

	logger := slog.With(slog.String("metric", metricName), slog.Int64("targetUtilization", targetUtilization), slog.Int("currentReplicas", currentReplicas))
	for _, pod := range podMetrics {
		if pod.DeletionTimestamp != nil && !pod.DeletionTimestamp.IsZero() {
			logger.Debug("skipping pod with deletion timestamp", slog.String("pod", pod.Name), slog.Time("deletionTimestamp", pod.DeletionTimestamp.Time))
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

	logger = logger.With(
		slog.Int("includedPods", len(includedPods)),
		slog.Int("missingMetricsPods", len(missingMetricsPods)),
		slog.Int("notYetReadyPods", len(notYetReadyPods)),
		slog.Int64("totalUsage", totalUsage),
		slog.Float64("currentAverageUsage", currentAverageUsage),
		slog.Float64("usageRatio", usageRatio))

	logger.Debug("calculated usage ratio")

	if math.Abs(1.0-usageRatio) <= hpaConfig.Tolerance {
		logger.Debug("usage ratio within tolerance of target utilization")
		return currentReplicas, nil
	}

	desiredReplicas := int(math.Ceil(float64(currentReplicas) * usageRatio))
	logger.Debug("calculated desired replicas", slog.Int("desiredReplicas", desiredReplicas))

	if len(missingMetricsPods) > 0 || len(notYetReadyPods) > 0 {
		totalPods := numIncludedPods + len(missingMetricsPods) + len(notYetReadyPods)
		adjustedTotalUsage := totalUsage

		logger = logger.With(slog.Int("totalPods", totalPods))

		if desiredReplicas < currentReplicas {
			adjustedTotalUsage += int64(len(missingMetricsPods)) * targetUtilization
			logger.Debug("adjusted total usage for missing metrics", slog.Int64("adjustedTotalUsage", adjustedTotalUsage))
		}

		adjustedAverageUsage := float64(adjustedTotalUsage) / float64(totalPods)
		adjustedUsageRatio := adjustedAverageUsage / float64(targetUtilization)

		logger = logger.With(
			slog.Int64("adjustedTotalUsage", adjustedTotalUsage),
			slog.Float64("adjustedAverageUsage", adjustedAverageUsage),
			slog.Float64("adjustedUsageRatio", adjustedUsageRatio))

		logger.Debug("calculated adjusted usage ratio")

		if (adjustedUsageRatio > 1.0 && usageRatio < 1.0) ||
			(adjustedUsageRatio < 1.0 && usageRatio > 1.0) ||
			math.Abs(1.0-adjustedUsageRatio) <= hpaConfig.Tolerance {
			// If the adjusted usage ratio is within tolerance of the target utilization, return the current replicas
			logger.Debug("adjusted usage ratio within tolerance of target utilization")
			return currentReplicas, nil
		}

		desiredReplicas = int(math.Ceil(float64(currentReplicas) * adjustedUsageRatio))
		logger.Debug("calculated adjusted desired replicas", slog.Int("desiredReplicas", desiredReplicas))
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
			slog.Warn("failed to calculate desired replicas for metric", key.Error.Field(err), slog.String("metric", metric.name))
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

// autoscale adjusts the deployment replicas based on calculated desired replicas
func autoscale(
	clientset *kubernetes.Clientset,
	namespace, deploymentName string,
	currentReplicas int,
	podMetrics map[string]PodMetricsInfo,
	targetCPUUtilization int64,
	targetMemoryUtilization int64,
	hpaConfig Config,
	stabilizationWindow *StabilizationWindow,
	timestamp time.Time,
) error {
	desiredReplicas, err := CalculateDesiredReplicas(
		currentReplicas,
		podMetrics,
		targetCPUUtilization,
		targetMemoryUtilization,
		hpaConfig,
		timestamp,
	)
	if err != nil {
		return err
	}

	stabilizationWindow.RecordRecommendation(desiredReplicas, timestamp)

	if desiredReplicas < currentReplicas {
		maxRecommendedReplicas := stabilizationWindow.GetMaxRecommendation()
		if maxRecommendedReplicas < currentReplicas {
			desiredReplicas = maxRecommendedReplicas
		} else {
			desiredReplicas = currentReplicas
		}
	}

	if desiredReplicas != currentReplicas {
		err := scaleDeployment(clientset, namespace, deploymentName, desiredReplicas)
		if err != nil {
			return fmt.Errorf("failed to scale deployment: %v", err)
		}
	}

	return nil
}

// scaleDeployment updates the deployment to the desired number of replicas
func scaleDeployment(clientset *kubernetes.Clientset, namespace, deploymentName string, desiredReplicas int) error {
	deploymentClient := clientset.AppsV1().Deployments(namespace)
	deployment, err := deploymentClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %v", err)
	}

	desiredReplicasInt32 := int32(desiredReplicas)
	deployment.Spec.Replicas = &desiredReplicasInt32
	_, err = deploymentClient.Update(context.TODO(), deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment: %v", err)
	}

	return nil
}

// getCurrentReplicas fetches the current number of replicas in the deployment
func getCurrentReplicas(clientset *kubernetes.Clientset, namespace, deploymentName string) (int, error) {
	deploymentClient := clientset.AppsV1().Deployments(namespace)
	deployment, err := deploymentClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get deployment: %v", err)
	}
	return int(*deployment.Spec.Replicas), nil
}

// func main() {
// 	kubeconfig := "/path/to/kubeconfig"
// 	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
// 	if err != nil {
// 		panic(err.Error())
// 	}

// 	clientset, err := kubernetes.NewForConfig(config)
// 	if err != nil {
// 		panic(err.Error())
// 	}

// 	metricsClientset, err := metricsclientset.NewForConfig(config)
// 	if err != nil {
// 		panic(err.Error())
// 	}

// 	namespace := "default"
// 	deploymentName := "my-deployment"
// 	podLabelSelector := "app=my-app"
// 	targetCPUUtilization := int64(100)    // in millicores
// 	targetMemoryUtilization := int64(500) // in MB

// 	stabilizationWindow := &StabilizationWindow{
// 		Window: DefaultConfig.DownscaleStabilization,
// 	}

// 	ticker := time.NewTicker(15 * time.Second)
// 	for {
// 		select {
// 		case <-ticker.C:
// 			timestamp := time.Now()
// 			currentReplicas, err := getCurrentReplicas(clientset, namespace, deploymentName)
// 			if err != nil {
// 				fmt.Printf("Error getting current replicas: %v\n", err)
// 				continue
// 			}

// 			podMetrics, err := getPodMetrics(clientset, metricsClientset, namespace, podLabelSelector)
// 			if err != nil {
// 				fmt.Printf("Error getting pod metrics: %v\n", err)
// 				continue
// 			}

// 			err = autoscale(
// 				clientset,
// 				namespace,
// 				deploymentName,
// 				currentReplicas,
// 				podMetrics,
// 				targetCPUUtilization,
// 				targetMemoryUtilization,
// 				DefaultConfig,
// 				stabilizationWindow,
// 				timestamp,
// 			)
// 			if err != nil {
// 				fmt.Printf("Error in autoscaling: %v\n", err)
// 			}
// 		}
// 	}
// }
