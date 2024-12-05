package controller

import (
	"fmt"
	"math"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
)

type Metric int

const (
	MetricCPU Metric = iota
	MetricMemory
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

// InstanceMetric contains metrics and status information for a pod
type InstanceMetric struct {
	*function.Instance
	CPUUsage    *int64
	MemoryUsage *int64
}

// Recommendation represents a scaling recommendation
type Recommendation struct {
	Instances int
	Timestamp time.Time
}

// StabilizationWindow holds scaling recommendations within a time window
type StabilizationWindow struct {
	Window          time.Duration
	Recommendations []Recommendation
}

// RecordRecommendation adds a new recommendation and prunes old ones
func (sw *StabilizationWindow) RecordRecommendation(instances int, timestamp time.Time) {
	sw.Recommendations = append(sw.Recommendations, Recommendation{
		Instances: instances,
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

// GetMaxRecommendation returns the maximum recommended instances in the window
func (sw *StabilizationWindow) GetMaxRecommendation() int {
	var maxInstances int
	for _, rec := range sw.Recommendations {
		if rec.Instances > maxInstances {
			maxInstances = rec.Instances
		}
	}
	return maxInstances
}

// calculateDesiredInstancesForMetric computes desired instances based on a single metric
func calculateDesiredInstancesForMetric(
	currentInstances int,
	metric Metric,
	instanceMetrics []InstanceMetric,
	targetUtilization int64,
	hpaConfig Config,
	timestamp time.Time,
) (int, error) {
	var instancesWithMetrics []InstanceMetric
	var instancesWithoutMetrics []InstanceMetric
	var notReadyInstances []InstanceMetric

	for _, instance := range instanceMetrics {
		var usage *int64
		switch metric {
		case MetricCPU:
			usage = instance.CPUUsage
		case MetricMemory:
			usage = instance.MemoryUsage
		default:
			return 0, fmt.Errorf("unsupported metric: %v", metric)
		}

		if metric == MetricCPU && instance.ReadyAt.IsZero() {
			if timestamp.Sub(instance.AssignedAt) < hpaConfig.InitialReadinessDelay {
				notReadyInstances = append(notReadyInstances, instance)
				continue
			}
		}

		if usage == nil {
			instancesWithoutMetrics = append(instancesWithoutMetrics, instance)
		} else {
			instancesWithMetrics = append(instancesWithMetrics, instance)
		}
	}

	var totalUsage int64
	for _, instance := range instancesWithMetrics {
		var usage int64
		switch metric {
		case MetricCPU:
			usage = *instance.CPUUsage
		case MetricMemory:
			usage = *instance.MemoryUsage / 1024 / 1024 // convert memory usage from bytes to mb
		}
		totalUsage += usage
	}

	if len(instancesWithMetrics) == 0 {
		return currentInstances, fmt.Errorf("no metrics available for metric %v", metric)
	}

	currentAverageUsage := float64(totalUsage) / float64(len(instancesWithMetrics))
	usageRatio := currentAverageUsage / float64(targetUtilization)

	if math.Abs(1.0-usageRatio) <= hpaConfig.Tolerance {
		return currentInstances, nil
	}

	desiredInstances := int(math.Ceil(float64(currentInstances) * usageRatio))

	if len(instancesWithoutMetrics) > 0 || len(notReadyInstances) > 0 {
		totalInstances := len(instancesWithMetrics) + len(instancesWithoutMetrics) + len(notReadyInstances)

		adjustedTotalUsage := totalUsage
		if desiredInstances < currentInstances {
			adjustedTotalUsage += int64(len(instancesWithoutMetrics)) * targetUtilization
		}

		adjustedAverageUsage := float64(adjustedTotalUsage) / float64(totalInstances)
		adjustedUsageRatio := adjustedAverageUsage / float64(targetUtilization)

		if (adjustedUsageRatio > 1.0 && usageRatio < 1.0) ||
			(adjustedUsageRatio < 1.0 && usageRatio > 1.0) ||
			math.Abs(1.0-adjustedUsageRatio) <= hpaConfig.Tolerance {
			// If the adjusted usage ratio is within tolerance of the target utilization, return the current instances
			return currentInstances, nil
		}

		desiredInstances = int(math.Ceil(float64(currentInstances) * adjustedUsageRatio))
	}

	return desiredInstances, nil
}

// calculateDesiredInstances computes desired instances based on multiple metrics
func calculateDesiredInstances(
	currentInstances int,
	instanceMetrics []InstanceMetric,
	targetCPUUtilization int64,
	targetMemoryUtilization int64,
	hpaConfig Config,
	timestamp time.Time,
) (int, error) {
	maxDesiredInstances := 0
	metricsToCalculate := []struct {
		name              Metric
		targetUtilization int64
	}{
		{MetricCPU, targetCPUUtilization},
		// {MetricMemory, targetMemoryUtilization},
	}

	scaleDownErrors := 0
	scaleDownSuggested := false

	for _, metric := range metricsToCalculate {
		desiredInstances, err := calculateDesiredInstancesForMetric(
			currentInstances,
			metric.name,
			instanceMetrics,
			metric.targetUtilization,
			hpaConfig,
			timestamp,
		)
		if err != nil {
			if desiredInstances < currentInstances {
				scaleDownErrors++
			}
			continue
		}

		if desiredInstances > maxDesiredInstances {
			maxDesiredInstances = desiredInstances
		}

		if desiredInstances < currentInstances {
			scaleDownSuggested = true
		}
	}

	if maxDesiredInstances == 0 {
		return currentInstances, fmt.Errorf("no metrics available")
	}

	if scaleDownSuggested && scaleDownErrors > 0 {
		return currentInstances, nil
	}

	return maxDesiredInstances, nil
}
