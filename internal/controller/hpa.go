package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
)

type Metric string

const (
	MetricCPU    Metric = "cpu"
	MetricMemory Metric = "memory"
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
func calculateDesiredInstancesForMetric(
	currentInstances int,
	metric Metric,
	instanceMetrics []InstanceMetric,
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

	if len(instancesWithMetrics) == 0 {
		return currentInstances, fmt.Errorf("no metrics available for metric %v", metric)
	}

	var targetUsage int
	var totalUsage int64
	for _, instance := range instancesWithMetrics {
		// accumulate total usage and keep track of target usage (they should all be identical)
		switch metric {
		case MetricCPU:
			targetUsage = instance.Scale.TargetCPUUsageMilli
			totalUsage += *instance.CPUUsage
		case MetricMemory:
			targetUsage = instance.Scale.TargetMemoryUsageMiB
			totalUsage += *instance.MemoryUsage / 1024 / 1024 // convert memory usage from bytes to MiB
		}
	}

	averageUsage := float64(totalUsage) / float64(len(instancesWithMetrics))
	usageRatio := averageUsage / float64(targetUsage)

	log.Debug(context.TODO(), "metric calculation",
		key.Function.Field(instancesWithMetrics[0].Function),
		slog.String("metric", string(metric)),
		key.CurrentInstances.Field(currentInstances),
		slog.Int("instancesWithMetrics", len(instancesWithMetrics)),
		slog.Int("instancesWithoutMetrics", len(instancesWithoutMetrics)),
		slog.Int("notReadyInstances", len(notReadyInstances)),
		slog.Int("targetUsage", targetUsage),
		slog.Int64("totalUsage", totalUsage),
		slog.Float64("currentAverageUsage", averageUsage),
		slog.Float64("usageRatio", usageRatio),
		slog.Float64("tolerance", hpaConfig.Tolerance),
	)

	if math.Abs(1.0-usageRatio) <= hpaConfig.Tolerance {
		return currentInstances, nil
	}

	desiredInstances := int(math.Ceil(float64(currentInstances) * usageRatio))

	if len(instancesWithoutMetrics) > 0 || len(notReadyInstances) > 0 {
		totalInstances := len(instancesWithMetrics) + len(instancesWithoutMetrics) + len(notReadyInstances)

		adjustedTotalUsage := totalUsage
		if desiredInstances < currentInstances {
			adjustedTotalUsage += int64(len(instancesWithoutMetrics)) * int64(targetUsage)
		}

		adjustedAverageUsage := float64(adjustedTotalUsage) / float64(totalInstances)
		adjustedUsageRatio := adjustedAverageUsage / float64(int64(targetUsage))

		if (adjustedUsageRatio > 1.0 && usageRatio < 1.0) ||
			(adjustedUsageRatio < 1.0 && usageRatio > 1.0) ||
			math.Abs(1.0-adjustedUsageRatio) <= hpaConfig.Tolerance {
			// the adjusted usage ratio is within tolerance of the target utilization, return the current instances
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
	hpaConfig Config,
	timestamp time.Time,
) (int, error) {
	maxDesiredInstances := 0
	scaleDownErrors := 0
	scaleDownSuggested := false

	for _, metric := range []Metric{MetricCPU /*, MetricMemory*/} {
		desiredInstances, err := calculateDesiredInstancesForMetric(
			currentInstances,
			metric,
			instanceMetrics,
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
