package controller

import (
	"fmt"
	"math"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
)

type Metric string

const (
	MetricCPU    Metric = "cpu"
	MetricMemory Metric = "memory"
)

// Config holds configuration values for the HPA algorithm
type Config struct {
	Tolerance              float64
	InitialReadinessDelay  time.Duration
	DownscaleStabilization time.Duration
}

var DefaultConfig = Config{
	Tolerance:              0.1,
	InitialReadinessDelay:  30 * time.Second,
	DownscaleStabilization: 90 * time.Second,
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
func (sw *StabilizationWindow) RecordRecommendation(desiredInstances int, timestamp time.Time) {
	sw.Recommendations = append(sw.Recommendations, Recommendation{
		Instances: desiredInstances,
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
func calculateDesiredInstancesForMetric(metric Metric, instanceMetrics []InstanceMetric, hpaConfig Config, timestamp time.Time) (int, error) {
	currentInstances := len(instanceMetrics)
	var instancesWithMetrics []InstanceMetric
	var instancesWithoutMetrics []InstanceMetric

	for _, instance := range instanceMetrics {
		var usage *int64
		switch metric {
		case MetricCPU:
			usage = instance.CPUUsage
		case MetricMemory:
			usage = instance.MemoryUsage
		default:
			return currentInstances, fmt.Errorf("unsupported metric: %v", metric)
		}

		if metric == MetricCPU && (instance.ReadyAt.IsZero() || timestamp.Sub(instance.ReadyAt) <= hpaConfig.InitialReadinessDelay) {
			// ignore CPU metrics for pods that have been ready for less than the initial readiness delay
			instancesWithoutMetrics = append(instancesWithoutMetrics, instance)
			continue
		}

		if usage == nil {
			instancesWithoutMetrics = append(instancesWithoutMetrics, instance)
		} else {
			instancesWithMetrics = append(instancesWithMetrics, instance)
		}
	}

	if len(instancesWithMetrics) == 0 {
		return currentInstances, fmt.Errorf("no metrics available for metric %v", metric)
	}

	var targetUsage int
	var totalUsage int
	for _, instance := range instancesWithMetrics {
		// accumulate total usage and keep track of target usage (they should all be identical)
		switch metric {
		case MetricCPU:
			targetUsage = instance.Scale.TargetCPUUsageMilli
			totalUsage += int(*instance.CPUUsage)
		case MetricMemory:
			targetUsage = instance.Scale.TargetMemoryUsageMiB
			totalUsage += int(*instance.MemoryUsage / 1024 / 1024) // convert memory usage from bytes to MiB
		}
	}

	averageUsage := float64(totalUsage) / float64(len(instancesWithMetrics))
	usageRatio := averageUsage / float64(targetUsage)
	usageDiscrepancy := math.Abs(1.0 - usageRatio)
	desiredInstances := int(math.Ceil(float64(currentInstances) * usageRatio))

	if usageDiscrepancy <= hpaConfig.Tolerance+1e-10 { // add a small epsilon to avoid floating point errors
		// the average usage is within tolerance of the target utilization, so we should not scale
		return currentInstances, nil
	}

	if len(instancesWithoutMetrics) > 0 {
		adjustedTotalUsage := totalUsage
		if desiredInstances < currentInstances {
			// we wanted to scale down, so we assume that instances without metrics are consuming 100% of target usage
			adjustedTotalUsage += len(instancesWithoutMetrics) * targetUsage
		} else {
			// we wanted to scale up, so we assume that instances without metrics are consuming 0% of target usage
		}

		adjustedAverageUsage := float64(adjustedTotalUsage) / float64(currentInstances)
		adjustedUsageRatio := adjustedAverageUsage / float64(targetUsage)

		if (adjustedUsageRatio > 1.0 && usageRatio < 1.0) ||
			(adjustedUsageRatio < 1.0 && usageRatio > 1.0) ||
			math.Abs(1.0-adjustedUsageRatio) <= hpaConfig.Tolerance+1e-10 {
			// the adjusted usage ratio is the opposite of the original
			// usage ratio, or the adjusted usage ratio is within
			// tolerance of the target utilization. either way, we
			// should noop and not scale
			return currentInstances, nil
		}

		desiredInstances = int(math.Ceil(float64(currentInstances) * adjustedUsageRatio))
	}

	return desiredInstances, nil
}

// calculateDesiredInstances computes desired instances based on multiple metrics
func calculateDesiredInstances(instanceMetrics []InstanceMetric, hpaConfig Config, timestamp time.Time) (int, error) {
	currentInstances := len(instanceMetrics)
	maxDesiredInstances := 0
	scaleDownErrors := 0
	scaleDownSuggested := false

	for _, metric := range []Metric{MetricCPU /*, MetricMemory*/} {
		desiredInstances, err := calculateDesiredInstancesForMetric(metric, instanceMetrics, hpaConfig, timestamp)
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
