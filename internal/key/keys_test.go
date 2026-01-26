package key_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/golden"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestKeys(t *testing.T) {
	keys := []key.Attr{
		key.Addr.Attr(fakeString),
		key.AssignedAt.Attr(fakeTime),
		key.Attempt.Attr(fakeInt),
		key.CPUUsageMilli.Attr(fakeUint32),
		key.Count.Attr(fakeInt),
		key.Deployment.Attr(fakeString),
		key.DesiredInstances.Attr(fakeUint32),
		key.Duration.Attr(fakeDuration),
		key.Error.Attr(fakeError),
		key.ExcludeInstanceNames.Attr(fakeStringSlice),
		key.Function.Attr(fakeFunction),
		key.GetInstanceDurationMs.Attr(fakeDuration),
		key.Heartbeat.Attr(fakeHeartbeat),
		key.InFlightRequests.Attr(fakeUint32),
		key.Instance.Attr(fakeInstance),
		key.K8sReplicaSet.Attr(fakeReplicaSet),
		key.Labels.Attr(fakeStringMap),
		key.MaxInstances.Attr(fakeUint32),
		key.MemoryUsageMiB.Attr(fakeUint32),
		key.Metadata.Attr(fakeString),
		key.MinInstances.Attr(fakeUint32),
		key.Name.Attr(fakeString),
		key.Namespace.Attr(fakeString),
		key.Pod.Attr(fakePod),
		key.ReadyAt.Attr(fakeTime),
		key.ReadyInstances.Attr(fakeInt),
		key.Reason.Attr(fakeString),
		key.ReplicaSet.Attr(fakeString),
		key.Request.Attr(fakeRequest),
		key.Response.Attr(fakeResponse),
		key.ResponsibleIP.Attr(fakeString),
		key.RouterIP.Attr(fakeString),
		key.Scale.Attr(fakeScale),
		key.ScaleDecision.Attr(fakeScaleDecision),
		key.TargetCPUUsageMilli.Attr(fakeUint32),
		key.TargetInFlightRequests.Attr(fakeUint32),
		key.TargetMemoryUsageMiB.Attr(fakeUint32),
		key.Tenant.Attr(fakeString),
		key.Timestamp.Attr(fakeTime),
		key.URL.Attr(fakeURL),
		key.UnclampedDesiredInstances.Attr(fakeUint32),
		key.UnreadyInstances.Attr(fakeInt),
	}

	slogAttrs := make(map[string]any)
	otelAttrs := make(map[string]any)

	for _, attr := range keys {
		// Convert slog attr to JSON and extract key-value pairs
		slogJSON := slogAttrToJSON(t, attr.Slog)
		maps.Copy(slogAttrs, slogJSON)

		// Collect otel attrs
		for _, otelAttr := range attr.Otel {
			otelAttrs[string(otelAttr.Key)] = otelAttr.Value.AsInterface()
		}
	}

	result := map[string]any{
		"slog": sortMapKeys(slogAttrs),
		"otel": sortMapKeys(otelAttrs),
	}

	output, err := json.Marshal(result, jsontext.Multiline(true), json.Deterministic(true))
	if err != nil {
		t.Fatalf("failed to marshal results: %v", err)
	}

	golden.Assert(t, string(output)+"\n", "keys.golden")
}

// slogAttrToJSON converts a slog.Attr to a JSON-friendly map.
func slogAttrToJSON(t *testing.T, attr slog.Attr) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 {
				switch a.Key {
				case slog.TimeKey, slog.LevelKey, slog.MessageKey:
					return slog.Attr{}
				}
			}
			return a
		},
	})
	logger := slog.New(handler)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "", attr)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse slog JSON: %v\nraw: %s", err, buf.String())
	}
	return result
}

// sortMapKeys returns a new map with keys sorted for deterministic output.
func sortMapKeys(m map[string]any) map[string]any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sorted := make(map[string]any, len(m))
	for _, k := range keys {
		sorted[k] = m[k]
	}
	return sorted
}

var (
	fakeString      = "test"
	fakeInt         = 42
	fakeUint32      = uint32(42)
	fakeFloat       = 1.5
	fakeTime        = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	fakeDuration    = 100 * time.Millisecond
	fakeError       = errors.New("error") //nolint:staticcheck // this error doesn't need to start with "err"
	fakeStringSlice = []string{"a", "b"}
	fakeStringMap   = map[string]string{"key": "value"}

	fakeScale = skipper.Scale_builder{
		MinInstances:           proto.Uint32(fakeUint32),
		MaxInstances:           proto.Uint32(fakeUint32),
		TargetCpuUsageMilli:    proto.Uint32(fakeUint32),
		TargetMemoryUsageMib:   proto.Uint32(fakeUint32),
		TargetInFlightRequests: proto.Uint32(fakeUint32),
	}.Build()

	fakeFunction = skipper.Function_builder{
		Namespace:  proto.String(fakeString),
		Deployment: proto.String(fakeString),
		Tenant:     proto.String(fakeString),
		Metadata:   proto.String(fakeString),
		Scale:      fakeScale,
	}.Build()

	fakeHeartbeat = skipper.Heartbeat_builder{
		Function:         fakeFunction,
		Timestamp:        timestamppb.New(fakeTime),
		InFlightRequests: proto.Uint32(fakeUint32),
	}.Build()

	fakeInstance = skipper.Instance_builder{
		Function:       fakeFunction,
		Name:           proto.String(fakeString),
		Addr:           proto.String(fakeString),
		ReplicaSet:     proto.String(fakeString),
		AssignedAt:     timestamppb.New(fakeTime),
		ReadyAt:        timestamppb.New(fakeTime),
		CpuUsageMilli:  proto.Uint32(fakeUint32),
		MemoryUsageMib: proto.Uint32(fakeUint32),
	}.Build()

	fakeScaleDecision = skipper.ScaleDecision_builder{
		DesiredInstances:          proto.Uint32(fakeUint32),
		UnclampedDesiredInstances: proto.Uint32(fakeUint32),
		Reason:                    skipper.ScaleReason_SCALE_REASON_CPU.Enum(),
		Metrics:                   []*skipper.ScaleMetric{skipper.ScaleMetric_builder{Name: proto.String(fakeString), Value: proto.Float64(fakeFloat)}.Build()},
	}.Build()

	fakePod = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fakeString,
			Namespace: fakeString,
			UID:       types.UID(fakeString),
			Labels:    fakeStringMap,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: fakeString,
			Conditions: []v1.PodCondition{
				{Type: v1.PodReady, Status: v1.ConditionTrue},
			},
		},
	}

	fakeReplicaSet = &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fakeString,
			Namespace: fakeString,
			UID:       types.UID(fakeString),
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:          int32(fakeInt),
			AvailableReplicas: int32(fakeInt),
		},
	}

	fakeURL = &url.URL{
		Scheme:   "http",
		Host:     "example.com",
		Path:     "/path",
		RawQuery: "query=value",
		Fragment: "fragment",
		User:     url.UserPassword("user", "password"),
	}

	fakeResponse = &http.Response{
		StatusCode:    fakeInt,
		Status:        fakeString,
		ContentLength: int64(fakeInt),
		Header: http.Header{
			"Content-Type":   []string{fakeString},
			"Content-Length": []string{strconv.Itoa(fakeInt)},
		},
	}

	fakeRequest = &http.Request{
		URL:           fakeURL,
		Method:        fakeString,
		ContentLength: int64(fakeInt),
		Header: http.Header{
			"Content-Type":   []string{fakeString},
			"Content-Length": []string{strconv.Itoa(fakeInt)},
		},
	}
)
