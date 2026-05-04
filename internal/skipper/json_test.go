package skipper

import (
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

// Note: JSON tests use github.com/go-json-experiment/json which handles
// standard JSON marshaling/unmarshaling for all skipper types.

var (
	goldenTime = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	goldenScale = Scale_builder{
		MinInstances:           proto.Uint32(1),
		MaxInstances:           proto.Uint32(10),
		TargetCpuUsageMilli:    proto.Uint32(500),
		TargetMemoryUsageMib:   proto.Uint32(256),
		TargetInFlightRequests: proto.Uint32(100),
	}.Build()

	goldenFunction = Function_builder{
		Namespace:  new("skipper-production"),
		Deployment: new("my-app"),
		Tenant:     new("tenant-123"),
		Metadata:   new("metadata-value"),
		Scale:      goldenScale,
	}.Build()

	goldenHeartbeat = Heartbeat_builder{
		Function:         goldenFunction,
		Timestamp:        timestamppb.New(goldenTime),
		InFlightRequests: proto.Uint32(42),
	}.Build()

	goldenInstance = Instance_builder{
		Function:       goldenFunction,
		Name:           new("my-app-abc123"),
		Addr:           new("10.0.0.1:8080"),
		ReplicaSet:     new("my-app-5f4b8c"),
		AssignedAt:     timestamppb.New(goldenTime),
		ReadyAt:        timestamppb.New(goldenTime.Add(5 * time.Second)),
		CpuUsageMilli:  proto.Uint32(250),
		MemoryUsageMib: proto.Uint32(128),
	}.Build()

	goldenScaleDecision = ScaleDecision_builder{
		DesiredInstances:          proto.Uint32(3),
		UnclampedDesiredInstances: proto.Uint32(5),
		Reason:                    ScaleReason_SCALE_REASON_CPU.Enum(),
		Metrics: []*ScaleMetric{
			ScaleMetric_builder{Name: new("cpu"), Value: new(0.75)}.Build(),
			ScaleMetric_builder{Name: new("memory"), Value: new(0.5)}.Build(),
		},
	}.Build()

	goldenScaleMetric = ScaleMetric_builder{
		Name:  new("in_flight_requests"),
		Value: new(1.5),
	}.Build()
)

type goldenResult struct {
	Function      *Function
	Heartbeat     *Heartbeat
	Instance      *Instance
	Scale         *Scale
	ScaleDecision *ScaleDecision
	ScaleMetric   *ScaleMetric
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

	assert.Assert(t, proto.Equal(result.Function, goldenFunction), "function mismatch")
	assert.Assert(t, proto.Equal(result.Scale, goldenScale), "scale mismatch")
	assert.Assert(t, proto.Equal(result.Heartbeat, goldenHeartbeat), "heartbeat mismatch")
	assert.Assert(t, proto.Equal(result.Instance, goldenInstance), "instance mismatch")
	assert.Assert(t, proto.Equal(result.ScaleDecision, goldenScaleDecision), "scale decision mismatch")
	assert.Assert(t, proto.Equal(result.ScaleMetric, goldenScaleMetric), "scale metric mismatch")
}
