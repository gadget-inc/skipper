package controller

import (
	"context"
	"net/http"
	"testing"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	"k8s.io/client-go/kubernetes/fake"
)

func withLifecyclePolicy(fn *skipper.Function, lifecyclePolicy *skipper.LifecyclePolicy) *skipper.Function {
	cloned := proto.Clone(fn).(*skipper.Function)
	cloned.SetLifecycle(lifecyclePolicy)
	return cloned
}

// capturedAssign records the assign request's PASETO expiration so tests can
// assert that the resolved lifecycle.token_ttl drove the claim.
type capturedAssign struct {
	tokenExpiration time.Time
}

func captureAssignHandler(t *testing.T, captured *capturedAssign) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		assert.Equal(t, req.Method, http.MethodPost)
		assert.Equal(t, req.URL.Path, "/__skipper/assign")

		parser := paseto.NewParserForValidNow()
		token, err := parser.ParseV2Public(fixture.ControllerPasetoPublicKey, req.Header.Get(key.Token.Header))
		assert.NilError(t, err)
		captured.tokenExpiration, err = token.GetExpiration()
		assert.NilError(t, err)

		rw.WriteHeader(http.StatusOK)
	})
}

// TestAssignPodPerFunctionTokenTTL verifies that a function with a per-function
// lifecycle.token_ttl receives a PASETO token whose exp claim reflects the
// shorter value, while a function omitting the policy uses the cluster
// default.
func TestAssignPodPerFunctionTokenTTL(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.TokenTTL = 7 * 24 * time.Hour // cluster default

	cases := []struct {
		name        string
		fn          func(*skipper.Function) *skipper.Function
		expectedTTL time.Duration
	}{
		{
			name:        "uses cluster default when lifecycle is omitted",
			fn:          func(f *skipper.Function) *skipper.Function { return f },
			expectedTTL: cfg.TokenTTL,
		},
		{
			name: "uses per-function override when set",
			fn: func(f *skipper.Function) *skipper.Function {
				return withLifecyclePolicy(f, skipper.LifecyclePolicy_builder{
					TokenTtl: durationpb.New(time.Hour),
				}.Build())
			},
			expectedTTL: time.Hour,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fn := tc.fn(fixture.NewFunction(t))
			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			captured := &capturedAssign{}
			fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, captureAssignHandler(t, captured)))

			ctrl := New(cfg, nil, fakeKubernetes, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			before := time.Now()
			_, err = ctrl.assignPod(ctx, fn)
			assert.NilError(t, err)

			actualTTL := captured.tokenExpiration.Sub(before)
			// Allow a small wall-clock tolerance because token issuance happens
			// some moments after `before`.
			lower := tc.expectedTTL - 5*time.Second
			upper := tc.expectedTTL + time.Second
			assert.Assert(t, actualTTL >= lower && actualTTL <= upper,
				"token TTL %s should be ~%s (range %s..%s)", actualTTL, tc.expectedTTL, lower, upper)
		})
	}
}

// TestAssignPodPerFunctionAssignTimeoutOverrideSurvivesSlowHandler verifies
// that a function with a longer lifecycle.assign_timeout still completes
// when the assign handler exceeds the cluster default. The cluster-default
// case fails on the same slow handler.
func TestAssignPodPerFunctionAssignTimeoutOverrideSurvivesSlowHandler(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.FunctionAssignTimeout = 50 * time.Millisecond // cluster default

	slowHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		time.Sleep(200 * time.Millisecond) // longer than cluster default, shorter than override
		rw.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name      string
		fn        func(*skipper.Function) *skipper.Function
		expectErr bool
	}{
		{
			name:      "cluster default times out on slow handler",
			fn:        func(f *skipper.Function) *skipper.Function { return f },
			expectErr: true,
		},
		{
			name: "per-function override succeeds on slow handler",
			fn: func(f *skipper.Function) *skipper.Function {
				return withLifecyclePolicy(f, skipper.LifecyclePolicy_builder{
					AssignTimeout: durationpb.New(2 * time.Second),
				}.Build())
			},
			expectErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fn := tc.fn(fixture.NewFunction(t))
			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, slowHandler))

			ctrl := New(cfg, nil, fakeKubernetes, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			_, err = ctrl.assignPod(ctx, fn)
			if tc.expectErr {
				assert.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				assert.NilError(t, err)
			}
		})
	}
}

// TestCleanupStuckInstancesPerFunctionAssignTimeout verifies that a stuck
// instance is cleaned up against the function's effective assign timeout.
// Two functions share a controller config (--function-assign-timeout = 30s)
// but one overrides lifecycle.assign_timeout = 1ms. The instance for the
// override is cleaned up after a few milliseconds; the cluster-default one is
// not.
func TestCleanupStuckInstancesPerFunctionAssignTimeout(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.FunctionAssignTimeout = 30 * time.Second

	clusterFn := fixture.NewFunction(t)
	overrideFn := withLifecyclePolicy(fixture.NewFunction(t), skipper.LifecyclePolicy_builder{
		AssignTimeout: durationpb.New(time.Millisecond),
	}.Build())

	clusterPod := fixture.NewAssignedPod(t, clusterFn, nil)
	overridePod := fixture.NewAssignedPod(t, overrideFn, nil)

	staleAssignedAt := timestamppb.New(time.Now().Add(-100 * time.Millisecond))
	clusterInstance := skipper.Instance_builder{
		Function:   clusterFn,
		Name:       new(clusterPod.Name),
		AssignedAt: staleAssignedAt,
	}.Build()
	overrideInstance := skipper.Instance_builder{
		Function:   overrideFn,
		Name:       new(overridePod.Name),
		AssignedAt: staleAssignedAt,
	}.Build()

	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod(), clusterPod, overridePod)
	ctrl := New(cfg, nil, fakeKubernetes, nil)
	sup := &Supervisor{ctrl: ctrl}

	remaining := sup.cleanupStuckInstances(t.Context(), []*skipper.Instance{clusterInstance, overrideInstance})

	assert.Equal(t, len(remaining), 1, "override instance should be cleaned up; cluster instance retained")
	assert.Equal(t, remaining[0].GetName(), clusterPod.Name,
		"only the cluster-default instance should remain")
}
