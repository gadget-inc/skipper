package controller

import (
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/shoenig/test/must"
)

func ptrInt64(val int64) *int64 {
	return &val
}

func TestCalculateDesiredInstancesForMetric(t *testing.T) {
	readyAt := time.Now().Add(-DefaultConfig.InitialReadinessDelay)

	testCases := []struct {
		name              string
		metricName        Metric
		podMetrics        []InstanceMetric
		targetUsage       int
		expectedInstances int
		expectError       bool
	}{
		{
			name:        "scale up",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: ptrInt64(200)},
			},
			expectedInstances: 2,
			expectError:       false,
		},
		{
			name:        "scale down",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: ptrInt64(50)},
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: ptrInt64(50)},
			},
			expectedInstances: 1,
			expectError:       false,
		},
		{
			name:        "no scaling",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: ptrInt64(100)},
			},
			expectedInstances: 1,
			expectError:       false,
		},
		{
			name:        "no scaling (within tolerance)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: ptrInt64(110)},
			},
			expectedInstances: 1,
			expectError:       false,
		},
		{
			name:        "no scaling (missing metric reverses decision)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: ptrInt64(150)}, // causes scale up   (averageUsage = 150, usageRatio = 1.5)
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: nil},           // causes scale down (adjustedAverageUsage = 75, adjustedUsageRatio = 0.75), therefor no scaling
			},
			expectedInstances: 2,
			expectError:       false,
		},
		{
			name:        "no scaling (within initial readiness delay)",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: ptrInt64(150)},    // causes scale up   (averageUsage = 150, usageRatio = 1.5)
				{Instance: &function.Instance{ReadyAt: time.Now()}, CPUUsage: ptrInt64(150)}, // causes scale down (adjustedAverageUsage = 75, adjustedUsageRatio = 0.75), therefor no scaling
			},
			expectedInstances: 2,
			expectError:       false,
		},
		{
			name:        "no metrics available",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: nil},
				{Instance: &function.Instance{ReadyAt: readyAt}, CPUUsage: nil},
			},
			expectedInstances: 2,
			expectError:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, pm := range tc.podMetrics {
				switch tc.metricName {
				case MetricCPU:
					pm.Function.Scale.TargetCPUUsageMilli = tc.targetUsage
				case MetricMemory:
					pm.Function.Scale.TargetMemoryUsageMiB = tc.targetUsage
				}
			}

			instances, err := calculateDesiredInstancesForMetric(tc.metricName, tc.podMetrics, DefaultConfig, time.Now())
			if tc.expectError {
				must.Error(t, err)
				return
			}

			must.NoError(t, err)
			must.Eq(t, tc.expectedInstances, instances)
		})
	}
}
