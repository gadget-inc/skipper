package function

import (
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

var (
	goldenTime = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	goldenScale = &Scale{
		MinInstances:           1,
		MaxInstances:           10,
		TargetCPUUsageMilli:    500,
		TargetMemoryUsageMiB:   256,
		TargetInFlightRequests: 100,
	}

	goldenFunction = &Function{
		Namespace:  "skipper-production",
		Deployment: "my-app",
		Tenant:     "tenant-123",
		Metadata:   "metadata-value",
		Scale:      goldenScale,
	}

	goldenHeartbeat = &Heartbeat{
		Function:         goldenFunction,
		Timestamp:        goldenTime,
		InFlightRequests: 42,
	}

	goldenInstance = &Instance{
		Function:       goldenFunction,
		Name:           "my-app-abc123",
		Addr:           "10.0.0.1:8080",
		ReplicaSet:     "my-app-5f4b8c",
		AssignedAt:     goldenTime,
		ReadyAt:        goldenTime.Add(5 * time.Second),
		CPUUsageMilli:  250,
		MemoryUsageMiB: 128,
	}

	goldenScalingDecision = ScalingDecision{
		DesiredInstances:          3,
		UnclampedDesiredInstances: 5,
		Reason:                    ScalingReasonCPU,
		Metrics: []ScalingMetric{
			{Name: "cpu", Value: 0.75},
			{Name: "memory", Value: 0.5},
		},
	}

	goldenScalingMetric = ScalingMetric{
		Name:  "in_flight_requests",
		Value: 1.5,
	}
)

type goldenResult struct {
	Function        *Function
	Heartbeat       *Heartbeat
	Instance        *Instance
	Scale           *Scale
	ScalingDecision ScalingDecision
	ScalingMetric   ScalingMetric
}

func TestJSONMarshal(t *testing.T) {
	result := goldenResult{
		Function:        goldenFunction,
		Heartbeat:       goldenHeartbeat,
		Instance:        goldenInstance,
		Scale:           goldenScale,
		ScalingDecision: goldenScalingDecision,
		ScalingMetric:   goldenScalingMetric,
	}

	output, err := json.Marshal(result, jsontext.Multiline(true))
	assert.NilError(t, err)

	golden.Assert(t, string(output)+"\n", "json.golden")
}

func TestJSONUnmarshal(t *testing.T) {
	var result goldenResult
	err := json.Unmarshal(golden.Get(t, "json.golden"), &result)
	assert.NilError(t, err)

	assert.DeepEqual(t, result.Function, goldenFunction)
	assert.DeepEqual(t, result.Scale, goldenScale)
	assert.DeepEqual(t, result.Heartbeat, goldenHeartbeat)
	assert.DeepEqual(t, result.Instance, goldenInstance)
	assert.DeepEqual(t, result.ScalingDecision, goldenScalingDecision)
	assert.DeepEqual(t, result.ScalingMetric, goldenScalingMetric)
}

// TestJSONUnmarshalSnakeCase verifies that snake_case field names are accepted
// during unmarshaling for backward compatibility migration.
func TestJSONUnmarshalSnakeCase(t *testing.T) {
	t.Run("Instance", func(t *testing.T) {
		snakeCaseJSON := []byte(`{
			"namespace": "skipper-production",
			"deployment": "my-app",
			"tenant": "tenant-123",
			"metadata": "metadata-value",
			"scale": {
				"min_instances": 1,
				"max_instances": 10,
				"target_cpu_usage_milli": 500,
				"target_memory_usage_mib": 256,
				"target_in_flight_requests": 100
			},
			"name": "my-app-abc123",
			"addr": "10.0.0.1:8080",
			"replica_set": "my-app-5f4b8c",
			"assigned_at": "2024-01-15T10:30:00Z",
			"ready_at": "2024-01-15T10:30:05Z",
			"cpu_usage_milli": 250,
			"memory_usage_mib": 128
		}`)

		var instance Instance
		err := json.Unmarshal(snakeCaseJSON, &instance)
		assert.NilError(t, err)
		assert.DeepEqual(t, &instance, goldenInstance)
	})

	t.Run("ScalingDecision", func(t *testing.T) {
		snakeCaseJSON := []byte(`{
			"desired_instances": 3,
			"unclamped_desired_instances": 5,
			"reason": "cpu",
			"metrics": [
				{"name": "cpu", "value": 0.75},
				{"name": "memory", "value": 0.5}
			]
		}`)

		var decision ScalingDecision
		err := json.Unmarshal(snakeCaseJSON, &decision)
		assert.NilError(t, err)
		assert.DeepEqual(t, decision, goldenScalingDecision)
	})

	t.Run("ScalingMetric", func(t *testing.T) {
		snakeCaseJSON := []byte(`{"name": "in_flight_requests", "value": 1.5}`)

		var metric ScalingMetric
		err := json.Unmarshal(snakeCaseJSON, &metric)
		assert.NilError(t, err)
		assert.DeepEqual(t, metric, goldenScalingMetric)
	})
}
