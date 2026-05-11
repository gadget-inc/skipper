package router

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gotest.tools/v3/assert"
)

// TestRetryMaxAttemptsOverride confirms the router's RoundTrip retry loop
// honors a per-tenant retry_max_attempts override.
func TestRetryMaxAttemptsOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		clusterMax       int
		assignment       func() *skipper.Assignment
		wantAttemptCount int32
		wantStatus       int
	}{
		{
			name:       "cluster default applies when assignment unset",
			clusterMax: 3,
			assignment: func() *skipper.Assignment {
				return retryAssignment(t, nil)
			},
			wantAttemptCount: 3,
			wantStatus:       http.StatusBadGateway,
		},
		{
			name:       "tenant override below cluster default reduces attempts",
			clusterMax: 5,
			assignment: func() *skipper.Assignment {
				return retryAssignment(t, func(b *skipper.Assignment_builder) {
					b.RetryMaxAttempts = proto.Uint32(2)
				})
			},
			wantAttemptCount: 2,
			wantStatus:       http.StatusBadGateway,
		},
		{
			name:       "tenant override above cluster default raises attempts",
			clusterMax: 2,
			assignment: func() *skipper.Assignment {
				return retryAssignment(t, func(b *skipper.Assignment_builder) {
					b.RetryMaxAttempts = proto.Uint32(4)
				})
			},
			wantAttemptCount: 4,
			wantStatus:       http.StatusBadGateway,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := tc.assignment()

			mcc := fixture.NewMockControllerClient(t)
			var attempts atomic.Int32
			mcc.HandleInstance(func(ctx context.Context, a *skipper.Assignment, excludeInstanceNames ...string) (*skipper.Instance, error) {
				attempts.Add(1)
				inst := skipper.Instance_builder{
					Assignment: a,
					Name:       new("failing-instance"),
					Addr:       new("127.0.0.1:1"), // unroutable
				}.Build()
				return inst, nil
			})

			cfg := testConfig()
			cfg.MaxRoundTripAttempts = tc.clusterMax
			// Keep the backoff loop snappy so the test stays fast.
			cfg.RoundTripRetryMinTimeout = 1 * time.Millisecond
			cfg.RoundTripRetryMaxTimeout = 5 * time.Millisecond
			router := New(cfg, mcc)
			router.roundTripper = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return nil, &net.OpError{Op: "dial", Err: errors.New("synthetic dial error")}
			})

			rw := httptest.NewRecorder()
			req := fixture.NewAssignmentRequest(t, a, http.MethodGet, "/", nil)
			router.ServeHTTP(rw, req)

			assert.Equal(t, rw.Code, tc.wantStatus)
			assert.Equal(t, attempts.Load(), tc.wantAttemptCount)
		})
	}
}

// TestRetryBackoffOverride confirms the router's backoff timer honors a
// per-tenant retry_min_backoff override by observing total request latency
// over multiple attempts.
func TestRetryBackoffOverride(t *testing.T) {
	t.Parallel()

	// A tenant who sets a long min_backoff should observe a noticeably longer
	// total retry duration than a tenant whose request uses the (small) cluster
	// default. We compare the slow-tenant latency against a generous lower
	// bound rather than the fast-tenant latency to keep the test stable on
	// loaded CI runners.
	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, a *skipper.Assignment, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return skipper.Instance_builder{
			Assignment: a,
			Name:       new("failing-instance"),
			Addr:       new("127.0.0.1:1"),
		}.Build(), nil
	})

	cfg := testConfig()
	cfg.MaxRoundTripAttempts = 3
	cfg.RoundTripRetryMinTimeout = 1 * time.Millisecond
	cfg.RoundTripRetryMaxTimeout = 5 * time.Millisecond
	router := New(cfg, mcc)
	router.roundTripper = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Err: errors.New("synthetic dial error")}
	})

	// Tenant override: 50ms min, 200ms max — backoff totals across 2 retry
	// gaps should be at least 100ms even on the fastest hardware.
	overrideAssignment := retryAssignment(t, func(b *skipper.Assignment_builder) {
		b.RetryMaxAttempts = proto.Uint32(3)
		b.RetryMinBackoff = durationpb.New(50 * time.Millisecond)
		b.RetryMaxBackoff = durationpb.New(200 * time.Millisecond)
	})

	start := time.Now()
	rw := httptest.NewRecorder()
	req := fixture.NewAssignmentRequest(t, overrideAssignment, http.MethodGet, "/", nil)
	router.ServeHTTP(rw, req)
	elapsed := time.Since(start)

	assert.Equal(t, rw.Code, http.StatusBadGateway)
	assert.Assert(t, elapsed >= 100*time.Millisecond,
		"3-attempt run with 50ms min backoff should take at least 100ms; got %s", elapsed)
}

// TestPlaceholderKnobsNoEffectOnRouter confirms that setting placeholder
// knobs (transport_*, zone_*, retry_backpressure, retry_status_codes,
// heartbeat_interval, assign_path) leaves router routing decisions identical
// to those for an assignment that did not set them.
func TestPlaceholderKnobsNoEffectOnRouter(t *testing.T) {
	t.Parallel()

	mcc := fixture.NewMockControllerClient(t)

	successInstance := func(t *testing.T, a *skipper.Assignment) *skipper.Instance {
		return fixture.NewInstance(t, a, func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte("ok"))
		})
	}

	mcc.HandleInstance(func(ctx context.Context, a *skipper.Assignment, excludeInstanceNames ...string) (*skipper.Instance, error) {
		return successInstance(t, a), nil
	})

	cfg := testConfig()
	router := New(cfg, mcc)

	baseAssignment := retryAssignment(t, nil)
	withPlaceholders := retryAssignment(t, func(b *skipper.Assignment_builder) {
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

	for _, a := range []*skipper.Assignment{baseAssignment, withPlaceholders} {
		rw := httptest.NewRecorder()
		req := fixture.NewAssignmentRequest(t, a, http.MethodGet, "/", nil)
		router.ServeHTTP(rw, req)
		assert.Equal(t, rw.Code, http.StatusOK)
		assert.Equal(t, rw.Body.String(), "ok")
	}
}

func retryAssignment(t *testing.T, modify func(b *skipper.Assignment_builder)) *skipper.Assignment {
	t.Helper()
	b := skipper.Assignment_builder{
		Namespace:  new("ns"),
		Deployment: new("deploy"),
		Tenant:     new("tenant"),
		Scale: skipper.Scale_builder{
			MinInstances: proto.Uint32(1),
			MaxInstances: proto.Uint32(10),
		}.Build(),
	}
	if modify != nil {
		modify(&b)
	}
	return b.Build()
}
