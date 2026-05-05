package skipper

import (
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"gotest.tools/v3/assert"
)

func TestResolverFallsBackToClusterDefault(t *testing.T) {
	t.Parallel()

	clusterFloat := 0.10
	clusterDuration := 30 * time.Second
	clusterUint32 := uint32(6)

	// All resolvers must return clusterDefault when the function-side value
	// is zero or the policy sub-message is unset.
	cases := []struct {
		name string
		fn   *Function
	}{
		{name: "nil sub-messages", fn: Function_builder{}.Build()},
		{
			name: "zero-valued sub-messages",
			fn: Function_builder{
				Hpa:       HpaPolicy_builder{}.Build(),
				Heartbeat: HeartbeatPolicy_builder{}.Build(),
				Proxy:     ProxyPolicy_builder{}.Build(),
				Lifecycle: LifecyclePolicy_builder{}.Build(),
			}.Build(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.fn.HPATolerance(clusterFloat), clusterFloat)
			assert.Equal(t, tc.fn.HPADownscaleStabilization(clusterDuration), clusterDuration)
			assert.Equal(t, tc.fn.HPAInitialReadinessDelay(clusterDuration), clusterDuration)
			assert.Equal(t, tc.fn.HeartbeatTimeout(clusterDuration), clusterDuration)
			assert.Equal(t, tc.fn.MaxRoundTripAttempts(clusterUint32), clusterUint32)
			assert.Equal(t, tc.fn.RetryMinBackoff(clusterDuration), clusterDuration)
			assert.Equal(t, tc.fn.RetryMaxBackoff(clusterDuration), clusterDuration)
			assert.Equal(t, tc.fn.AssignTimeout(clusterDuration), clusterDuration)
			assert.Equal(t, tc.fn.TokenTTL(clusterDuration), clusterDuration)
		})
	}
}

func TestResolverReturnsFunctionValue(t *testing.T) {
	t.Parallel()

	fnTolerance := 0.05
	fnDuration := 5 * time.Minute
	fnUint32 := uint32(10)

	clusterFloat := 0.20
	clusterDuration := 30 * time.Second
	clusterUint32 := uint32(6)

	fn := Function_builder{
		Hpa: HpaPolicy_builder{
			Tolerance:              new(fnTolerance),
			DownscaleStabilization: durationpb.New(fnDuration),
			InitialReadinessDelay:  durationpb.New(fnDuration),
		}.Build(),
		Heartbeat: HeartbeatPolicy_builder{
			Timeout: durationpb.New(fnDuration),
		}.Build(),
		Proxy: ProxyPolicy_builder{
			MaxAttempts:     new(fnUint32),
			RetryMinBackoff: durationpb.New(fnDuration),
			RetryMaxBackoff: durationpb.New(fnDuration * 2),
		}.Build(),
		Lifecycle: LifecyclePolicy_builder{
			AssignTimeout: durationpb.New(fnDuration),
			TokenTtl:      durationpb.New(fnDuration),
		}.Build(),
	}.Build()

	assert.Equal(t, fn.HPATolerance(clusterFloat), fnTolerance)
	assert.Equal(t, fn.HPADownscaleStabilization(clusterDuration), fnDuration)
	assert.Equal(t, fn.HPAInitialReadinessDelay(clusterDuration), fnDuration)
	assert.Equal(t, fn.HeartbeatTimeout(clusterDuration), fnDuration)
	assert.Equal(t, fn.MaxRoundTripAttempts(clusterUint32), fnUint32)
	assert.Equal(t, fn.RetryMinBackoff(clusterDuration), fnDuration)
	assert.Equal(t, fn.RetryMaxBackoff(clusterDuration), fnDuration*2)
	assert.Equal(t, fn.AssignTimeout(clusterDuration), fnDuration)
	assert.Equal(t, fn.TokenTTL(clusterDuration), fnDuration)
}

func TestValidatePolicies(t *testing.T) {
	t.Parallel()

	baseScale := Scale_builder{MinInstances: new(uint32(1)), MaxInstances: new(uint32(10))}.Build()
	build := func(mut func(b *Function_builder)) *Function {
		b := Function_builder{
			Namespace:  new("ns"),
			Deployment: new("d"),
			Tenant:     new("t"),
			Scale:      baseScale,
		}
		mut(&b)
		return b.Build()
	}

	cases := []struct {
		name    string
		fn      *Function
		wantErr string
	}{
		{
			name: "all policies omitted",
			fn:   build(func(*Function_builder) {}),
		},
		{
			name: "all policies zero-valued",
			fn: build(func(b *Function_builder) {
				b.Hpa = HpaPolicy_builder{}.Build()
				b.Heartbeat = HeartbeatPolicy_builder{}.Build()
				b.Proxy = ProxyPolicy_builder{}.Build()
				b.Lifecycle = LifecyclePolicy_builder{}.Build()
			}),
		},
		{
			name: "valid populated policies",
			fn: build(func(b *Function_builder) {
				b.Hpa = HpaPolicy_builder{
					Tolerance:              new(0.05),
					DownscaleStabilization: durationpb.New(time.Minute),
					InitialReadinessDelay:  durationpb.New(time.Minute),
				}.Build()
				b.Heartbeat = HeartbeatPolicy_builder{Timeout: durationpb.New(time.Minute)}.Build()
				b.Proxy = ProxyPolicy_builder{
					MaxAttempts:     new(uint32(5)),
					RetryMinBackoff: durationpb.New(10 * time.Millisecond),
					RetryMaxBackoff: durationpb.New(time.Second),
				}.Build()
				b.Lifecycle = LifecyclePolicy_builder{
					AssignTimeout: durationpb.New(time.Minute),
					TokenTtl:      durationpb.New(time.Hour),
				}.Build()
			}),
		},
		{
			name: "negative hpa.tolerance",
			fn: build(func(b *Function_builder) {
				b.Hpa = HpaPolicy_builder{Tolerance: new(-0.5)}.Build()
			}),
			wantErr: "hpa.tolerance",
		},
		{
			name: "negative hpa.downscale_stabilization",
			fn: build(func(b *Function_builder) {
				b.Hpa = HpaPolicy_builder{DownscaleStabilization: durationpb.New(-time.Second)}.Build()
			}),
			wantErr: "hpa.downscale_stabilization",
		},
		{
			name: "negative hpa.initial_readiness_delay",
			fn: build(func(b *Function_builder) {
				b.Hpa = HpaPolicy_builder{InitialReadinessDelay: durationpb.New(-time.Second)}.Build()
			}),
			wantErr: "hpa.initial_readiness_delay",
		},
		{
			name: "negative heartbeat.timeout",
			fn: build(func(b *Function_builder) {
				b.Heartbeat = HeartbeatPolicy_builder{Timeout: durationpb.New(-time.Second)}.Build()
			}),
			wantErr: "heartbeat.timeout",
		},
		{
			name: "negative proxy.retry_min_backoff",
			fn: build(func(b *Function_builder) {
				b.Proxy = ProxyPolicy_builder{RetryMinBackoff: durationpb.New(-time.Second)}.Build()
			}),
			wantErr: "proxy.retry_min_backoff",
		},
		{
			name: "negative proxy.retry_max_backoff",
			fn: build(func(b *Function_builder) {
				b.Proxy = ProxyPolicy_builder{RetryMaxBackoff: durationpb.New(-time.Second)}.Build()
			}),
			wantErr: "proxy.retry_max_backoff",
		},
		{
			name: "inverted backoff bounds",
			fn: build(func(b *Function_builder) {
				b.Proxy = ProxyPolicy_builder{
					RetryMinBackoff: durationpb.New(time.Second),
					RetryMaxBackoff: durationpb.New(100 * time.Millisecond),
				}.Build()
			}),
			wantErr: "must be <= proxy.retry_max_backoff",
		},
		{
			name: "negative lifecycle.assign_timeout",
			fn: build(func(b *Function_builder) {
				b.Lifecycle = LifecyclePolicy_builder{AssignTimeout: durationpb.New(-time.Second)}.Build()
			}),
			wantErr: "lifecycle.assign_timeout",
		},
		{
			name: "negative lifecycle.token_ttl",
			fn: build(func(b *Function_builder) {
				b.Lifecycle = LifecyclePolicy_builder{TokenTtl: durationpb.New(-time.Second)}.Build()
			}),
			wantErr: "lifecycle.token_ttl",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.fn.Validate()
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
			} else {
				assert.NilError(t, err)
			}
		})
	}
}

func TestFunctionFromHeaderPreservesPolicies(t *testing.T) {
	t.Parallel()

	header := `{
		"namespace":"ns","deployment":"d","tenant":"t",
		"scale":{"min_instances":1,"max_instances":10},
		"hpa":{"tolerance":0.05,"downscale_stabilization":"60s","initial_readiness_delay":"30s"},
		"heartbeat":{"timeout":"1800s"},
		"proxy":{"max_attempts":3,"retry_min_backoff":"0.010s","retry_max_backoff":"1s"},
		"lifecycle":{"assign_timeout":"45s","token_ttl":"3600s"}
	}`

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(FunctionKey.Header, header)

	fn, err := FunctionFromHeader(req)
	assert.NilError(t, err)

	assert.Equal(t, fn.HPATolerance(0), 0.05)
	assert.Equal(t, fn.HPADownscaleStabilization(0), 60*time.Second)
	assert.Equal(t, fn.HPAInitialReadinessDelay(0), 30*time.Second)
	assert.Equal(t, fn.HeartbeatTimeout(0), 30*time.Minute)
	assert.Equal(t, fn.MaxRoundTripAttempts(0), uint32(3))
	assert.Equal(t, fn.RetryMinBackoff(0), 10*time.Millisecond)
	assert.Equal(t, fn.RetryMaxBackoff(0), time.Second)
	assert.Equal(t, fn.AssignTimeout(0), 45*time.Second)
	assert.Equal(t, fn.TokenTTL(0), time.Hour)
}

func TestFunctionFromHeaderOmittedPoliciesAreZero(t *testing.T) {
	t.Parallel()

	header := `{"namespace":"ns","deployment":"d","tenant":"t","scale":{"min_instances":1,"max_instances":10}}`
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(FunctionKey.Header, header)

	fn, err := FunctionFromHeader(req)
	assert.NilError(t, err)

	// Each resolver returns the cluster default when the policy is unset.
	assert.Equal(t, fn.HPATolerance(0.10), 0.10)
	assert.Equal(t, fn.HeartbeatTimeout(90*time.Second), 90*time.Second)
	assert.Equal(t, fn.MaxRoundTripAttempts(6), uint32(6))
	assert.Equal(t, fn.TokenTTL(7*24*time.Hour), 7*24*time.Hour)
}
