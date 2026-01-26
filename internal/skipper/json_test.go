package skipper

import (
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

// Note: JSON tests use github.com/go-json-experiment/json which handles
// standard JSON marshaling/unmarshaling for all skipper types.

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

	goldenScaleDecision = ScaleDecision{
		DesiredInstances:          3,
		UnclampedDesiredInstances: 5,
		Reason:                    ScaleReasonCPU,
		Metrics: []ScaleMetric{
			{Name: "cpu", Value: 0.75},
			{Name: "memory", Value: 0.5},
		},
	}

	goldenScaleMetric = ScaleMetric{
		Name:  "in_flight_requests",
		Value: 1.5,
	}
)

type goldenResult struct {
	Function      *Function
	Heartbeat     *Heartbeat
	Instance      *Instance
	Scale         *Scale
	ScaleDecision ScaleDecision
	ScaleMetric   ScaleMetric
}

func TestJSONMarshal(t *testing.T) {
	result := goldenResult{
		Function:      goldenFunction,
		Heartbeat:     goldenHeartbeat,
		Instance:      goldenInstance,
		Scale:         goldenScale,
		ScaleDecision: goldenScaleDecision,
		ScaleMetric:   goldenScaleMetric,
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
	assert.DeepEqual(t, result.ScaleDecision, goldenScaleDecision)
	assert.DeepEqual(t, result.ScaleMetric, goldenScaleMetric)
}

func TestScaleReasonUnmarshal(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected ScaleReason
	}{
		{name: "CPU", input: `"CPU"`, expected: ScaleReasonCPU},
		{name: "HEARTBEAT_TIMEOUT", input: `"HEARTBEAT_TIMEOUT"`, expected: ScaleReasonHeartbeatTimeout},
		{name: "IN_FLIGHT_REQUESTS", input: `"IN_FLIGHT_REQUESTS"`, expected: ScaleReasonInFlightRequests},
		{name: "MEMORY", input: `"MEMORY"`, expected: ScaleReasonMemory},
		{name: "NO_READY_INSTANCES", input: `"NO_READY_INSTANCES"`, expected: ScaleReasonNoReadyInstances},
		{name: "UNKNOWN", input: `"UNKNOWN"`, expected: ScaleReasonUnknown},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var reason ScaleReason
			err := json.Unmarshal([]byte(tc.input), &reason)
			assert.NilError(t, err)
			assert.Equal(t, reason, tc.expected)
		})
	}
}

func TestScaleMetricUnmarshal(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected ScaleMetric
	}{
		{
			name:     "number value",
			input:    `{"name": "cpu", "value": 0.75}`,
			expected: ScaleMetric{Name: "cpu", Value: 0.75},
		},
		{
			name:     "integer number value",
			input:    `{"name": "count", "value": 42}`,
			expected: ScaleMetric{Name: "count", Value: 42},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var metric ScaleMetric
			err := json.Unmarshal([]byte(tc.input), &metric)
			assert.NilError(t, err)
			assert.DeepEqual(t, metric, tc.expected)
		})
	}
}

func TestIsValidScaleReason(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "CPU", input: "CPU", expected: true},
		{name: "HEARTBEAT_TIMEOUT", input: "HEARTBEAT_TIMEOUT", expected: true},
		{name: "IN_FLIGHT_REQUESTS", input: "IN_FLIGHT_REQUESTS", expected: true},
		{name: "MEMORY", input: "MEMORY", expected: true},
		{name: "NO_READY_INSTANCES", input: "NO_READY_INSTANCES", expected: true},
		{name: "UNKNOWN", input: "UNKNOWN", expected: true},
		{name: "invalid", input: "INVALID", expected: false},
		{name: "empty", input: "", expected: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := IsValidScaleReason(tc.input)
			assert.Equal(t, result, tc.expected)
		})
	}
}
