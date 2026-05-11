package controller

import (
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

// withHeartbeat returns a fresh heartbeat carrying the given assignment and a
// timestamp slightly in the past, so the heartbeat-timeout branch of
// calculateDesiredInstances depends entirely on the timeout knob under test.
func withHeartbeat(fn *skipper.Assignment, when time.Time, inFlight uint32) *skipper.Heartbeat {
	hb := &skipper.Heartbeat{}
	hb.SetAssignment(fn)
	hb.SetTimestamp(timestamppb.New(when))
	hb.SetInFlightRequests(inFlight)
	return hb
}

func policyTestAssignment(modify func(b *skipper.Assignment_builder)) *skipper.Assignment {
	b := skipper.Assignment_builder{
		Namespace:  new("ns"),
		Deployment: new("deploy"),
		Tenant:     new("tenant"),
		Scale: skipper.Scale_builder{
			MinInstances:           proto.Uint32(1),
			MaxInstances:           proto.Uint32(10),
			TargetCpuUsageMilli:    proto.Uint32(500),
			TargetMemoryUsageMib:   proto.Uint32(256),
			TargetInFlightRequests: proto.Uint32(100),
		}.Build(),
	}
	if modify != nil {
		modify(&b)
	}
	return b.Build()
}

// TestHeartbeatTimeoutOverride confirms calculateDesiredInstances honors
// per-assignment heartbeat_timeout for the scale-to-zero decision.
func TestHeartbeatTimeoutOverride(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HeartbeatTimeout = 90 * time.Second

	staleAt := time.Now().Add(-60 * time.Second) // beyond 30s, within 90s

	defaultFn := policyTestAssignment(nil)
	overriddenFn := policyTestAssignment(func(b *skipper.Assignment_builder) {
		b.HeartbeatTimeout = durationpb.New(30 * time.Second)
	})

	// Cluster default 90s: 60s-old heartbeat is fresh → no scale-to-zero.
	dDefault := calculateDesiredInstances(t.Context(), defaultFn, cfg, withHeartbeat(defaultFn, staleAt, 0), nil)
	assert.Assert(t, dDefault.GetReason() != skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT,
		"cluster-default timeout should treat 60s-old heartbeat as fresh; got reason=%v", dDefault.GetReason())

	// Tenant override 30s: 60s-old heartbeat triggers scale-to-zero.
	dOverride := calculateDesiredInstances(t.Context(), overriddenFn, cfg, withHeartbeat(overriddenFn, staleAt, 0), nil)
	assert.Equal(t, dOverride.GetReason(), skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT)
	assert.Equal(t, dOverride.GetDesiredInstances(), uint32(0))
}

// TestScaleToleranceOverride confirms calculateDesiredInstancesForMetric reads
// the tenant override of scale_tolerance.
func TestScaleToleranceOverride(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPATolerance = 0.1 // 10% — wide enough to swallow modest discrepancies
	cfg.HPAInitialReadinessDelay = 0

	// Two instances at 250m, target 500m, currentInstances=2 → averageUsage 250m,
	// usageRatio 0.5 → discrepancy 0.5 — outside any reasonable tolerance, so
	// the algorithm wants to scale down.
	readyAt := time.Now().Add(-time.Hour)
	instances := []*skipper.Instance{
		skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(250)}.Build(),
		skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(250)}.Build(),
	}

	defaultFn := policyTestAssignment(nil)
	wideTolerance := policyTestAssignment(func(b *skipper.Assignment_builder) {
		// Tenant override pushes tolerance up to 60% — the 50% discrepancy now
		// falls inside the no-scale window.
		b.ScaleTolerance = new(0.6)
	})

	dDefault, _ := calculateDesiredInstancesForMetric(t.Context(), defaultFn, cfg, MetricCPU, instances)
	assert.Assert(t, dDefault < 2, "cluster-default tolerance should scale down; got %d", dDefault)

	dOverride, _ := calculateDesiredInstancesForMetric(t.Context(), wideTolerance, cfg, MetricCPU, instances)
	assert.Equal(t, dOverride, 2, "wide tenant tolerance should hold current instance count")
}

// TestInitialReadinessDelayOverride confirms calculateDesiredInstancesForMetric
// reads the tenant override of scale_initial_readiness_delay.
func TestInitialReadinessDelayOverride(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPAInitialReadinessDelay = 1 * time.Hour // huge cluster default — pods always "too new"
	cfg.HPATolerance = 0

	// Pod ready 10 minutes ago.
	readyAt := time.Now().Add(-10 * time.Minute)
	instances := []*skipper.Instance{
		skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(1000)}.Build(),
	}

	// Cluster default treats the pod as too-new → no metrics → no scale-up.
	defaultFn := policyTestAssignment(nil)
	dDefault, _ := calculateDesiredInstancesForMetric(t.Context(), defaultFn, cfg, MetricCPU, instances)
	assert.Equal(t, dDefault, 1, "cluster default of 1h readiness delay excludes the pod")

	// Tenant override of 1m treats the 10m-ready pod as eligible — usage 1000m
	// versus target 500m drives scale-up.
	shortDelayFn := policyTestAssignment(func(b *skipper.Assignment_builder) {
		b.ScaleInitialReadinessDelay = durationpb.New(1 * time.Minute)
	})
	dOverride, _ := calculateDesiredInstancesForMetric(t.Context(), shortDelayFn, cfg, MetricCPU, instances)
	assert.Assert(t, dOverride > 1, "tenant-override readiness delay should observe usage; got %d", dOverride)
}

// TestDownscaleStabilizationOverride confirms recordRecommendation reads the
// tenant override of scale_downscale_stabilization.
func TestDownscaleStabilizationOverride(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPADownscaleStabilization = 5 * time.Minute

	supervisorFor := func(t *testing.T, fn *skipper.Assignment) *Supervisor {
		t.Helper()
		ctrl := &Controller{config: cfg}
		s := &Supervisor{ctrl: ctrl}
		s.fn.Store(fn)
		return s
	}

	// Pre-populate stabilization window with an old recommendation (3 min ago)
	// and a fresh one (now).
	mkSupervisor := func(t *testing.T, fn *skipper.Assignment) *Supervisor {
		s := supervisorFor(t, fn)
		s.stabilizationWindow = []Recommendation{
			{DesiredInstances: 5, Timestamp: time.Now().Add(-3 * time.Minute)},
		}
		return s
	}

	// Cluster default 5m: 3-min-old recommendation falls inside the window.
	defaultFn := policyTestAssignment(nil)
	sDefault := mkSupervisor(t, defaultFn)
	maxDefault := sDefault.recordRecommendation(defaultFn, 1)
	assert.Equal(t, maxDefault.DesiredInstances, uint32(5),
		"cluster-default stabilization should retain 3-min-old recommendation")

	// Tenant override of 1 minute: 3-min-old recommendation is pruned.
	shortFn := policyTestAssignment(func(b *skipper.Assignment_builder) {
		b.ScaleDownscaleStabilization = durationpb.New(1 * time.Minute)
	})
	sShort := mkSupervisor(t, shortFn)
	maxShort := sShort.recordRecommendation(shortFn, 1)
	assert.Equal(t, maxShort.DesiredInstances, uint32(1),
		"tenant-override stabilization should drop 3-min-old recommendation")
}

// TestAssignTimeoutOverride confirms cleanupStuckInstances reads the
// per-assignment assign_timeout when deciding whether an instance is stuck.
// We exercise the keep-path (override widens the threshold past the
// instance's age) to avoid touching the apiserver in this unit test;
// integration coverage of the delete-path lives in TestCleanupStuckInstance.
func TestAssignTimeoutOverride(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.AssignTimeout = 30 * time.Second

	ctrl := &Controller{config: cfg}

	// Tenant override 5 minutes → stuck threshold = 10 minutes → 90s-old
	// instance is fresh; cleanupStuckInstances should keep it without
	// calling deletePod.
	longFn := policyTestAssignment(func(b *skipper.Assignment_builder) {
		b.AssignTimeout = durationpb.New(5 * time.Minute)
	})
	instance := skipper.Instance_builder{
		Name:       new("fresh-pod"),
		AssignedAt: timestamppb.New(time.Now().Add(-90 * time.Second)),
	}.Build()
	instance.SetAssignment(longFn)
	s := &Supervisor{ctrl: ctrl}
	s.fn.Store(longFn)
	left := s.cleanupStuckInstances(t.Context(), longFn, []*skipper.Instance{instance})
	assert.Equal(t, len(left), 1, "tenant override should keep 90s-old instance under 5m timeout")
}

// TestScaleTargetCPUOverride confirms calculateDesiredInstances reads the
// per-assignment scale_target_cpu_millicores (flat-preferred-over-Scale).
func TestScaleTargetCPUOverride(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPATolerance = 0
	cfg.HPAInitialReadinessDelay = 0
	cfg.HeartbeatTimeout = 1 * time.Hour

	readyAt := time.Now().Add(-time.Hour)
	instances := []*skipper.Instance{
		skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(500)}.Build(),
	}
	hb := &skipper.Heartbeat{}
	hb.SetTimestamp(timestamppb.Now())

	// Nested-only: target 500m, usage 500m → ratio 1 → no scaling.
	nestedFn := policyTestAssignment(nil)
	hb.SetAssignment(nestedFn)
	dNested := calculateDesiredInstances(t.Context(), nestedFn, cfg, hb, instances)
	assert.Equal(t, dNested.GetDesiredInstances(), uint32(1))

	// Flat override 100m: usage 500m → ratio 5 → scale up.
	flatFn := policyTestAssignment(func(b *skipper.Assignment_builder) {
		b.ScaleTargetCpuMillicores = proto.Uint32(100)
	})
	hb.SetAssignment(flatFn)
	dFlat := calculateDesiredInstances(t.Context(), flatFn, cfg, hb, instances)
	assert.Assert(t, dFlat.GetDesiredInstances() > 1, "flat-preferred target should drive scale-up; got %d", dFlat.GetDesiredInstances())
}

// TestPlaceholderKnobsNoEffectOnSupervisor confirms that setting any
// placeholder knob (transport_*, zone_*, retry_backpressure, retry_status_codes,
// heartbeat_interval, assign_path) leaves the supervisor's decisions identical
// to an assignment that did not set the knob.
func TestPlaceholderKnobsNoEffectOnSupervisor(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPATolerance = 0
	cfg.HPAInitialReadinessDelay = 0
	cfg.HeartbeatTimeout = 1 * time.Hour

	hb := &skipper.Heartbeat{}
	hb.SetTimestamp(timestamppb.Now())
	hb.SetInFlightRequests(5)
	instances := []*skipper.Instance{
		skipper.Instance_builder{ReadyAt: timestamppb.New(time.Now().Add(-time.Hour)), CpuUsageMilli: proto.Uint32(500)}.Build(),
	}

	baseFn := policyTestAssignment(nil)
	hb.SetAssignment(baseFn)
	baseDecision := calculateDesiredInstances(t.Context(), baseFn, cfg, hb, instances)

	withPlaceholders := policyTestAssignment(func(b *skipper.Assignment_builder) {
		b.ZoneSpread = skipper.ZoneSpread_ZONE_SPREAD_REQUIRED.Enum()
		b.ZoneMin = proto.Uint32(3)
		b.ZoneAffinity = skipper.ZoneAffinity_ZONE_AFFINITY_REQUIRED.Enum()
		b.AssignPath = new("/__skipper/v2/assign")
		b.HeartbeatInterval = durationpb.New(7 * time.Second)
		b.RetryBackpressure = skipper.Backpressure_BACKPRESSURE_RETRY_AND_EJECT.Enum()
		b.RetryStatusCodes = []uint32{503, 504, 529}
		b.TransportDialTimeout = durationpb.New(100 * time.Millisecond)
		b.TransportKeepalive = durationpb.New(30 * time.Second)
		b.TransportIdleConnTimeout = durationpb.New(90 * time.Second)
		b.TransportTlsHandshakeTimeout = durationpb.New(10 * time.Second)
		b.TransportMaxIdleConns = proto.Uint32(200)
		b.TransportForceHttp2 = new(true)
		b.TransportDisableCompression = new(true)
		b.TransportFlushInterval = durationpb.New(100 * time.Millisecond)
	})
	hb.SetAssignment(withPlaceholders)
	placeholderDecision := calculateDesiredInstances(t.Context(), withPlaceholders, cfg, hb, instances)

	assert.Equal(t, placeholderDecision.GetDesiredInstances(), baseDecision.GetDesiredInstances(),
		"placeholder knobs must not affect supervisor scaling decisions")
	assert.Equal(t, placeholderDecision.GetReason(), baseDecision.GetReason())
}
