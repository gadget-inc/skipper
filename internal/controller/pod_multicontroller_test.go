package controller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
	"gotest.tools/v3/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// TestMultiControllerContention asserts the partition + reservation design
// keeps multi-controller racing on a shared unassigned pool collision-free.
// N controllers each call assignPod for the same function concurrently;
// because every pod hashes to exactly one controller, no two controllers
// attempt to patch the same K8s object. Pre-change, N controllers all saw
// every unassigned pod and rand.Intn would collide, generating cross-
// controller Conflict errors on every successful assign.
func TestMultiControllerContention(t *testing.T) {
	t.Parallel()

	const (
		numControllers = 3
		poolSize       = 30
	)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)

	pool := make([]runtime.Object, 0, poolSize)
	for range poolSize {
		pool = append(pool, fixture.NewAvailablePod(t, fn, nil))
	}

	multi := newMultiControllerFixture(t, ctx, numControllers, pool...)

	var applyCalls, totalPatches int64
	var counterMu sync.Mutex
	multi.fakeKubernetes.PrependReactor("patch", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		counterMu.Lock()
		defer counterMu.Unlock()
		totalPatches++
		if pa, ok := action.(ktesting.PatchAction); ok && pa.GetPatchType() == types.ApplyPatchType {
			applyCalls++
		}
		return false, nil, nil
	})

	var wg sync.WaitGroup
	results := make([]*skipper.Instance, numControllers)
	errs := make([]error, numControllers)
	wg.Add(numControllers)
	for i := range numControllers {
		go func() {
			defer wg.Done()
			results[i], errs[i] = multi.controllers[i].assignPod(ctx, fn)
		}()
	}
	wg.Wait()

	// (b) every call returns nil error
	for i, err := range errs {
		assert.NilError(t, err, "controller %d (%s) assignPod failed", i, multi.peerIPs[i])
	}

	// (a) every concurrent assignPod returns a distinct pod name
	seen := make(map[string]int)
	for _, inst := range results {
		assert.Assert(t, inst != nil, "expected non-nil instance")
		seen[inst.GetName()]++
	}
	assert.Equal(t, len(seen), numControllers, "expected %d distinct pod names, got %v", numControllers, seen)

	// (c) no pod's tenant label is overwritten (each assigned pod has the function's tenant)
	for _, inst := range results {
		assert.Equal(t, inst.GetFunction().GetTenant(), fn.GetTenant())
	}

	// (d) applies/success == 1 per successful assign: each controller ran SSA
	// exactly once and RetryOnConflict's closure didn't re-enter. Plus one
	// async ready-at JSON-patch per success, so total patches >= applies.
	counterMu.Lock()
	defer counterMu.Unlock()
	assert.Equal(t, applyCalls, int64(numControllers),
		"applies/success should be exactly 1 per assign (got %d applies for %d successes)", applyCalls, numControllers)
	assert.Assert(t, totalPatches >= applyCalls,
		"total patches (%d) should be at least applies (%d) -- async readyAt patches add 1 per success",
		totalPatches, applyCalls)
}

// TestRingRebalanceReclaim asserts that when a controller leaves the ring,
// pods that hashed to the departing controller become claimable by the
// surviving controllers. The partition redistributes via the existing ring
// rebalance without any explicit handoff; the assignPod path on a surviving
// controller transparently picks up the orphaned partition.
func TestRingRebalanceReclaim(t *testing.T) {
	t.Parallel()

	const numControllers = 3

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)

	const poolSize = 30
	pool := make([]runtime.Object, 0, poolSize)
	for range poolSize {
		pool = append(pool, fixture.NewAvailablePod(t, fn, nil))
	}

	multi := newMultiControllerFixture(t, ctx, numControllers, pool...)

	// Identify a victim controller and capture which pods it owns under the
	// initial 3-peer topology.
	victim := multi.controllers[numControllers-1]
	victimIP := multi.peerIPs[numControllers-1]

	survivor := multi.controllers[0]
	beforeOwnedBySurvivor, err := survivor.getUnassignedPods(fn)
	assert.NilError(t, err)
	beforeCountForSurvivor := len(beforeOwnedBySurvivor)
	assert.Assert(t, beforeCountForSurvivor > 0 && beforeCountForSurvivor < poolSize,
		"initial partition for survivor should be a strict subset of the pool (got %d of %d)",
		beforeCountForSurvivor, poolSize)

	// Confirm at least one pod is NOT owned by the survivor pre-rebalance --
	// i.e., the orphan candidates exist.
	beforeOwnedSet := make(map[string]struct{}, beforeCountForSurvivor)
	for _, pod := range beforeOwnedBySurvivor {
		beforeOwnedSet[pod.Name] = struct{}{}
	}
	allPods, err := survivor.listPods(fn.GetNamespace(), doesNotHaveTenantSelector)
	assert.NilError(t, err)
	var orphanCandidate string
	for _, pod := range allPods {
		if _, owned := beforeOwnedSet[pod.Name]; !owned {
			orphanCandidate = pod.Name
			break
		}
	}
	assert.Assert(t, orphanCandidate != "", "expected at least one pod not owned by the survivor pre-rebalance")
	_ = victim

	// Simulate the victim leaving the ring from the survivor's perspective.
	survivor.ring.Remove(victimIP)

	// After rebalance, the survivor's partition expands. With the victim
	// out of the ring, pods that hashed to it now hash to one of the
	// remaining peers, so the survivor sees a strictly larger owned set.
	afterOwnedBySurvivor, err := survivor.getUnassignedPods(fn)
	assert.NilError(t, err)
	assert.Assert(t, len(afterOwnedBySurvivor) > beforeCountForSurvivor,
		"survivor's partition should grow after victim leaves (before=%d, after=%d)",
		beforeCountForSurvivor, len(afterOwnedBySurvivor))

	// The survivor can assign a pod from the rebalanced partition without
	// error, drawing from the orphan share the victim has released.
	instance, err := survivor.assignPod(ctx, fn)
	assert.NilError(t, err)
	assert.Assert(t, instance != nil)

	// Sanity: no double-assignment -- the assigned pod is now tenant-labelled
	// and excluded from any future getUnassignedPods call on any controller.
	stillUnassigned, err := survivor.listPods(fn.GetNamespace(), doesNotHaveTenantSelector)
	assert.NilError(t, err)
	for _, pod := range stillUnassigned {
		assert.Assert(t, pod.Name != instance.GetName(),
			"pod %q was assigned but still appears in the unassigned pool", instance.GetName())
	}
}

// TestOneshotForwarding asserts that a controller not responsible for a
// oneshot function forwards the GetInstance call to the function-responsible
// controller via the controller client, instead of running assignPod against
// its own (likely empty) partition for that function.
func TestOneshotForwarding(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	// Seed exactly one other controller in the ring whose IP is NOT
	// testConfig().PodIP, so ring.Get(fn) returns the other peer regardless
	// of fn's hash.
	peerCtrlPod := fixture.NewControllerPodAt("peer-controller", "127.0.0.99")
	fakeKubernetes := fake.NewClientset(peerCtrlPod)

	fn := fixture.NewFunction(t)
	fn.SetOneshot(true)

	expectedInstance := fixture.NewInstance(t, fn, nil)
	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleInstance(func(ctx context.Context, fwdFn *skipper.Function, exclude ...string) (*skipper.Instance, error) {
		assert.Assert(t, fwdFn.GetOneshot(), "forwarded function should be oneshot")
		assert.Equal(t, fwdFn.GetTenant(), fn.GetTenant())
		return expectedInstance, nil
	})

	ctrl := New(testConfig(), func(host string) Client { return mcc }, fakeKubernetes, nil)
	assert.NilError(t, ctrl.startInformers(ctx))

	got, err := ctrl.supervisor(fn).getReadyInstance(ctx, nil)
	assert.NilError(t, err)
	assert.Equal(t, got, expectedInstance, "expected forwarded instance, got local assign")
}
