package web

import (
	"html/template"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

func TestTimeAgo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		ts   *timestamppb.Timestamp
		want string
	}{
		{name: "nil", ts: nil, want: "—"},
		{name: "just now", ts: timestamppb.Now(), want: "0s ago"},
		{name: "seconds", ts: timestamppb.New(time.Now().Add(-30 * time.Second)), want: "30s ago"},
		{name: "minutes", ts: timestamppb.New(time.Now().Add(-5 * time.Minute)), want: "5m ago"},
		{name: "hours", ts: timestamppb.New(time.Now().Add(-2 * time.Hour)), want: "2h ago"},
		{name: "days", ts: timestamppb.New(time.Now().Add(-48 * time.Hour)), want: "2d ago"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := timeAgo(tc.ts)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	t.Parallel()

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, formatTimestamp(nil), "—")
	})

	t.Run("formats as RFC3339", func(t *testing.T) {
		t.Parallel()
		ts := timestamppb.New(time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC))
		assert.Equal(t, formatTimestamp(ts), "2025-01-15T10:30:00Z")
	})
}

func TestDurationBetween(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		start *timestamppb.Timestamp
		end   *timestamppb.Timestamp
		want  string
	}{
		{name: "nil start", start: nil, end: timestamppb.Now(), want: "—"},
		{name: "nil end", start: timestamppb.Now(), end: nil, want: "—"},
		{name: "seconds", start: timestamppb.New(time.Now()), end: timestamppb.New(time.Now().Add(15 * time.Second)), want: "15s"},
		{name: "minutes", start: timestamppb.New(time.Now()), end: timestamppb.New(time.Now().Add(90 * time.Second)), want: "1m 30s"},
		{name: "exact minutes", start: timestamppb.New(time.Now()), end: timestamppb.New(time.Now().Add(2 * time.Minute)), want: "2m"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := durationBetween(tc.start, tc.end)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestFunctionKey(t *testing.T) {
	t.Parallel()

	fn := skipper.Assignment_builder{
		Namespace:  new("default"),
		Deployment: new("web-app"),
		Tenant:     new("tenant-1"),
	}.Build()

	assert.Equal(t, assignmentKey(fn), "default:web-app:tenant-1")
	assert.Equal(t, assignmentKey(nil), "")
}

func TestAssignmentPath(t *testing.T) {
	t.Parallel()

	fn := skipper.Assignment_builder{
		Namespace:  new("default"),
		Deployment: new("web-app"),
		Tenant:     new("tenant-1"),
	}.Build()

	assert.Equal(t, assignmentPath(fn), "/assignments/default:web-app:tenant-1")
}

func TestScaleReasonLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, scaleReasonLabel(skipper.ScaleReason_SCALE_REASON_CPU), "cpu")
	assert.Equal(t, scaleReasonLabel(skipper.ScaleReason_SCALE_REASON_MEMORY), "memory")
	assert.Equal(t, scaleReasonLabel(skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT), "heartbeat_timeout")
	assert.Equal(t, scaleReasonLabel(skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS), "in_flight_requests")
	assert.Equal(t, scaleReasonLabel(skipper.ScaleReason_SCALE_REASON_NO_READY_INSTANCES), "no_ready_instances")
	assert.Equal(t, scaleReasonLabel(skipper.ScaleReason_SCALE_REASON_UNSPECIFIED), "unspecified")
}

func TestEventTypeLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, eventTypeLabel(skipper.EventType_EVENT_TYPE_SCALE_UP), "scale up")
	assert.Equal(t, eventTypeLabel(skipper.EventType_EVENT_TYPE_SCALE_DOWN), "scale down")
	assert.Equal(t, eventTypeLabel(skipper.EventType_EVENT_TYPE_POD_ASSIGNED), "pod assigned")
	assert.Equal(t, eventTypeLabel(skipper.EventType_EVENT_TYPE_POD_DELETED), "pod deleted")
	assert.Equal(t, eventTypeLabel(skipper.EventType_EVENT_TYPE_STALE_REPLACEMENT), "stale replacement")
	assert.Equal(t, eventTypeLabel(skipper.EventType_EVENT_TYPE_HEARTBEAT_TIMEOUT), "heartbeat timeout")
	assert.Equal(t, eventTypeLabel(skipper.EventType_EVENT_TYPE_STUCK_INSTANCE_CLEANUP), "stuck instance cleanup")
	assert.Equal(t, eventTypeLabel(skipper.EventType_EVENT_TYPE_UNSPECIFIED), "unspecified")
}

func TestEventSeverityBadge(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		severity skipper.EventSeverity
		want     template.HTML
	}{
		{name: "warn", severity: skipper.EventSeverity_EVENT_SEVERITY_WARN, want: `<span class="badge badge-yellow">warn</span>`},
		{name: "info", severity: skipper.EventSeverity_EVENT_SEVERITY_INFO, want: `<span class="badge badge-muted">info</span>`},
		{name: "unspecified", severity: skipper.EventSeverity_EVENT_SEVERITY_UNSPECIFIED, want: `<span class="badge badge-muted">unknown</span>`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := eventSeverityBadge(tc.severity)
			assert.Equal(t, string(got), string(tc.want))
		})
	}
}

func TestStatusBadge(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		ready bool
		want  template.HTML
	}{
		{name: "ready", ready: true, want: `<span class="badge badge-green">ready</span>`},
		{name: "pending", ready: false, want: `<span class="badge badge-yellow">pending</span>`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := statusBadge(tc.ready)
			assert.Equal(t, string(got), string(tc.want))
		})
	}
}

func TestInstanceState(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		inst     *skipper.Instance
		activeRS string
		want     string
	}{
		{
			name: "ready instance on active RS",
			inst: skipper.Instance_builder{
				Name:       new("pod-1"),
				ReplicaSet: new("rs-1"),
				ReadyAt:    timestamppb.Now(),
			}.Build(),
			activeRS: "rs-1",
			want:     "ready",
		},
		{
			name: "pending instance on active RS",
			inst: skipper.Instance_builder{
				Name:       new("pod-2"),
				ReplicaSet: new("rs-1"),
			}.Build(),
			activeRS: "rs-1",
			want:     "pending",
		},
		{
			name: "ready instance on non-active RS",
			inst: skipper.Instance_builder{
				Name:       new("pod-3"),
				ReplicaSet: new("rs-old"),
				ReadyAt:    timestamppb.Now(),
			}.Build(),
			activeRS: "rs-1",
			want:     "stale",
		},
		{
			name: "ready instance with empty activeRS",
			inst: skipper.Instance_builder{
				Name:       new("pod-4"),
				ReplicaSet: new("rs-1"),
				ReadyAt:    timestamppb.Now(),
			}.Build(),
			activeRS: "",
			want:     "ready",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := instanceState(tc.inst, tc.activeRS)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestInstanceStateBadge(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		inst     *skipper.Instance
		activeRS string
		want     template.HTML
	}{
		{
			name: "ready instance on active RS",
			inst: skipper.Instance_builder{
				Name:       new("pod-1"),
				ReplicaSet: new("rs-1"),
				ReadyAt:    timestamppb.Now(),
			}.Build(),
			activeRS: "rs-1",
			want:     `<span class="badge badge-green">ready</span>`,
		},
		{
			name: "pending instance on active RS",
			inst: skipper.Instance_builder{
				Name:       new("pod-2"),
				ReplicaSet: new("rs-1"),
			}.Build(),
			activeRS: "rs-1",
			want:     `<span class="badge badge-yellow">pending</span>`,
		},
		{
			name: "ready instance on non-active RS",
			inst: skipper.Instance_builder{
				Name:       new("pod-3"),
				ReplicaSet: new("rs-old"),
				ReadyAt:    timestamppb.Now(),
			}.Build(),
			activeRS: "rs-1",
			want:     `<span class="badge badge-yellow">stale</span>`,
		},
		{
			name: "ready instance with empty activeRS",
			inst: skipper.Instance_builder{
				Name:       new("pod-4"),
				ReplicaSet: new("rs-1"),
				ReadyAt:    timestamppb.Now(),
			}.Build(),
			activeRS: "",
			want:     `<span class="badge badge-green">ready</span>`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := instanceStateBadge(tc.inst, tc.activeRS)
			assert.Equal(t, string(got), string(tc.want))
		})
	}
}

func TestMetricsString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		metrics []*skipper.ScaleMetric
		want    string
	}{
		{name: "nil", metrics: nil, want: "—"},
		{name: "empty", metrics: []*skipper.ScaleMetric{}, want: "—"},
		{
			name: "single metric",
			metrics: []*skipper.ScaleMetric{
				skipper.ScaleMetric_builder{Name: new("cpu"), Value: new(1.0)}.Build(),
			},
			want: "cpu: 1.0",
		},
		{
			name: "multiple metrics",
			metrics: []*skipper.ScaleMetric{
				skipper.ScaleMetric_builder{Name: new("cpu"), Value: new(50.0)}.Build(),
				skipper.ScaleMetric_builder{Name: new("memory"), Value: new(128.0)}.Build(),
			},
			want: "cpu: 50.0, memory: 128.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := metricsString(tc.metrics)
			assert.Equal(t, got, tc.want)
		})
	}
}
