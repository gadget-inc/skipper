package controller

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"
)

// BenchmarkAssignPodContention measures the assignment hot path with N
// controllers backed by a single fake clientset, each calling assignPod
// concurrently for a distinct function.
//
// The fake clientset serializes Patch and Apply actions through a tracker
// mutex and -- since k8s 1.30 -- uses managedFieldObjectTracker for
// server-side apply, which is heavier per-call than the old JSON-patch
// path. The bench cannot reproduce the May 12 production incident's
// bimodal p99 (3-7 min); production tail latency was driven by N
// controllers racing on the same K8s object via JSON-patch test ops, which
// the partitioned design eliminates entirely. The meaningful "before"
// for that comparison is the production-data baseline documented in the
// Brief. This benchmark exists to set the forward-looking acceptance bar
// for Phase 2 (no retries, bounded per-call latency under the parallelism
// of the production controller cohort) and to catch regressions in the
// assignment hot path between commits.
//
// GOMAXPROCS is pinned to numControllers for the duration of the run so
// b.RunParallel uses exactly N goroutines (one per controller) -- the
// production-shaped contention scenario. Without the pin, GOMAXPROCS on
// the test host can be 10-14 and the bench measures fake-mutex contention
// rather than the assignment hot path itself.
//
// Reported metrics:
//   - ns/op: default mean latency per assignPod call
//   - p99-ms: 99th-percentile per-iteration latency
//   - patches/success: total Patch+Apply actions divided by successful
//     assigns (each success consists of one main apply and one async
//     ready-at patch, so the steady-state value is ~2 here even when
//     RetryOnConflict never fires)
//   - applies/success: main SSA Apply actions divided by successful
//     assigns -- the retry-path indicator. 1.0 means RetryOnConflict's
//     fn ran exactly once per assign; values >1.0 indicate conflicts.
func BenchmarkAssignPodContention(b *testing.B) {
	const (
		numControllers   = 3
		poolPerFunction  = 20_000
		assignCtxTimeout = 60 * time.Second
	)

	prevMaxProcs := runtime.GOMAXPROCS(numControllers)
	b.Cleanup(func() { runtime.GOMAXPROCS(prevMaxProcs) })

	ctx, cancel := context.WithTimeout(context.Background(), assignCtxTimeout)
	b.Cleanup(cancel)

	sharedAssign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	b.Cleanup(sharedAssign.Close)

	sharedHost, sharedPortStr, err := net.SplitHostPort(sharedAssign.Listener.Addr().String())
	if err != nil {
		b.Fatalf("split assign host: %v", err)
	}
	sharedPort, err := strconv.Atoi(sharedPortStr)
	if err != nil {
		b.Fatalf("parse assign port: %v", err)
	}

	fns := make([]*skipper.Function, numControllers)
	for i := range fns {
		fns[i] = fixture.NewFunction(b)
	}

	// Build the unassigned pool before starting informers. The fake watcher
	// channel only buffers ~100 events; adding tens of thousands of pods
	// after informers register would overflow it. Seeding through
	// fake.NewClientset (via the fixture's extraObjects) lets informers pick
	// the pool up via initial-list with no watch traffic.
	poolObjs := make([]k8sruntime.Object, 0, numControllers*poolPerFunction)
	for c := range numControllers {
		for range poolPerFunction {
			poolObjs = append(poolObjs, benchAvailablePod(fns[c], sharedHost, int32(sharedPort)))
		}
	}

	multi := newMultiControllerFixture(b, ctx, numControllers, poolObjs...)

	var totalPatches, applyPatches atomic.Int64
	multi.fakeKubernetes.PrependReactor("patch", "pods", func(action ktesting.Action) (bool, k8sruntime.Object, error) {
		totalPatches.Add(1)
		if pa, ok := action.(ktesting.PatchAction); ok && pa.GetPatchType() == types.ApplyPatchType {
			applyPatches.Add(1)
		}
		return false, nil, nil
	})

	var (
		goroutineCounter atomic.Int64
		successes        atomic.Int64

		durMu        sync.Mutex
		allDurations []time.Duration
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		gid := int(goroutineCounter.Add(1) - 1)
		ctrl := multi.controllers[gid%numControllers]
		fn := fns[gid%numControllers]

		local := make([]time.Duration, 0, 1024)
		for pb.Next() {
			start := time.Now()
			_, err := ctrl.assignPod(ctx, fn)
			elapsed := time.Since(start)
			if err != nil {
				b.Fatalf("assignPod failed (goroutine %d): %v", gid, err)
			}
			local = append(local, elapsed)
			successes.Add(1)
		}

		durMu.Lock()
		allDurations = append(allDurations, local...)
		durMu.Unlock()
	})

	b.StopTimer()

	if s := successes.Load(); s > 0 {
		b.ReportMetric(float64(totalPatches.Load())/float64(s), "patches/success")
		b.ReportMetric(float64(applyPatches.Load())/float64(s), "applies/success")
	}

	if len(allDurations) > 0 {
		slices.Sort(allDurations)
		// floor(0.99 * n) is the 99th-percentile index for the conventional
		// "nearest-rank" definition; clamp to len-1 to stay in bounds.
		p99idx := int(0.99 * float64(len(allDurations)))
		if p99idx >= len(allDurations) {
			p99idx = len(allDurations) - 1
		}
		p99 := allDurations[p99idx]
		b.ReportMetric(float64(p99.Microseconds())/1000.0, "p99-ms")
	}
}

// benchAvailablePod constructs an unassigned pod whose Status.PodIP and
// container port resolve to a shared httptest.Server, so the bench can scale
// the pool to tens of thousands of pods without spawning a server per pod.
func benchAvailablePod(fn *skipper.Function, podIP string, port int32) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			// Full UUID (not the [:8] prefix used by fixture.NewAvailablePod) to
			// keep collision probability negligible across pools of 50k+ pods.
			Name:      fn.GetDeployment() + "-" + uuid.NewString(),
			Namespace: fn.GetNamespace(),
			Labels: map[string]string{
				key.Deployment.Label: fn.GetDeployment(),
			},
			Annotations: map[string]string{
				key.Port.Label: "http",
			},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: fn.GetDeployment() + "-bench-rs"},
			},
		},
		Status: v1.PodStatus{
			PodIP: podIP,
			Phase: v1.PodRunning,
			Conditions: []v1.PodCondition{
				{Type: v1.PodReady, Status: v1.ConditionTrue},
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "main",
					Ports: []v1.ContainerPort{
						{Name: "http", ContainerPort: port},
					},
				},
			},
		},
	}
}
