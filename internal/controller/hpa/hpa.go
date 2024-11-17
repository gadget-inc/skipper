package hpa

import (
	"context"
	"fmt"
	"math"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
	defaultTolerance               = 0.1
	defaultInitialReadinessDelay   = 30 * time.Second
	defaultCPUInitializationPeriod = 5 * time.Minute
	defaultDownscaleStabilization  = 5 * time.Minute
)

// HPAConfig holds configuration values for the HPA algorithm
type HPAConfig struct {
	Tolerance               float64
	InitialReadinessDelay   time.Duration
	CPUInitializationPeriod time.Duration
	DownscaleStabilization  time.Duration
}

// PodMetricsInfo contains metrics and status information for a pod
type PodMetricsInfo struct {
	CPUUsage          *int64
	MemoryUsage       *int64
	Ready             bool
	StartTime         time.Time
	DeletionTimestamp *metav1.Time
}

// Recommendation represents a scaling recommendation
type Recommendation struct {
	Replicas  int32
	Timestamp time.Time
}

// StabilizationWindow holds scaling recommendations within a time window
type StabilizationWindow struct {
	Window          time.Duration
	Recommendations []Recommendation
}

// RecordRecommendation adds a new recommendation and prunes old ones
func (sw *StabilizationWindow) RecordRecommendation(replicas int32, timestamp time.Time) {
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
func (sw *StabilizationWindow) GetMaxRecommendation() int32 {
	var maxReplicas int32
	for _, rec := range sw.Recommendations {
		if rec.Replicas > maxReplicas {
			maxReplicas = rec.Replicas
		}
	}
	return maxReplicas
}

// getPodMetrics fetches metrics and status for pods matching the selector
func getPodMetrics(clientset kubernetes.Interface, metricsClientset metricsclientset.Interface, namespace, selector string) (map[string]PodMetricsInfo, error) {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}

	podMetrics := make(map[string]PodMetricsInfo)
	podMetricsList, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod metrics: %v", err)
	}

	metricsMap := make(map[string]metricsv1beta1.PodMetrics)
	for _, m := range podMetricsList.Items {
		metricsMap[m.Name] = m
	}

	for _, pod := range pods.Items {
		info := PodMetricsInfo{
			Ready:             false,
			StartTime:         pod.Status.StartTime.Time,
			DeletionTimestamp: pod.DeletionTimestamp,
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
				info.Ready = true
				break
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

		podMetrics[pod.Name] = info
	}

	return podMetrics, nil
}

// calculateDesiredReplicasForMetric computes desired replicas based on a single metric
func calculateDesiredReplicasForMetric(
	currentReplicas int32,
	metricName string,
	podMetrics map[string]PodMetricsInfo,
	targetUtilization int64,
	hpaConfig HPAConfig,
	timestamp time.Time,
) (int32, error) {
	includedPods := []PodMetricsInfo{}
	missingMetricsPods := []PodMetricsInfo{}
	notYetReadyPods := []PodMetricsInfo{}

	for _, pod := range podMetrics {
		if pod.DeletionTimestamp != nil && !pod.DeletionTimestamp.IsZero() {
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
			timeSinceStart := timestamp.Sub(pod.StartTime)
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
			usage = *pod.MemoryUsage
		}
		totalUsage += usage
	}

	numIncludedPods := len(includedPods)
	if numIncludedPods == 0 {
		return currentReplicas, fmt.Errorf("no metrics available for metric %s", metricName)
	}

	currentAverageUsage := float64(totalUsage) / float64(numIncludedPods)
	usageRatio := currentAverageUsage / float64(targetUtilization)

	if math.Abs(1.0-usageRatio) <= hpaConfig.Tolerance {
		return currentReplicas, nil
	}

	desiredReplicas := int32(math.Ceil(float64(currentReplicas) * usageRatio))

	if len(missingMetricsPods) > 0 || len(notYetReadyPods) > 0 {
		totalPods := numIncludedPods + len(missingMetricsPods) + len(notYetReadyPods)
		adjustedTotalUsage := totalUsage

		if desiredReplicas < currentReplicas {
			adjustedTotalUsage += int64(len(missingMetricsPods)) * targetUtilization
		}

		adjustedAverageUsage := float64(adjustedTotalUsage) / float64(totalPods)
		adjustedUsageRatio := adjustedAverageUsage / float64(targetUtilization)

		if (adjustedUsageRatio > 1.0 && usageRatio < 1.0) ||
			(adjustedUsageRatio < 1.0 && usageRatio > 1.0) ||
			math.Abs(1.0-adjustedUsageRatio) <= hpaConfig.Tolerance {
			return currentReplicas, nil
		}

		desiredReplicas = int32(math.Ceil(float64(currentReplicas) * adjustedUsageRatio))
	}

	return desiredReplicas, nil
}

// calculateDesiredReplicas computes desired replicas based on multiple metrics
func calculateDesiredReplicas(
	currentReplicas int32,
	podMetrics map[string]PodMetricsInfo,
	targetCPUUtilization int64,
	targetMemoryUtilization int64,
	hpaConfig HPAConfig,
	timestamp time.Time,
) (int32, error) {
	maxDesiredReplicas := currentReplicas
	metricsToCalculate := []struct {
		name              string
		targetUtilization int64
	}{
		{"cpu", targetCPUUtilization},
		{"memory", targetMemoryUtilization},
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

	if scaleDownSuggested && scaleDownErrors > 0 {
		return currentReplicas, nil
	}

	return maxDesiredReplicas, nil
}

// autoscale adjusts the deployment replicas based on calculated desired replicas
func autoscale(
	clientset *kubernetes.Clientset,
	namespace, deploymentName string,
	currentReplicas int32,
	podMetrics map[string]PodMetricsInfo,
	targetCPUUtilization int64,
	targetMemoryUtilization int64,
	hpaConfig HPAConfig,
	stabilizationWindow *StabilizationWindow,
	timestamp time.Time,
) error {
	desiredReplicas, err := calculateDesiredReplicas(
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
func scaleDeployment(clientset *kubernetes.Clientset, namespace, deploymentName string, desiredReplicas int32) error {
	deploymentClient := clientset.AppsV1().Deployments(namespace)
	deployment, err := deploymentClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %v", err)
	}

	deployment.Spec.Replicas = &desiredReplicas
	_, err = deploymentClient.Update(context.TODO(), deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment: %v", err)
	}

	return nil
}

// getCurrentReplicas fetches the current number of replicas in the deployment
func getCurrentReplicas(clientset *kubernetes.Clientset, namespace, deploymentName string) (int32, error) {
	deploymentClient := clientset.AppsV1().Deployments(namespace)
	deployment, err := deploymentClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get deployment: %v", err)
	}
	return *deployment.Spec.Replicas, nil
}

func main() {
	kubeconfig := "/path/to/kubeconfig"
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	metricsClientset, err := metricsclientset.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	hpaConfig := HPAConfig{
		Tolerance:               defaultTolerance,
		InitialReadinessDelay:   defaultInitialReadinessDelay,
		CPUInitializationPeriod: defaultCPUInitializationPeriod,
		DownscaleStabilization:  defaultDownscaleStabilization,
	}

	namespace := "default"
	deploymentName := "my-deployment"
	podLabelSelector := "app=my-app"
	targetCPUUtilization := int64(100)    // in millicores
	targetMemoryUtilization := int64(500) // in MB

	stabilizationWindow := &StabilizationWindow{
		Window: hpaConfig.DownscaleStabilization,
	}

	ticker := time.NewTicker(15 * time.Second)
	for {
		select {
		case <-ticker.C:
			timestamp := time.Now()
			currentReplicas, err := getCurrentReplicas(clientset, namespace, deploymentName)
			if err != nil {
				fmt.Printf("Error getting current replicas: %v\n", err)
				continue
			}

			podMetrics, err := getPodMetrics(clientset, metricsClientset, namespace, podLabelSelector)
			if err != nil {
				fmt.Printf("Error getting pod metrics: %v\n", err)
				continue
			}

			err = autoscale(
				clientset,
				namespace,
				deploymentName,
				currentReplicas,
				podMetrics,
				targetCPUUtilization,
				targetMemoryUtilization,
				hpaConfig,
				stabilizationWindow,
				timestamp,
			)
			if err != nil {
				fmt.Printf("Error in autoscaling: %v\n", err)
			}
		}
	}
}
