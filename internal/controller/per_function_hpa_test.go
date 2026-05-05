package controller

import (
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

func withHpaPolicy(fn *skipper.Function, hpa *skipper.HpaPolicy) *skipper.Function {
	cloned := proto.Clone(fn).(*skipper.Function)
	cloned.SetHpa(hpa)
	return cloned
}

func liveHeartbeat(fn *skipper.Function, inFlight uint32) *skipper.Heartbeat {
	return skipper.Heartbeat_builder{
		Function:         fn,
		Timestamp:        timestamppb.Now(),
		InFlightRequests: new(inFlight),
	}.Build()
}

// readyInstance returns an instance for fn that became ready a long time ago,
// so its CPU metric is always considered eligible for scaling decisions.
func readyInstance(fn *skipper.Function, cpuMilli uint32) *skipper.Instance {
	return skipper.Instance_builder{
		Function:      fn,
		ReadyAt:       timestamppb.New(time.Now().Add(-time.Hour)),
		CpuUsageMilli: new(cpuMilli),
	}.Build()
}

// TestCalculateDesiredInstancesPerFunctionTolerance verifies that two
// functions sharing scaling targets but with different hpa.tolerance produce
// divergent scale decisions under identical observed load: the tighter
// tolerance scales up sooner.
func TestCalculateDesiredInstancesPerFunctionTolerance(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPATolerance = 0.10 // cluster default

	base := fixture.NewFunction(t)
	// Run with one instance whose CPU usage is 5% above target. With the cluster
	// default tolerance (10%), the discrepancy is within tolerance and there is
	// no scaling. With a 1% override, the discrepancy is outside tolerance, so
	// the override triggers a scale up.
	tightFn := withHpaPolicy(base, skipper.HpaPolicy_builder{Tolerance: new(0.01)}.Build())
	loose := base // omits hpa policy entirely

	scale := tightFn.GetScale()
	scale.SetTargetCpuUsageMilli(100)
	scale = loose.GetScale()
	scale.SetTargetCpuUsageMilli(100)

	tightInstances := []*skipper.Instance{readyInstance(tightFn, 105)}
	looseInstances := []*skipper.Instance{readyInstance(loose, 105)}

	tightDesired, _ := calculateDesiredInstancesForMetric(t.Context(), cfg, tightFn, MetricCPU, tightInstances)
	looseDesired, _ := calculateDesiredInstancesForMetric(t.Context(), cfg, loose, MetricCPU, looseInstances)

	assert.Equal(t, looseDesired, 1, "loose tolerance (cluster default 10%%) absorbs the 5%% over-target")
	assert.Assert(t, tightDesired > looseDesired,
		"tight tolerance (1%%) should scale above 1, got %d (vs loose %d)", tightDesired, looseDesired)
}

// TestRecordRecommendationPerFunctionStabilization verifies that two
// supervisors with different hpa.downscale_stabilization values prune their
// stabilization windows on independent schedules. The supervisor with the
// shorter window forgets older recommendations sooner, which means it can
// scale down sooner after the same load dip.
func TestRecordRecommendationPerFunctionStabilization(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPADownscaleStabilization = 5 * time.Minute // cluster default

	base := fixture.NewFunction(t)
	shortFn := withHpaPolicy(base, skipper.HpaPolicy_builder{
		DownscaleStabilization: durationpb.New(30 * time.Second),
	}.Build())

	ctrl := New(cfg, nil, nil, nil)
	shortSup := &Supervisor{ctrl: ctrl}
	shortSup.fn.Store(shortFn)
	clusterSup := &Supervisor{ctrl: ctrl}
	clusterSup.fn.Store(base)

	// Seed both supervisors with a high recommendation 1 minute ago.
	old := time.Now().Add(-time.Minute)
	for _, s := range []*Supervisor{shortSup, clusterSup} {
		s.stabilizationWindow = []Recommendation{{DesiredInstances: 5, Timestamp: old}}
	}

	// Now record a low recommendation. Each supervisor returns the max within
	// its own window. The short-window supervisor has already evicted the old
	// recommendation; the cluster-default supervisor still sees it.
	shortMax := shortSup.recordRecommendation(1)
	clusterMax := clusterSup.recordRecommendation(1)

	assert.Equal(t, shortMax.DesiredInstances, uint32(1),
		"short stabilization window should forget the 1-minute-old high recommendation")
	assert.Equal(t, clusterMax.DesiredInstances, uint32(5),
		"cluster default (5m) should still hold the 1-minute-old high recommendation")
}

// TestCalculateDesiredInstancesPerFunctionInitialReadinessDelay verifies that
// a function with a longer initial-readiness delay excludes a freshly-ready
// pod from its CPU metric, while a function on the cluster default would
// include it.
func TestCalculateDesiredInstancesPerFunctionInitialReadinessDelay(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPAInitialReadinessDelay = 30 * time.Second // cluster default

	base := fixture.NewFunction(t)
	patientFn := withHpaPolicy(base, skipper.HpaPolicy_builder{
		InitialReadinessDelay: durationpb.New(10 * time.Minute),
	}.Build())

	// A pod that became ready 90 seconds ago. Past cluster default (30s), so
	// the cluster-default-only function counts it. The patient function's 10m
	// override still excludes it.
	freshReady := timestamppb.New(time.Now().Add(-90 * time.Second))

	patientInstance := skipper.Instance_builder{
		Function:      patientFn,
		ReadyAt:       freshReady,
		CpuUsageMilli: proto.Uint32(900), // far above target
	}.Build()
	patientFn.GetScale().SetTargetCpuUsageMilli(100)

	clusterFn := proto.Clone(base).(*skipper.Function)
	clusterInstance := skipper.Instance_builder{
		Function:      clusterFn,
		ReadyAt:       freshReady,
		CpuUsageMilli: proto.Uint32(900),
	}.Build()
	clusterFn.GetScale().SetTargetCpuUsageMilli(100)

	patientDesired, _ := calculateDesiredInstancesForMetric(t.Context(), cfg, patientFn, MetricCPU, []*skipper.Instance{patientInstance})
	clusterDesired, _ := calculateDesiredInstancesForMetric(t.Context(), cfg, clusterFn, MetricCPU, []*skipper.Instance{clusterInstance})

	assert.Equal(t, patientDesired, 1,
		"patient function (10m delay) should exclude the 90s-old pod and stay at 1")
	assert.Assert(t, clusterDesired > 1,
		"cluster default (30s delay) should include the 90s-old pod and scale up, got %d", clusterDesired)
}

// TestCalculateDesiredInstancesOmittedHpaPolicy verifies that a function
// omitting the hpa sub-message falls back to cluster flags at every HPA
// decision site -- behavior matches today.
func TestCalculateDesiredInstancesOmittedHpaPolicy(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPATolerance = 0.10
	cfg.HPAInitialReadinessDelay = 30 * time.Second

	fn := fixture.NewFunction(t)
	assert.Assert(t, fn.GetHpa() == nil, "fixture function must omit hpa policy")

	// Tolerance fallback: 5% over-target with cluster default 10% => no scaling.
	fn.GetScale().SetTargetCpuUsageMilli(100)
	tolerantInstance := readyInstance(fn, 105)
	desired, _ := calculateDesiredInstancesForMetric(t.Context(), cfg, fn, MetricCPU, []*skipper.Instance{tolerantInstance})
	assert.Equal(t, desired, 1, "5%% over-target should be within cluster default tolerance")

	// Initial readiness fallback: a pod ready 90s ago is past the 30s cluster
	// default, so its metric is included.
	freshReady := skipper.Instance_builder{
		Function:      fn,
		ReadyAt:       timestamppb.New(time.Now().Add(-90 * time.Second)),
		CpuUsageMilli: proto.Uint32(500),
	}.Build()
	desired, _ = calculateDesiredInstancesForMetric(t.Context(), cfg, fn, MetricCPU, []*skipper.Instance{freshReady})
	assert.Assert(t, desired > 1, "90s-old pod should be included in scaling decisions, got %d", desired)
}

// TestRecordRecommendationOmittedHpaPolicy verifies that a function omitting
// the hpa sub-message uses the cluster default for the stabilization window.
func TestRecordRecommendationOmittedHpaPolicy(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HPADownscaleStabilization = 5 * time.Minute

	ctrl := New(cfg, nil, nil, nil)
	sup := &Supervisor{ctrl: ctrl}
	sup.fn.Store(fixture.NewFunction(t))

	// Recommendation 1 minute ago is well within the 5-minute cluster default,
	// so it should still be in the window.
	old := time.Now().Add(-time.Minute)
	sup.stabilizationWindow = []Recommendation{{DesiredInstances: 7, Timestamp: old}}

	max := sup.recordRecommendation(1)
	assert.Equal(t, max.DesiredInstances, uint32(7),
		"function without hpa policy should use cluster default (5m) and keep the 1m-old recommendation")
}

// liveScaleDecision drives calculateDesiredInstances with a fresh heartbeat so
// no heartbeat-timeout reason fires; useful for asserting protectionPeriod and
// other downstream logic.
func liveScaleDecision(t *testing.T, cfg *Config, fn *skipper.Function, inFlight uint32, instances []*skipper.Instance) *skipper.ScaleDecision {
	t.Helper()
	return calculateDesiredInstances(t.Context(), cfg, fn, liveHeartbeat(fn, inFlight), instances)
}

// (sanity) the helper builds a fresh decision; ensure it doesn't return a
// heartbeat-timeout decision.
func TestLiveScaleDecisionHelper(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	fn := fixture.NewFunction(t)
	dec := liveScaleDecision(t, cfg, fn, 0, nil)
	assert.Assert(t, dec.GetReason() != skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT)
}
