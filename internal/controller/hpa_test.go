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
	now := time.Now()

	testCases := []struct {
		name              string
		metricName        Metric
		podMetrics        []InstanceMetric
		targetUsage       int64
		expectedInstances int
		expectError       bool
	}{
		{
			name:        "Scaling up due to high CPU utilization",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(200)},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(200)},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(200)},
			},
			expectedInstances: 6,
			expectError:       false,
		},
		{
			name:        "Scaling down due to low CPU utilization",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
			},
			expectedInstances: 3,
			expectError:       false,
		},
		{
			name:        "No scaling when within tolerance",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(110)},
			},
			expectedInstances: 1,
			expectError:       false,
		},
		// - Included Pods: Pods with available metrics (Pod1, 150m CPU usage).
		// - Total Usage = 150m; Num Included Pods = 1; Average Usage = 150m.
		// - Usage Ratio = 1.5 (> 1.1 tolerance), suggesting scale-up.
		// - Missing Metrics Pods (Pod2) are assumed to consume 0% of target for scale-up.
		// - Adjusted Usage: 150m total over 2 pods = 75m; Adjusted Ratio = 0.75 (< target).
		// - Reassessed ratio suggests scale-down, reversing initial direction, so no scaling occurs.
		{
			name:        "Handling missing metrics when scaling up",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(150)},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: nil},
			},
			expectedInstances: 2,
			expectError:       false,
		},
		{
			name:        "Handling not-yet-ready pods when scaling up",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(150)},
				{Instance: &function.Instance{AssignedAt: now.Add(-10 * time.Second)}, CPUUsage: ptrInt64(150)},
			},
			expectedInstances: 2,
			expectError:       false,
		},
		{
			name:        "No metrics available",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []InstanceMetric{
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: nil},
				{Instance: &function.Instance{ReadyAt: now}, CPUUsage: nil},
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
					pm.Function.Scale.TargetCPUUsageMilli = int(tc.targetUsage)
				case MetricMemory:
					pm.Function.Scale.TargetMemoryUsageMiB = int(tc.targetUsage)
				}
			}

			instances, err := calculateDesiredInstancesForMetric(tc.metricName, tc.podMetrics, DefaultConfig, now)
			if tc.expectError {
				must.Error(t, err)
				return
			}

			must.NoError(t, err)
			must.Eq(t, tc.expectedInstances, instances)
		})
	}
}
