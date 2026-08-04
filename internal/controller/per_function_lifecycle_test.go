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

// TestCleanupStuckInstancesUsesFreshFunctionSnapshot pins the snapshot
// invariant: cleanupStuckInstances must read lifecycle.assign_timeout from
// the converge-tick fn snapshot, not from instance.GetFunction(). The
// per-instance function pointer is the policy serialized into the pod
// annotation at assignment time -- a tenant who shortens
// lifecycle.assign_timeout after assignment expects existing stuck instances
// to be cleaned up faster, not stranded behind the old (longer) timeout.
func TestCleanupStuckInstancesUsesFreshFunctionSnapshot(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.FunctionAssignTimeout = 30 * time.Second // cluster default

	// A pod was assigned earlier when the function carried no override --
	// instance.GetFunction() therefore captures the long cluster default.
	staleFn := fixture.NewFunction(t)
	staleAssignedAt := timestamppb.New(time.Now().Add(-100 * time.Millisecond))
	pod := fixture.NewAssignedPod(t, staleFn, nil)
	instance := skipper.Instance_builder{
		Function:   staleFn,
		Name:       new(pod.Name),
		AssignedAt: staleAssignedAt,
	}.Build()

	// The tenant has since shortened assign_timeout to 1ms via the next
	// request. The converge tick captures the fresh snapshot.
	freshFn := withLifecyclePolicy(staleFn, skipper.LifecyclePolicy_builder{
		AssignTimeout: durationpb.New(time.Millisecond),
	}.Build())

	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod(), pod)
	ctrl := New(cfg, nil, fakeKubernetes, nil)
	sup := &Supervisor{ctrl: ctrl}

	remaining := sup.cleanupStuckInstances(t.Context(), freshFn, []*skipper.Instance{instance})

	assert.Equal(t, len(remaining), 0,
		"fresh fn snapshot (1ms assign_timeout) must drive cleanup; the stale annotation policy must not strand the instance behind a 30s cluster default")
}

// TestCleanupStuckInstancesOmittedLifecyclePolicy verifies the cluster default
// path: a function omitting lifecycle.assign_timeout uses
// --function-assign-timeout * 2 as the cleanup threshold.
func TestCleanupStuckInstancesOmittedLifecyclePolicy(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.FunctionAssignTimeout = time.Millisecond

	fn := fixture.NewFunction(t)
	assert.Assert(t, fn.GetLifecycle() == nil, "fixture function must omit lifecycle policy")

	pod := fixture.NewAssignedPod(t, fn, nil)
	instance := skipper.Instance_builder{
		Function:   fn,
		Name:       new(pod.Name),
		AssignedAt: timestamppb.New(time.Now().Add(-100 * time.Millisecond)),
	}.Build()

	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod(), pod)
	ctrl := New(cfg, nil, fakeKubernetes, nil)
	sup := &Supervisor{ctrl: ctrl}

	remaining := sup.cleanupStuckInstances(t.Context(), fn, []*skipper.Instance{instance})

	assert.Equal(t, len(remaining), 0,
		"cluster default 1ms * 2 = 2ms threshold should clean up a 100ms-stuck instance")
}
