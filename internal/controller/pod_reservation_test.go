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
	"k8s.io/client-go/kubernetes/fake"
)

// TestAssignPod_IntraControllerReservation asserts that two assignPod calls
// running in the same controller goroutine never target the same pod, even
// when getUnassignedPod's rand.Intn pick selects an already-claimed pod.
// Without the reservation, two concurrent picks on the same pod both run the
// SSA apply; on the fake clientset SSA does not enforce field-manager
// conflicts, so the second write silently overwrites the first and both
// callers receive an instance for the same pod name. The reservation makes
// the second caller loop back to getUnassignedPod and claim a different pod.
//
// This test stays in-controller (single ring member). The cross-controller
// race is exercised by Phase 3's multi-controller integration test.
func TestAssignPod_IntraControllerReservation(t *testing.T) {
	t.Parallel()

	const (
		poolSize   = 10
		goroutines = 10
	)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)

	pool := []runtime.Object{fixture.NewControllerPod()}
	for range poolSize {
		pool = append(pool, fixture.NewAvailablePod(t, fn, nil))
	}

	fakeKubernetes := fake.NewClientset(pool...)
	ctrl := New(testConfig(), nil, fakeKubernetes, nil)
	assert.NilError(t, ctrl.startInformers(ctx))

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		instances []*skipper.Instance
		errs      []error
	)
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			instance, err := ctrl.assignPod(ctx, fn)
			mu.Lock()
			defer mu.Unlock()
			instances = append(instances, instance)
			errs = append(errs, err)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		assert.NilError(t, err, "goroutine %d", i)
	}
	assert.Equal(t, len(instances), goroutines)

	seen := make(map[string]int)
	for _, inst := range instances {
		seen[inst.GetName()]++
	}
	for name, count := range seen {
		assert.Equal(t, count, 1, "pod %q was assigned %d times (expected exactly once)", name, count)
	}
}
