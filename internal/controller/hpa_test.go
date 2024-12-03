package controller

import (
	"testing"
	"time"

	"github.com/gadget-inc/fusion/internal/function"
)

func ptrInt64(val int64) *int64 {
	return &val
}

func TestCalculateDesiredReplicasForMetric(t *testing.T) {
	now := time.Now()
	hpaConfig := Config{
		Tolerance:               0.1,
		InitialReadinessDelay:   30 * time.Second,
		CPUInitializationPeriod: 5 * time.Minute,
	}

	tests := []struct {
		name              string
		currentReplicas   int
		metricName        Metric
		podMetrics        []InstanceMetric
		targetUtilization int64
		expectedReplicas  int
		expectError       bool
	}{
		{
			name:              "Scaling up due to high CPU utilization",
			currentReplicas:   3,
			metricName:        MetricCPU,
			targetUtilization: 100,
			podMetrics: []InstanceMetric{
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(200)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(200)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(200)},
			},
			expectedReplicas: 6,
			expectError:      false,
		},
		{
			name:              "Scaling down due to low CPU utilization",
			currentReplicas:   6,
			metricName:        MetricCPU,
			targetUtilization: 200,
			podMetrics: []InstanceMetric{
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(50)},
			},
			expectedReplicas: 2,
			expectError:      false,
		},
		{
			name:              "No scaling when within tolerance",
			currentReplicas:   4,
			metricName:        MetricCPU,
			targetUtilization: 100,
			podMetrics: []InstanceMetric{
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(105)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(95)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(100)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(100)},
			},
			expectedReplicas: 4,
			expectError:      false,
		},
		// - Included Pods: Pods with available metrics (Pod1, 150m CPU usage).
		// - Total Usage = 150m; Num Included Pods = 1; Average Usage = 150m.
		// - Usage Ratio = 1.5 (> 1.1 tolerance), suggesting scale-up.
		// - Missing Metrics Pods (Pod2) are assumed to consume 0% of target for scale-up.
		// - Adjusted Usage: 150m total over 2 pods = 75m; Adjusted Ratio = 0.75 (< target).
		// - Reassessed ratio suggests scale-down, reversing initial direction, so no scaling occurs.
		{
			name:              "Handling missing metrics when scaling up",
			currentReplicas:   2,
			metricName:        MetricCPU,
			targetUtilization: 100,
			podMetrics: []InstanceMetric{
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(150)},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: nil},
			},
			expectedReplicas: 2,
			expectError:      false,
		},
		{
			name:              "Handling not-yet-ready pods when scaling up",
			currentReplicas:   2,
			metricName:        MetricCPU,
			targetUtilization: 100,
			podMetrics: []InstanceMetric{
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: ptrInt64(150)},
				{Instance: function.Instance{AssignedAt: now.Add(-10 * time.Second)}, CPUUsage: ptrInt64(150)},
			},
			expectedReplicas: 2,
			expectError:      false,
		},
		{
			name:              "No metrics available",
			currentReplicas:   2,
			metricName:        MetricCPU,
			targetUtilization: 100,
			podMetrics: []InstanceMetric{
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: nil},
				{Instance: function.Instance{ReadyAt: now}, CPUUsage: nil},
			},
			expectedReplicas: 2,
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicas, err := calculateDesiredReplicasForMetric(
				tt.currentReplicas,
				tt.metricName,
				tt.podMetrics,
				tt.targetUtilization,
				hpaConfig,
				now,
			)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got none")
				}
				return
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
			}

			if replicas != tt.expectedReplicas {
				t.Errorf("expected replicas: %d, got: %d", tt.expectedReplicas, replicas)
			}
		})
	}
}
