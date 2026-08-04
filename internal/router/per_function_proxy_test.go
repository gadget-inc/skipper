package router

import (
	"context"
	"errors"
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

func withProxyPolicy(fn *skipper.Function, proxyPolicy *skipper.ProxyPolicy) *skipper.Function {
	cloned := proto.Clone(fn).(*skipper.Function)
	cloned.SetProxy(proxyPolicy)
	return cloned
}

// failingControllerClient counts Instance calls and always returns a temporary
// error so RoundTrip exhausts every allowed attempt before returning.
type failingControllerClient struct {
	*fixture.MockControllerClient
	calls atomic.Int32
}

func newFailingClient(t *testing.T) *failingControllerClient {
	c := &failingControllerClient{MockControllerClient: fixture.NewMockControllerClient(t)}
	c.HandleInstance(func(_ context.Context, _ *skipper.Function, _ ...string) (*skipper.Instance, error) {
		c.calls.Add(1)
		return nil, errors.New("temporary error")
	})
	return c
}

func runRoundTripExhaustion(t *testing.T, cfg *Config, fn *skipper.Function) (int, error) {
	t.Helper()
	mcc := newFailingClient(t)
	router := New(cfg, mcc.MockControllerClient)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	fn.SetHeader(req)
	req = req.WithContext(withFunction(t.Context(), fn))

	_, err := router.RoundTrip(req)
	return int(mcc.calls.Load()), err
}

// TestRoundTripPerFunctionMaxAttemptsCapsBelowClusterDefault verifies that a
// request whose Function header sets proxy.max_attempts = 1 stops after one
// attempt, regardless of --max-round-trip-attempts.
func TestRoundTripPerFunctionMaxAttemptsCapsBelowClusterDefault(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.MaxRoundTripAttempts = 6
	cfg.RoundTripRetryMinTimeout = time.Microsecond
	cfg.RoundTripRetryMaxTimeout = time.Millisecond

	fn := withProxyPolicy(fixture.NewFunction(t), skipper.ProxyPolicy_builder{
		MaxAttempts: new(uint32(1)),
	}.Build())

	calls, err := runRoundTripExhaustion(t, cfg, fn)
	assert.Assert(t, err != nil, "expected error after exhausting attempts")
	assert.Equal(t, calls, 1, "proxy.max_attempts=1 should stop after 1 attempt")
}

// TestRoundTripPerFunctionMaxAttemptsExceedsClusterDefault verifies that a
// request whose Function header sets proxy.max_attempts = 10 attempts up to
// 10 times even when --max-round-trip-attempts is 6.
func TestRoundTripPerFunctionMaxAttemptsExceedsClusterDefault(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.MaxRoundTripAttempts = 6
	cfg.RoundTripRetryMinTimeout = time.Microsecond
	cfg.RoundTripRetryMaxTimeout = time.Millisecond

	fn := withProxyPolicy(fixture.NewFunction(t), skipper.ProxyPolicy_builder{
		MaxAttempts: new(uint32(10)),
	}.Build())

	calls, err := runRoundTripExhaustion(t, cfg, fn)
	assert.Assert(t, err != nil, "expected error after exhausting attempts")
	assert.Equal(t, calls, 10, "proxy.max_attempts=10 should run 10 attempts even when cluster default is 6")
}

// TestRoundTripOmittedProxyPolicyFallsBackToClusterDefault verifies that a
// request omitting the proxy sub-message uses --max-round-trip-attempts.
func TestRoundTripOmittedProxyPolicyFallsBackToClusterDefault(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.MaxRoundTripAttempts = 4
	cfg.RoundTripRetryMinTimeout = time.Microsecond
	cfg.RoundTripRetryMaxTimeout = time.Millisecond

	fn := fixture.NewFunction(t)
	assert.Assert(t, fn.GetProxy() == nil, "fixture function must omit proxy policy")

	calls, err := runRoundTripExhaustion(t, cfg, fn)
	assert.Assert(t, err != nil, "expected error after exhausting attempts")
	assert.Equal(t, calls, 4, "omitted proxy policy should fall back to cluster default of 4")
}

// TestCalculateBackoffPerFunctionBounds verifies that per-function
// proxy.retry_min_backoff / proxy.retry_max_backoff drive the curve when
// passed in -- backoff stays within the function's bounds.
func TestCalculateBackoffPerFunctionBounds(t *testing.T) {
	t.Parallel()

	fnMinBackoff := 50 * time.Millisecond
	fnMaxBackoff := 200 * time.Millisecond

	for range 100 {
		got := calculateBackoff(2, fnMinBackoff, fnMaxBackoff)
		assert.Assert(t, got >= fnMinBackoff*2, "attempt 2 backoff %s below per-function min*2 = %s", got, fnMinBackoff*2)
		assert.Assert(t, got <= fnMaxBackoff, "attempt 2 backoff %s above per-function max = %s", got, fnMaxBackoff)
	}
}

// TestHeartbeatStateUpdateFunctionOnPolicyChange exercises the pointer-identity
// swap path on a policy change. A later request whose Function header carries a
// different proxy policy goes through the LRU header cache as a distinct
// pointer, and updateFunction stores it.
func TestHeartbeatStateUpdateFunctionOnPolicyChange(t *testing.T) {
	t.Parallel()

	base := fixture.NewFunction(t)
	state := newHeartbeatState(base)

	withRetries := withProxyPolicy(base, skipper.ProxyPolicy_builder{
		MaxAttempts:     new(uint32(10)),
		RetryMinBackoff: durationpb.New(50 * time.Millisecond),
		RetryMaxBackoff: durationpb.New(time.Second),
	}.Build())
	assert.Equal(t, base.Hash(), withRetries.Hash())
	assert.Assert(t, !proto.Equal(base, withRetries))

	state.updateFunction(withRetries)

	hb := state.toProto()
	assert.Assert(t, proto.Equal(hb.GetFunction(), withRetries),
		"updateFunction should swap to the request that carries the new proxy policy")
	assert.Equal(t, hb.GetFunction().MaxRoundTripAttempts(0), uint32(10))
}

// TestRoundTripPerFunctionInvertedBackoffBoundsClamps guards the
// hybrid-resolution case: a tenant sets only retry_min_backoff (e.g., 5s)
// while leaving retry_max_backoff unset, and the cluster default for max is
// shorter (1s). Function.Validate cannot catch this because the function-side
// value is consistent in isolation. The router must clamp at the call site so
// the resolved pair is never inverted; otherwise calculateBackoff's min(...)
// silently caps at the cluster max and the tenant's minimum is lost. The
// clamp also reports whether it fired so the caller can warn the operator
// that a tenant override was demoted to the cluster ceiling.
func TestRoundTripPerFunctionInvertedBackoffBoundsClamps(t *testing.T) {
	t.Parallel()

	t.Run("inverted", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig()
		cfg.RoundTripRetryMinTimeout = 100 * time.Millisecond
		cfg.RoundTripRetryMaxTimeout = time.Second // cluster cap

		// Tenant sets only min, larger than the cluster max.
		fn := withProxyPolicy(fixture.NewFunction(t), skipper.ProxyPolicy_builder{
			RetryMinBackoff: durationpb.New(5 * time.Second),
		}.Build())

		// Simulate the resolution that happens at the top of RoundTrip.
		minBackoff := fn.RetryMinBackoff(cfg.RoundTripRetryMinTimeout)
		maxBackoff := fn.RetryMaxBackoff(cfg.RoundTripRetryMaxTimeout)
		resolvedMin, resolvedMax, wasInverted := clampBackoffBounds(minBackoff, maxBackoff)

		assert.Assert(t, wasInverted,
			"inverted resolved pair (tenant min=%s, cluster max=%s) must report wasInverted=true",
			minBackoff, maxBackoff)
		assert.Assert(t, resolvedMin <= resolvedMax,
			"clamped bounds must satisfy min <= max; got min=%s max=%s", resolvedMin, resolvedMax)
		assert.Equal(t, resolvedMax, cfg.RoundTripRetryMaxTimeout,
			"cluster max ceiling stays in force when the tenant did not override it")

		// Backoff must be within the clamped pair on every attempt.
		for attempt := 1; attempt < 6; attempt++ {
			got := calculateBackoff(attempt, resolvedMin, resolvedMax)
			assert.Assert(t, got <= resolvedMax,
				"attempt %d backoff %s exceeded resolved max %s", attempt, got, resolvedMax)
		}
	})

	t.Run("ordered", func(t *testing.T) {
		t.Parallel()

		// Already-ordered pair: clamp does not fire and wasInverted is false.
		resolvedMin, resolvedMax, wasInverted := clampBackoffBounds(100*time.Millisecond, time.Second)

		assert.Assert(t, !wasInverted,
			"already-ordered pair must report wasInverted=false")
		assert.Equal(t, resolvedMin, 100*time.Millisecond)
		assert.Equal(t, resolvedMax, time.Second)
	})
}
