package controller

import (
	"context"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"gotest.tools/v3/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPodHashKey_StableHash(t *testing.T) {
	t.Parallel()
	h1 := podHashKey("deployment-abc-12345").Hash()
	h2 := podHashKey("deployment-abc-12345").Hash()
	assert.Equal(t, h1, h2)
}

func TestPodOwnedByMe_EmptyRingOwnsEverything(t *testing.T) {
	t.Parallel()
	ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
	assert.Assert(t, ctrl.podOwnedByMe("any-pod-name"))
}

func TestPodOwnedByMe_SelfOnlyRingOwnsEverything(t *testing.T) {
	t.Parallel()
	ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
	ctrl.ring.Add(ctrl.config.PodIP)
	for _, name := range []string{"a", "b", "c", "very-long-name-with-dashes"} {
		assert.Assert(t, ctrl.podOwnedByMe(name), "self-only ring should own %q", name)
	}
}

// TestPodPartition_StableDeterministicMapping asserts the partition invariant:
// every pod maps to exactly one controller in the ring, and that mapping is
// stable across calls (so a pod won't bounce between owners between two
// getUnassignedPod polls inside the same ring topology).
func TestPodPartition_StableDeterministicMapping(t *testing.T) {
	t.Parallel()

	const n = 3
	peerIPs := []string{fixture.ControllerIP, fixture.ControllerIP2, "127.0.2.3"}

	ctrls := make([]*Controller, n)
	for i := range ctrls {
		cfg := testConfig()
		cfg.PodIP = peerIPs[i]
		ctrls[i] = New(cfg, nil, fake.NewClientset(), nil)
		for _, ip := range peerIPs {
			ctrls[i].ring.Add(ip)
		}
	}

	podNames := make([]string, 100)
	for i := range podNames {
		podNames[i] = "pod-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i%10)) + "-" + string(rune('a'+(i*7)%26))
	}

	// For each pod, count how many controllers claim ownership.
	for _, name := range podNames {
		ownerCount := 0
		for _, ctrl := range ctrls {
			if ctrl.podOwnedByMe(name) {
				ownerCount++
			}
		}
		assert.Equal(t, ownerCount, 1, "pod %q should be owned by exactly one controller, got %d", name, ownerCount)
	}

	// Stability: the same pod is owned by the same controller across calls.
	firstPassOwners := make(map[string]string)
	for _, name := range podNames {
		for _, ctrl := range ctrls {
			if ctrl.podOwnedByMe(name) {
				firstPassOwners[name] = ctrl.config.PodIP
			}
		}
	}
	for _, name := range podNames {
		for _, ctrl := range ctrls {
			if ctrl.podOwnedByMe(name) {
				assert.Equal(t, ctrl.config.PodIP, firstPassOwners[name],
					"pod %q owner changed between calls", name)
			}
		}
	}
}

// TestGetUnassignedPods_FiltersToOwned asserts the partition shortens the
// candidate set in the multi-peer steady state: every pod returned hashes to
// this controller, and pods owned by peers are excluded.
func TestGetUnassignedPods_FiltersToOwned(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)

	pool := []runtime.Object{
		fixture.NewControllerPodAt("ctrl-self", fixture.ControllerIP),
		fixture.NewControllerPodAt("ctrl-peer", fixture.ControllerIP2),
	}
	for range 50 {
		pool = append(pool, fixture.NewAvailablePod(t, fn, nil))
	}

	fakeKubernetes := fake.NewClientset(pool...)
	ctrl := New(testConfig(), nil, fakeKubernetes, nil)
	assert.NilError(t, ctrl.startInformers(ctx))
	assert.Equal(t, len(ctrl.ring.List()), 2, "ring should hold both peers")

	pods, err := ctrl.getUnassignedPods(fn)
	assert.NilError(t, err)
	assert.Assert(t, len(pods) > 0, "expected at least one owned pod in a 50-pod pool")
	for _, pod := range pods {
		assert.Assert(t, ctrl.podOwnedByMe(pod.Name), "returned pod %q is not owned by this controller", pod.Name)
	}
	assert.Assert(t, len(pods) < 50, "partition should exclude peer-owned pods (got all 50)")
}

// TestGetUnassignedPods_FallsBackWhenPartitionEmpty asserts the empty-partition
// fallback: when this controller has zero owned pods (e.g., cohort transition
// window where self's IP is briefly absent from the ring) but the unfiltered
// pool is non-empty, getUnassignedPods returns the unfiltered pool so the
// function-responsible controller doesn't starve.
func TestGetUnassignedPods_FallsBackWhenPartitionEmpty(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)

	pool := []runtime.Object{
		fixture.NewControllerPodAt("ctrl-self", fixture.ControllerIP),
		fixture.NewControllerPodAt("ctrl-peer-1", fixture.ControllerIP2),
		fixture.NewControllerPodAt("ctrl-peer-2", "127.0.2.3"),
	}
	for range 5 {
		pool = append(pool, fixture.NewAvailablePod(t, fn, nil))
	}

	fakeKubernetes := fake.NewClientset(pool...)
	ctrl := New(testConfig(), nil, fakeKubernetes, nil)
	assert.NilError(t, ctrl.startInformers(ctx))

	// Force the empty-partition condition by removing self's IP from the
	// ring. Mirrors the brief window during a cohort transition where
	// removeFromRing has fired for self but the new Add hasn't landed yet.
	ctrl.ring.Remove(ctrl.config.PodIP)
	assert.Equal(t, len(ctrl.ring.List()), 2, "ring should hold only peers")

	pods, err := ctrl.getUnassignedPods(fn)
	assert.NilError(t, err)
	assert.Equal(t, len(pods), 5, "fallback should return every available pod")
}
