package skipper

import (
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
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

	goldenAssignment = Assignment_builder{
		Namespace:  new("skipper-production"),
		Deployment: new("my-app"),
		Tenant:     new("tenant-123"),
		Metadata:   new("metadata-value"),
		Scale:      goldenScale,
	}.Build()

	goldenHeartbeat = Heartbeat_builder{
		Assignment:       goldenAssignment,
		Timestamp:        timestamppb.New(goldenTime),
		InFlightRequests: proto.Uint32(42),
	}.Build()

	goldenInstance = Instance_builder{
		Assignment:     goldenAssignment,
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
	Assignment    *Assignment
	Heartbeat     *Heartbeat
	Instance      *Instance
	Scale         *Scale
	ScaleDecision *ScaleDecision
	ScaleMetric   *ScaleMetric
}

func TestJSONMarshal(t *testing.T) {
	result := goldenResult{
		Assignment:    goldenAssignment,
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

	assert.Assert(t, proto.Equal(result.Assignment, goldenAssignment), "assignment mismatch")
	assert.Assert(t, proto.Equal(result.Scale, goldenScale), "scale mismatch")
	assert.Assert(t, proto.Equal(result.Heartbeat, goldenHeartbeat), "heartbeat mismatch")
	assert.Assert(t, proto.Equal(result.Instance, goldenInstance), "instance mismatch")
	assert.Assert(t, proto.Equal(result.ScaleDecision, goldenScaleDecision), "scale decision mismatch")
	assert.Assert(t, proto.Equal(result.ScaleMetric, goldenScaleMetric), "scale metric mismatch")
}

// TestJSONFlatPolicyRoundtrip exercises the full flat policy surface
// (29 fields) plus the three enums, ensuring marshal/unmarshal preserves
// every wired and placeholder knob a tenant can set in the header.
func TestJSONFlatPolicyRoundtrip(t *testing.T) {
	t.Parallel()

	full := Assignment_builder{
		Namespace:                    new("ns"),
		Deployment:                   new("deploy"),
		Tenant:                       new("tenant"),
		Metadata:                     new("opaque"),
		Oneshot:                      new(true),
		ScaleMinInstances:            proto.Uint32(2),
		ScaleMaxInstances:            proto.Uint32(20),
		ScaleTargetCpuMillicores:     proto.Uint32(500),
		ScaleTargetMemoryMebibytes:   proto.Uint32(256),
		ScaleTargetInFlightRequests:  proto.Uint32(100),
		ScaleTolerance:               new(0.15),
		ScaleDownscaleStabilization:  durationpb.New(2 * time.Minute),
		ScaleInitialReadinessDelay:   durationpb.New(15 * time.Second),
		ZoneSpread:                   ZoneSpread_ZONE_SPREAD_PREFERRED.Enum(),
		ZoneMin:                      proto.Uint32(2),
		ZoneAffinity:                 ZoneAffinity_ZONE_AFFINITY_PREFERRED.Enum(),
		AssignPath:                   new("/__skipper/assign-v2"),
		AssignTimeout:                durationpb.New(45 * time.Second),
		AssignTokenTtl:               durationpb.New(2 * time.Hour),
		HeartbeatInterval:            durationpb.New(7 * time.Second),
		HeartbeatTimeout:             durationpb.New(120 * time.Second),
		RetryMaxAttempts:             proto.Uint32(8),
		RetryMinBackoff:              durationpb.New(150 * time.Millisecond),
		RetryMaxBackoff:              durationpb.New(10 * time.Second),
		RetryBackpressure:            Backpressure_BACKPRESSURE_RETRY.Enum(),
		RetryStatusCodes:             []uint32{503, 504, 529},
		TransportDialTimeout:         durationpb.New(500 * time.Millisecond),
		TransportKeepalive:           durationpb.New(30 * time.Second),
		TransportIdleConnTimeout:     durationpb.New(90 * time.Second),
		TransportTlsHandshakeTimeout: durationpb.New(10 * time.Second),
		TransportMaxIdleConns:        proto.Uint32(100),
		TransportForceHttp2:          new(true),
		TransportDisableCompression:  new(true),
		TransportFlushInterval:       durationpb.New(100 * time.Millisecond),
	}.Build()

	body, err := json.Marshal(full)
	assert.NilError(t, err)

	var got Assignment
	err = json.Unmarshal(body, &got)
	assert.NilError(t, err)
	assert.Assert(t, proto.Equal(&got, full), "full flat-policy assignment did not roundtrip")
}

// TestJSONFlatScaleHybrid covers the three shapes a tenant can use for scale:
// nested only, flat only, and both — confirming that JSON parses each into
// distinct presence bits and that resolvers see flat-wins on conflict.
func TestJSONFlatScaleHybrid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantMin uint32
		wantMax uint32
		wantCPU uint32
	}{
		{
			name:    "nested only",
			input:   `{"namespace":"n","deployment":"d","tenant":"t","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500}}`,
			wantMin: 1, wantMax: 10, wantCPU: 500,
		},
		{
			name:    "flat only",
			input:   `{"namespace":"n","deployment":"d","tenant":"t","scale_min_instances":2,"scale_max_instances":20,"scale_target_cpu_millicores":600}`,
			wantMin: 2, wantMax: 20, wantCPU: 600,
		},
		{
			name:    "both, flat wins",
			input:   `{"namespace":"n","deployment":"d","tenant":"t","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500},"scale_min_instances":3,"scale_max_instances":30,"scale_target_cpu_millicores":700}`,
			wantMin: 3, wantMax: 30, wantCPU: 700,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var a Assignment
			err := json.Unmarshal([]byte(tc.input), &a)
			assert.NilError(t, err)
			assert.Equal(t, a.ScaleMinInstances(), tc.wantMin)
			assert.Equal(t, a.ScaleMaxInstances(), tc.wantMax)
			assert.Equal(t, a.ScaleTargetCPUMillicores(), tc.wantCPU)
		})
	}
}
