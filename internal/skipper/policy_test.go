package skipper

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gotest.tools/v3/assert"
)

func TestScaleResolversFlatPreferred(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		assignment             *Assignment
		wantMin                uint32
		wantMax                uint32
		wantTargetCPU          uint32
		wantTargetMem          uint32
		wantTargetInFlight     uint32
		wantHasMin, wantHasMax bool
	}{
		{
			name: "nested only",
			assignment: Assignment_builder{
				Namespace:  new("n"),
				Deployment: new("d"),
				Tenant:     new("t"),
				Scale: Scale_builder{
					MinInstances:           proto.Uint32(1),
					MaxInstances:           proto.Uint32(10),
					TargetCpuUsageMilli:    proto.Uint32(500),
					TargetMemoryUsageMib:   proto.Uint32(256),
					TargetInFlightRequests: proto.Uint32(100),
				}.Build(),
			}.Build(),
			wantMin: 1, wantMax: 10, wantTargetCPU: 500, wantTargetMem: 256, wantTargetInFlight: 100,
			wantHasMin: true, wantHasMax: true,
		},
		{
			name: "flat only",
			assignment: Assignment_builder{
				Namespace:                   new("n"),
				Deployment:                  new("d"),
				Tenant:                      new("t"),
				ScaleMinInstances:           proto.Uint32(2),
				ScaleMaxInstances:           proto.Uint32(20),
				ScaleTargetCpuMillicores:    proto.Uint32(600),
				ScaleTargetMemoryMebibytes:  proto.Uint32(512),
				ScaleTargetInFlightRequests: proto.Uint32(200),
			}.Build(),
			wantMin: 2, wantMax: 20, wantTargetCPU: 600, wantTargetMem: 512, wantTargetInFlight: 200,
			wantHasMin: true, wantHasMax: true,
		},
		{
			name: "both shapes, flat wins",
			assignment: Assignment_builder{
				Namespace:  new("n"),
				Deployment: new("d"),
				Tenant:     new("t"),
				Scale: Scale_builder{
					MinInstances:           proto.Uint32(1),
					MaxInstances:           proto.Uint32(10),
					TargetCpuUsageMilli:    proto.Uint32(500),
					TargetMemoryUsageMib:   proto.Uint32(256),
					TargetInFlightRequests: proto.Uint32(100),
				}.Build(),
				ScaleMinInstances:           proto.Uint32(3),
				ScaleMaxInstances:           proto.Uint32(30),
				ScaleTargetCpuMillicores:    proto.Uint32(700),
				ScaleTargetMemoryMebibytes:  proto.Uint32(1024),
				ScaleTargetInFlightRequests: proto.Uint32(300),
			}.Build(),
			wantMin: 3, wantMax: 30, wantTargetCPU: 700, wantTargetMem: 1024, wantTargetInFlight: 300,
			wantHasMin: true, wantHasMax: true,
		},
		{
			name: "hybrid: min from nested, max from flat",
			assignment: Assignment_builder{
				Namespace:  new("n"),
				Deployment: new("d"),
				Tenant:     new("t"),
				Scale: Scale_builder{
					MinInstances: proto.Uint32(2),
				}.Build(),
				ScaleMaxInstances: proto.Uint32(15),
			}.Build(),
			wantMin: 2, wantMax: 15,
			wantHasMin: true, wantHasMax: true,
		},
		{
			name: "neither shape present",
			assignment: Assignment_builder{
				Namespace:  new("n"),
				Deployment: new("d"),
				Tenant:     new("t"),
			}.Build(),
			wantHasMin: false, wantHasMax: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotMin, hasMin := tc.assignment.resolvedScaleMin()
			gotMax, hasMax := tc.assignment.resolvedScaleMax()

			assert.Equal(t, hasMin, tc.wantHasMin, "resolvedScaleMin presence")
			assert.Equal(t, hasMax, tc.wantHasMax, "resolvedScaleMax presence")
			if tc.wantHasMin {
				assert.Equal(t, gotMin, tc.wantMin)
				assert.Equal(t, tc.assignment.ScaleMinInstances(), tc.wantMin)
			}
			if tc.wantHasMax {
				assert.Equal(t, gotMax, tc.wantMax)
				assert.Equal(t, tc.assignment.ScaleMaxInstances(), tc.wantMax)
			}

			assert.Equal(t, tc.assignment.ScaleTargetCPUMillicores(), tc.wantTargetCPU)
			assert.Equal(t, tc.assignment.ScaleTargetMemoryMebibytes(), tc.wantTargetMem)
			assert.Equal(t, tc.assignment.ScaleTargetInFlightRequests(), tc.wantTargetInFlight)
		})
	}
}

func TestDurationResolversFallback(t *testing.T) {
	t.Parallel()

	cluster := 30 * time.Second

	tests := []struct {
		name      string
		set       *Assignment
		resolve   func(a *Assignment) time.Duration
		wantUnset time.Duration
		wantSet   time.Duration
	}{
		{
			name: "HeartbeatTimeout",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances: proto.Uint32(1),
				HeartbeatTimeout:  durationpb.New(120 * time.Second),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.HeartbeatTimeout(cluster) },
			wantUnset: cluster,
			wantSet:   120 * time.Second,
		},
		{
			name: "AssignTimeout",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances: proto.Uint32(1),
				AssignTimeout:     durationpb.New(45 * time.Second),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.AssignTimeout(cluster) },
			wantUnset: cluster,
			wantSet:   45 * time.Second,
		},
		{
			name: "AssignTokenTTL",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances: proto.Uint32(1),
				AssignTokenTtl:    durationpb.New(1 * time.Hour),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.AssignTokenTTL(cluster) },
			wantUnset: cluster,
			wantSet:   1 * time.Hour,
		},
		{
			name: "ScaleDownscaleStabilization",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances:           proto.Uint32(1),
				ScaleDownscaleStabilization: durationpb.New(2 * time.Minute),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.ScaleDownscaleStabilization(cluster) },
			wantUnset: cluster,
			wantSet:   2 * time.Minute,
		},
		{
			name: "ScaleInitialReadinessDelay",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances:          proto.Uint32(1),
				ScaleInitialReadinessDelay: durationpb.New(15 * time.Second),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.ScaleInitialReadinessDelay(cluster) },
			wantUnset: cluster,
			wantSet:   15 * time.Second,
		},
		{
			name: "RetryMinBackoff",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances: proto.Uint32(1),
				RetryMinBackoff:   durationpb.New(200 * time.Millisecond),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.RetryMinBackoff(cluster) },
			wantUnset: cluster,
			wantSet:   200 * time.Millisecond,
		},
		{
			name: "RetryMaxBackoff",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances: proto.Uint32(1),
				RetryMaxBackoff:   durationpb.New(10 * time.Second),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.RetryMaxBackoff(cluster) },
			wantUnset: cluster,
			wantSet:   10 * time.Second,
		},
		{
			name: "HeartbeatInterval (placeholder)",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances: proto.Uint32(1),
				HeartbeatInterval: durationpb.New(7 * time.Second),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.HeartbeatInterval(cluster) },
			wantUnset: cluster,
			wantSet:   7 * time.Second,
		},
		{
			name: "TransportDialTimeout (placeholder)",
			set: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances:    proto.Uint32(1),
				TransportDialTimeout: durationpb.New(500 * time.Millisecond),
			}.Build(),
			resolve:   func(a *Assignment) time.Duration { return a.TransportDialTimeout(cluster) },
			wantUnset: cluster,
			wantSet:   500 * time.Millisecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			empty := Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				ScaleMaxInstances: proto.Uint32(1),
			}.Build()
			assert.Equal(t, tc.resolve(empty), tc.wantUnset, "fallback to cluster default")
			assert.Equal(t, tc.resolve(tc.set), tc.wantSet, "tenant override")
		})
	}
}

func TestRetryMaxAttemptsResolver(t *testing.T) {
	t.Parallel()

	cluster := 6

	empty := Assignment_builder{
		Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
		ScaleMaxInstances: proto.Uint32(1),
	}.Build()
	override := Assignment_builder{
		Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
		ScaleMaxInstances: proto.Uint32(1),
		RetryMaxAttempts:  proto.Uint32(3),
	}.Build()

	assert.Equal(t, empty.RetryMaxAttempts(cluster), cluster, "fallback to cluster default")
	assert.Equal(t, override.RetryMaxAttempts(cluster), 3, "tenant override")
}

func TestScaleToleranceResolver(t *testing.T) {
	t.Parallel()

	cluster := 0.1

	empty := Assignment_builder{
		Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
		ScaleMaxInstances: proto.Uint32(1),
	}.Build()
	override := Assignment_builder{
		Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
		ScaleMaxInstances: proto.Uint32(1),
		ScaleTolerance:    new(0.25),
	}.Build()

	assert.Equal(t, empty.ScaleTolerance(cluster), cluster, "fallback to cluster default")
	assert.Equal(t, override.ScaleTolerance(cluster), 0.25, "tenant override")
}

func TestPlaceholderEnumResolvers(t *testing.T) {
	t.Parallel()

	// Default (unset) — should return the proto3 default (zero enum value).
	a := Assignment_builder{
		Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
		ScaleMaxInstances: proto.Uint32(1),
	}.Build()
	assert.Equal(t, a.ZoneSpread(), ZoneSpread_ZONE_SPREAD_UNSPECIFIED)
	assert.Equal(t, a.ZoneAffinity(), ZoneAffinity_ZONE_AFFINITY_UNSPECIFIED)
	assert.Equal(t, a.RetryBackpressure(), Backpressure_BACKPRESSURE_UNSPECIFIED)

	// Explicit override.
	override := Assignment_builder{
		Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
		ScaleMaxInstances: proto.Uint32(1),
		ZoneSpread:        ZoneSpread_ZONE_SPREAD_PREFERRED.Enum(),
		ZoneAffinity:      ZoneAffinity_ZONE_AFFINITY_REQUIRED.Enum(),
		RetryBackpressure: Backpressure_BACKPRESSURE_RETRY.Enum(),
	}.Build()
	assert.Equal(t, override.ZoneSpread(), ZoneSpread_ZONE_SPREAD_PREFERRED)
	assert.Equal(t, override.ZoneAffinity(), ZoneAffinity_ZONE_AFFINITY_REQUIRED)
	assert.Equal(t, override.RetryBackpressure(), Backpressure_BACKPRESSURE_RETRY)
}

func TestValidatePolicyFields(t *testing.T) {
	t.Parallel()

	base := func(modify func(b *Assignment_builder)) *Assignment {
		b := Assignment_builder{
			Namespace:         new("n"),
			Deployment:        new("d"),
			Tenant:            new("t"),
			ScaleMaxInstances: proto.Uint32(1),
		}
		modify(&b)
		return b.Build()
	}

	tests := []struct {
		name       string
		assignment *Assignment
		wantErr    string
	}{
		{name: "flat-only valid", assignment: base(func(b *Assignment_builder) {}), wantErr: ""},
		{name: "nested-only valid", assignment: Assignment_builder{
			Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
			Scale: Scale_builder{MaxInstances: proto.Uint32(1)}.Build(),
		}.Build(), wantErr: ""},
		{
			name: "hybrid min flat / max nested - invalid bounds",
			assignment: Assignment_builder{
				Namespace: new("n"), Deployment: new("d"), Tenant: new("t"),
				Scale:             Scale_builder{MaxInstances: proto.Uint32(3)}.Build(),
				ScaleMinInstances: proto.Uint32(5),
			}.Build(),
			wantErr: "scale.min_instances (5) must be <= scale.max_instances (3)",
		},
		{
			name:       "no scale at all",
			assignment: Assignment_builder{Namespace: new("n"), Deployment: new("d"), Tenant: new("t")}.Build(),
			wantErr:    "missing scale",
		},
		{name: "negative scale_tolerance", assignment: base(func(b *Assignment_builder) {
			b.ScaleTolerance = new(-0.1)
		}), wantErr: "scale_tolerance"},
		{name: "negative heartbeat_timeout", assignment: base(func(b *Assignment_builder) {
			b.HeartbeatTimeout = durationpb.New(-1 * time.Second)
		}), wantErr: "heartbeat_timeout"},
		{name: "zero heartbeat_timeout", assignment: base(func(b *Assignment_builder) {
			b.HeartbeatTimeout = durationpb.New(0)
		}), wantErr: "heartbeat_timeout must be > 0"},
		{name: "zero assign_timeout", assignment: base(func(b *Assignment_builder) {
			b.AssignTimeout = durationpb.New(0)
		}), wantErr: "assign_timeout must be > 0"},
		{name: "zero assign_token_ttl", assignment: base(func(b *Assignment_builder) {
			b.AssignTokenTtl = durationpb.New(0)
		}), wantErr: "assign_token_ttl must be > 0"},
		{name: "zero retry_max_attempts", assignment: base(func(b *Assignment_builder) {
			b.RetryMaxAttempts = proto.Uint32(0)
		}), wantErr: "retry_max_attempts must be > 0"},
		{name: "negative retry_min_backoff", assignment: base(func(b *Assignment_builder) {
			b.RetryMinBackoff = durationpb.New(-1 * time.Millisecond)
		}), wantErr: "retry_min_backoff"},
		{name: "inverted retry backoff bounds", assignment: base(func(b *Assignment_builder) {
			b.RetryMinBackoff = durationpb.New(5 * time.Second)
			b.RetryMaxBackoff = durationpb.New(1 * time.Second)
		}), wantErr: "retry_min_backoff (5s) must be <= retry_max_backoff (1s)"},
		{name: "negative scale_downscale_stabilization", assignment: base(func(b *Assignment_builder) {
			b.ScaleDownscaleStabilization = durationpb.New(-1 * time.Second)
		}), wantErr: "scale_downscale_stabilization"},
		{name: "negative scale_initial_readiness_delay", assignment: base(func(b *Assignment_builder) {
			b.ScaleInitialReadinessDelay = durationpb.New(-1 * time.Second)
		}), wantErr: "scale_initial_readiness_delay"},
		{name: "negative transport_dial_timeout", assignment: base(func(b *Assignment_builder) {
			b.TransportDialTimeout = durationpb.New(-1 * time.Millisecond)
		}), wantErr: "transport_dial_timeout"},
		{name: "negative heartbeat_interval (placeholder)", assignment: base(func(b *Assignment_builder) {
			b.HeartbeatInterval = durationpb.New(-1 * time.Second)
		}), wantErr: "heartbeat_interval"},
		// Placeholder enum: UNSPECIFIED is valid (placeholder-knob policy:
		// Validate is wired-state-agnostic; followups treat UNSPECIFIED as
		// fall back to cluster default).
		{name: "zone_spread unspecified accepted", assignment: base(func(b *Assignment_builder) {
			b.ZoneSpread = ZoneSpread_ZONE_SPREAD_UNSPECIFIED.Enum()
		}), wantErr: ""},
		{name: "zone_spread explicit value accepted", assignment: base(func(b *Assignment_builder) {
			b.ZoneSpread = ZoneSpread_ZONE_SPREAD_REQUIRED.Enum()
		}), wantErr: ""},
		// Well-formed placeholder values pass.
		{name: "transport_dial_timeout 100ms accepted", assignment: base(func(b *Assignment_builder) {
			b.TransportDialTimeout = durationpb.New(100 * time.Millisecond)
		}), wantErr: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.assignment.Validate()
			if tc.wantErr == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
