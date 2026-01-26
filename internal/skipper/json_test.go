package skipper

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
		// Non-prefixed values (current format)
		{name: "CPU", input: `"CPU"`, expected: ScaleReasonCPU},
		{name: "HEARTBEAT_TIMEOUT", input: `"HEARTBEAT_TIMEOUT"`, expected: ScaleReasonHeartbeatTimeout},
		{name: "IN_FLIGHT_REQUESTS", input: `"IN_FLIGHT_REQUESTS"`, expected: ScaleReasonInFlightRequests},
		{name: "MEMORY", input: `"MEMORY"`, expected: ScaleReasonMemory},
		{name: "NO_READY_INSTANCES", input: `"NO_READY_INSTANCES"`, expected: ScaleReasonNoReadyInstances},
		{name: "UNKNOWN", input: `"UNKNOWN"`, expected: ScaleReasonUnknown},

		// Protobuf-prefixed values
		{name: "prefixed CPU", input: `"SCALE_REASON_CPU"`, expected: ScaleReasonCPU},
		{name: "prefixed HEARTBEAT_TIMEOUT", input: `"SCALE_REASON_HEARTBEAT_TIMEOUT"`, expected: ScaleReasonHeartbeatTimeout},
		{name: "prefixed IN_FLIGHT_REQUESTS", input: `"SCALE_REASON_IN_FLIGHT_REQUESTS"`, expected: ScaleReasonInFlightRequests},
		{name: "prefixed MEMORY", input: `"SCALE_REASON_MEMORY"`, expected: ScaleReasonMemory},
		{name: "prefixed NO_READY_INSTANCES", input: `"SCALE_REASON_NO_READY_INSTANCES"`, expected: ScaleReasonNoReadyInstances},
		{name: "prefixed UNKNOWN", input: `"SCALE_REASON_UNKNOWN"`, expected: ScaleReasonUnknown},
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

func TestScaleMetricUnmarshalNumber(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected ScaleMetric
	}{
		{
			name:     "string value",
			input:    `{"name": "cpu", "value": "0.75"}`,
			expected: ScaleMetric{Name: "cpu", Value: 0.75},
		},
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

func TestScaleDecisionUnmarshalProtobufFormat(t *testing.T) {
	t.Parallel()

	// Protobuf format: prefixed reason, number values
	input := []byte(`{
		"desired_instances": "3",
		"unclamped_desired_instances": "5",
		"reason": "SCALE_REASON_CPU",
		"metrics": [
			{"name": "cpu", "value": 0.75},
			{"name": "memory", "value": 0.5}
		]
	}`)

	var decision ScaleDecision
	err := json.Unmarshal(input, &decision)
	assert.NilError(t, err)

	assert.DeepEqual(t, decision, goldenScaleDecision)
}

func TestBackwardsCompatibleUnmarshal(t *testing.T) {
	t.Parallel()

	t.Run("Scale", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name  string
			input string
		}{
			{
				name:  "number format (old)",
				input: `{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}`,
			},
			{
				name:  "string format (new)",
				input: `{"min_instances":"1","max_instances":"10","target_cpu_usage_milli":"500","target_memory_usage_mib":"256","target_in_flight_requests":"100"}`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var scale Scale
				err := json.Unmarshal([]byte(tc.input), &scale)
				assert.NilError(t, err)
				assert.DeepEqual(t, &scale, goldenScale)
			})
		}
	})

	t.Run("ScaleDecision", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name  string
			input string
		}{
			{
				name:  "number format (old)",
				input: `{"desired_instances":3,"unclamped_desired_instances":5,"reason":"CPU","metrics":[{"name":"cpu","value":0.75},{"name":"memory","value":0.5}]}`,
			},
			{
				name:  "string format (new)",
				input: `{"desired_instances":"3","unclamped_desired_instances":"5","reason":"CPU","metrics":[{"name":"cpu","value":0.75},{"name":"memory","value":0.5}]}`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var decision ScaleDecision
				err := json.Unmarshal([]byte(tc.input), &decision)
				assert.NilError(t, err)
				assert.DeepEqual(t, decision, goldenScaleDecision)
			})
		}
	})

	t.Run("Instance", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name  string
			input string
		}{
			{
				name:  "number format (old)",
				input: `{"function":{"namespace":"skipper-production","deployment":"my-app","tenant":"tenant-123","metadata":"metadata-value","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}},"name":"my-app-abc123","addr":"10.0.0.1:8080","replica_set":"my-app-5f4b8c","assigned_at":"2024-01-15T10:30:00Z","ready_at":"2024-01-15T10:30:05Z","cpu_usage_milli":250,"memory_usage_mib":128}`,
			},
			{
				name:  "string format (new)",
				input: `{"function":{"namespace":"skipper-production","deployment":"my-app","tenant":"tenant-123","metadata":"metadata-value","scale":{"min_instances":"1","max_instances":"10","target_cpu_usage_milli":"500","target_memory_usage_mib":"256","target_in_flight_requests":"100"}},"name":"my-app-abc123","addr":"10.0.0.1:8080","replica_set":"my-app-5f4b8c","assigned_at":"2024-01-15T10:30:00Z","ready_at":"2024-01-15T10:30:05Z","cpu_usage_milli":"250","memory_usage_mib":"128"}`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var instance Instance
				err := json.Unmarshal([]byte(tc.input), &instance)
				assert.NilError(t, err)
				assert.DeepEqual(t, &instance, goldenInstance)
			})
		}
	})

	t.Run("Heartbeat", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name  string
			input string
		}{
			{
				name:  "number format (old)",
				input: `{"function":{"namespace":"skipper-production","deployment":"my-app","tenant":"tenant-123","metadata":"metadata-value","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}},"timestamp":"2024-01-15T10:30:00Z","in_flight_requests":42}`,
			},
			{
				name:  "string format (new)",
				input: `{"function":{"namespace":"skipper-production","deployment":"my-app","tenant":"tenant-123","metadata":"metadata-value","scale":{"min_instances":"1","max_instances":"10","target_cpu_usage_milli":"500","target_memory_usage_mib":"256","target_in_flight_requests":"100"}},"timestamp":"2024-01-15T10:30:00Z","in_flight_requests":"42"}`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var heartbeat Heartbeat
				err := json.Unmarshal([]byte(tc.input), &heartbeat)
				assert.NilError(t, err)
				assert.DeepEqual(t, &heartbeat, goldenHeartbeat)
			})
		}
	})
}

func TestNormalizeScaleReason(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected ScaleReason
	}{
		// Non-prefixed values
		{name: "CPU", input: "CPU", expected: ScaleReasonCPU},
		{name: "MEMORY", input: "MEMORY", expected: ScaleReasonMemory},

		// Protobuf-prefixed values
		{name: "prefixed CPU", input: "SCALE_REASON_CPU", expected: ScaleReasonCPU},
		{name: "prefixed MEMORY", input: "SCALE_REASON_MEMORY", expected: ScaleReasonMemory},

		// Unknown values are preserved
		{name: "unknown value", input: "CUSTOM", expected: ScaleReason("CUSTOM")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := NormalizeScaleReason(tc.input)
			assert.Equal(t, result, tc.expected)
		})
	}
}
