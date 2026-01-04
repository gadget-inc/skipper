package controller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestScale(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *function.Function
		fakeKubernetes *fake.Clientset
		instances      []*function.Instance
	}

	testCases := []struct {
		name             string
		desiredInstances int
		err              error
		setup            func(*testing.T, *testState)
		check            func(*testing.T, *testState)
	}{
		// ==================== Basic scaling operations ====================
		{
			// Basic scale up: assign an available pod to reach desired instance count
			name:             "scales up by assigning available pod",
			desiredInstances: 1,
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1)
			},
		},
		{
			// Scale up with surplus pods: only assigns needed pods, leaves extras unassigned
			name:             "only assigns needed pods when extras available",
			desiredInstances: 1,
			setup: func(t *testing.T, state *testState) {
				for range 5 {
					state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1)

				// verify 4 pods remain unassigned
				instance := state.instances[0]
				pods, err := state.fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{
					LabelSelector: doesNotHaveTenantSelector.String(),
				})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 4)
			},
		},
		{
			// No pods available: should timeout waiting for pods
			name:             "times out when no pods available",
			desiredInstances: 1,
			err:              context.DeadlineExceeded,
			setup: func(t *testing.T, state *testState) {
				// intentionally empty - no pods available
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			// Metadata mismatch: assigned pod has different metadata, can't be reused
			name:             "ignores pods with different metadata",
			desiredInstances: 1,
			err:              context.DeadlineExceeded,
			setup: func(t *testing.T, state *testState) {
				fn := *state.fn // copy the function
				fn.Metadata = "different"
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, &fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			// Already at desired count: no scaling needed, returns existing instances
			name:             "returns existing instances when already at desired count",
			desiredInstances: 1,
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1)
			},
		},

		// ==================== Scale down operations ====================
		{
			// Scale down: keeps most recently assigned instance (likely has warmest cache)
			name:             "keeps most recently assigned instance when scaling down",
			desiredInstances: 1,
			setup: func(t *testing.T, state *testState) {
				// add max - 1 instances with older assignment times
				for range state.fn.Scale.MaxInstances - 1 {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}

				// add one instance with the most recent assignment time
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Name = "most-recent-assigned-at"
				pod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(time.Second).UTC().Format(time.RFC3339)
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1)
				assert.Assert(t, state.instances[0].Name == "most-recent-assigned-at")
			},
		},

		// ==================== Ready/unready instance interactions ====================
		{
			// Scale to max with one unready: assigns new pod to reach max ready instances
			// Unready instances are preserved during scale up (they might become ready soon)
			name:             "scales to max ready instances while preserving unready",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, state *testState) {
				assert.Assert(t, state.fn.Scale.MaxInstances == 5)

				// add max - 1 ready instances
				for range state.fn.Scale.MaxInstances - 1 {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}

				// add 1 unready instance
				unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
				unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				state.fakeKubernetes.Tracker().Add(unreadyPod)

				// add 1 available pod for scaling
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				fn := state.instances[0].Function
				assert.Assert(t, len(state.instances) == fn.Scale.MaxInstances)

				pods, err := state.fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == fn.Scale.MaxInstances+1)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == fn.Scale.MaxInstances)
				assert.Assert(t, unreadyCount == 1) // unready instance preserved
			},
		},
		{
			// All unready, over max: can't scale up when too many total instances exist
			name:             "blocks scale up when total instances exceed max",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, state *testState) {
				assert.Assert(t, state.fn.Scale.MaxInstances == 5)

				// add max + 1 unready instances (exceeds total instance limit)
				for range state.fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					state.fakeKubernetes.Tracker().Add(unreadyPod)
				}

				// available pod exists but shouldn't be used
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				// no ready instances returned because total count exceeds max
				assert.Assert(t, len(state.instances) == 0)
			},
		},
		{
			// Some ready, many unready: returns ready instances but can't scale up further
			name:             "returns existing ready instances when blocked by total count",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, state *testState) {
				assert.Assert(t, state.fn.Scale.MaxInstances == 5)

				// add 2 ready instances
				for range 2 {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}

				// add max + 1 unready instances
				for range state.fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					state.fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, state *testState) {
				// only 2 ready instances returned (can't scale up due to total count)
				assert.Assert(t, len(state.instances) == 2)

				fn := state.instances[0].Function
				pods, err := state.fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == fn.Scale.MaxInstances+3)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == 2)
				assert.Assert(t, unreadyCount == fn.Scale.MaxInstances+1) // unready preserved during scale up
			},
		},
		{
			// Scale to 0 cleans up all unready instances
			name:             "deletes all unready instances when scaling to zero",
			desiredInstances: 0,
			setup: func(t *testing.T, state *testState) {
				// add max + 1 unready instances
				for range state.fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					state.fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 0)

				// all unready instances should be deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(fixture.FunctionNamespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
			},
		},
		{
			// Scale down deletes all unready instances when maintaining one ready
			name:             "deletes all unready instances when scaling down to one",
			desiredInstances: 1,
			setup: func(t *testing.T, state *testState) {
				// add 1 ready instance
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))

				// add max unready instances
				for range state.fn.Scale.MaxInstances {
					unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					state.fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 1)

				// all unready instances should be deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(fixture.FunctionNamespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
			},
		},
		{
			// Can't scale up when total instances at limit (preserves unready during scale up)
			name:             "preserves unready instances during blocked scale up",
			desiredInstances: 2,
			setup: func(t *testing.T, state *testState) {
				// add 1 ready instance
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))

				// add max unready instances (1 ready + max unready = max+1 total)
				for range state.fn.Scale.MaxInstances {
					unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					state.fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, state *testState) {
				// only 1 ready instance returned (can't scale up, already over max total)
				assert.Assert(t, len(state.instances) == 1)

				fn := state.instances[0].Function
				pods, err := state.fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == fn.Scale.MaxInstances+1)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == 1)
				assert.Assert(t, unreadyCount == fn.Scale.MaxInstances) // unready preserved
			},
		},
		{
			// Scale down from max ready + 1 unready: deletes excess ready and unready
			name:             "deletes excess ready and unready instances when scaling down",
			desiredInstances: 2,
			setup: func(t *testing.T, state *testState) {
				// add max ready instances
				for range state.fn.Scale.MaxInstances {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}

				// add 1 unready instance
				unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
				unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				state.fakeKubernetes.Tracker().Add(unreadyPod)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 2)

				fn := state.instances[0].Function
				pods, err := state.fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 2)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == 2)
				assert.Assert(t, unreadyCount == 0) // unready always deleted during scale down
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}

			tc.setup(t, state)

			ctrl := New(testConfig(), nil, state.fakeKubernetes, nil)
			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			state.instances, err = ctrl.supervisor(state.fn).scale(ctx, ScalingDecision{
				DesiredInstances: tc.desiredInstances,
				Reason:           "test",
			})
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, state)
		})
	}
}

// TestScaleForwarding verifies that scale requests are forwarded to the responsible
// controller when the current controller is not responsible for the function.
// This happens in multi-controller deployments where functions are sharded across controllers.
func TestScaleForwarding(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	// Create a controller pod with a different IP so this controller won't be responsible
	ctrlPod := fixture.NewControllerPod()
	ctrlPod.Status.PodIP = "127.0.0.2"
	fakeKubernetes := fake.NewClientset(ctrlPod)

	fn := fixture.NewFunction(t)

	// Set up mock client to handle forwarded scale request
	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleScale(func(ctx context.Context, fn *function.Function, desiredInstances int, reason string) ([]*function.Instance, error) {
		return []*function.Instance{fixture.NewInstance(t, fn, nil)}, nil
	})

	ctrl := New(testConfig(), func(host string, port int) Client { return mcc }, fakeKubernetes, nil)

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Scale should succeed via forwarding to mock client
	instances, err := ctrl.supervisor(fn).scale(ctx, ScalingDecision{
		DesiredInstances: 1,
		Reason:           "test",
	})
	assert.NilError(t, err)
	assert.Assert(t, len(instances) == 1)
}

// TestStabilizationWindowUsesCorrectFlag verifies that the stabilization window is pruned
// using FlagHPADownscaleStabilization, not FlagHeartbeatTimeout. These flags serve different
// purposes and could be configured independently.
func TestStabilizationWindowUsesCorrectFlag(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	// Set different values for the two configs to distinguish which one is being used
	// HeartbeatTimeout: 30s (shorter) - used for scaling to 0 when no heartbeats
	// DownscaleStabilization: 90s (longer) - used for stabilization window
	cfg := testConfig()
	cfg.HeartbeatTimeout = 30 * time.Second
	cfg.HPADownscaleStabilization = 90 * time.Second

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	// Create 2 assigned pods
	pod1 := fixture.NewAssignedPod(t, fn, nil)
	pod2 := fixture.NewAssignedPod(t, fn, nil)
	fakeKubernetes.Tracker().Add(pod1)
	fakeKubernetes.Tracker().Add(pod2)

	ctrl := New(cfg, nil, fakeKubernetes, nil)
	ctrl.startedAt = time.Now().Add(-(cfg.HPADownscaleStabilization + time.Second))
	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	supervisor := ctrl.supervisor(fn)

	// Pre-populate the stabilization window with a recommendation that's:
	// - Older than FlagHeartbeatTimeout (30s) - would be pruned if using wrong flag
	// - Within FlagHPADownscaleStabilization (90s) - should be kept with correct flag
	recommendationTime := time.Now().Add(-45 * time.Second) // 45s ago
	supervisor.stabilizationWindow = []Recommendation{
		{DesiredInstances: 2, Timestamp: recommendationTime},
	}

	// Add a recent heartbeat so we don't scale to 0
	supervisor.routerHeartbeats.Store(fixture.RouterIP, &function.Heartbeat{
		Function:  fn,
		Timestamp: time.Now(),
	})

	// Create instances slice matching the pods
	instances := []*function.Instance{
		fixture.NewInstance(t, fn, nil),
		fixture.NewInstance(t, fn, nil),
	}

	// Call converge - it will calculate desired instances (should be 1 or 2 depending on metrics)
	// The key is checking if the old recommendation (45s ago) was kept in the stabilization window
	_, err = supervisor.converge(ctx, instances)
	assert.NilError(t, err)

	// The stabilization window should still contain the old recommendation because
	// 45s < 90s (FlagHPADownscaleStabilization)
	// If the bug exists (using FlagHeartbeatTimeout), the recommendation would be pruned
	// because 45s > 30s (FlagHeartbeatTimeout)
	foundOldRecommendation := false
	for _, rec := range supervisor.stabilizationWindow {
		if rec.Timestamp.Equal(recommendationTime) {
			foundOldRecommendation = true
			break
		}
	}
	assert.Assert(t, foundOldRecommendation, "stabilization window should keep recommendations within FlagHPADownscaleStabilization period, but the 45s old recommendation was pruned (likely using FlagHeartbeatTimeout=30s instead)")
}

// TestConvergeConcurrentAccess verifies that concurrent calls to
// converge don't cause data races. Run with -race to detect issues.
func TestConvergeConcurrentAccess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	// Create assigned pods for the function
	for range 3 {
		fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
	}

	cfg := testConfig()
	ctrl := New(cfg, nil, fakeKubernetes, nil)
	ctrl.startedAt = time.Now().Add(-(cfg.HPADownscaleStabilization + time.Second))
	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	supervisor := ctrl.supervisor(fn)

	// Add a heartbeat so we don't scale to 0
	supervisor.routerHeartbeats.Store(fixture.RouterIP, &function.Heartbeat{
		Function:  fn,
		Timestamp: time.Now(),
	})

	// Run multiple concurrent calls to converge to trigger any race conditions
	// The race detector will catch unsynchronized access to stabilizationWindow
	const numGoroutines = 10
	const iterationsPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range iterationsPerGoroutine {
				instances := []*function.Instance{
					fixture.NewInstance(t, fn, nil),
					fixture.NewInstance(t, fn, nil),
				}
				// Update heartbeat timestamp to keep function alive
				supervisor.routerHeartbeats.Store(fixture.RouterIP, &function.Heartbeat{
					Function:  fn,
					Timestamp: time.Now(),
				})
				_, _ = supervisor.converge(ctx, instances)
			}
		}()
	}

	wg.Wait()

	// If we get here without the race detector complaining, the test passes
	// The stabilizationWindow should have accumulated recommendations
}

// TestCalculateDesiredInstancesForMetric tests the HPA-style algorithm that determines
// the desired number of instances based on resource utilization metrics.
// The algorithm follows Kubernetes HPA behavior: desiredInstances = ceil(currentInstances * (currentUsage / targetUsage))
func TestCalculateDesiredInstancesForMetric(t *testing.T) {
	t.Parallel()

	// Instances must be ready past the initial readiness delay to be included in scaling decisions
	cfg := testConfig()
	readyAt := time.Now().Add(-cfg.HPAInitialReadinessDelay)

	testCases := []struct {
		name              string
		metricName        Metric
		podMetrics        []*function.Instance
		targetUsage       int
		expectedInstances int
	}{
		// ==================== Basic scaling decisions ====================
		{
			// Usage at 2x target triggers scale up to 2 instances
			// Formula: ceil(1 * (200/100)) = 2
			name:        "scales up when usage exceeds target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 200},
			},
			expectedInstances: 2,
		},
		{
			// Usage at 0.5x target triggers scale down to 1 instance
			// Formula: ceil(2 * (50/100)) = 1
			name:        "scales down when usage below target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 50},
				{ReadyAt: readyAt, CPUUsageMilli: 50},
			},
			expectedInstances: 1,
		},
		{
			// Usage exactly at target means no scaling needed
			name:        "no scaling when usage equals target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 100},
			},
			expectedInstances: 1,
		},

		// ==================== Tolerance and edge cases ====================
		{
			// Usage slightly above target (within tolerance) doesn't trigger scale up
			// This prevents thrashing from small fluctuations
			name:        "no scaling when usage within tolerance",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 110},
			},
			expectedInstances: 1,
		},
		{
			// When one instance is missing metrics, the algorithm assumes target usage for that instance
			// This prevents incorrect scale-up decisions when metrics are temporarily unavailable
			// Instance 1: 150m (would scale up), Instance 2: 0m (missing metric)
			// Adjusted average: 75m (would scale down) -> no scaling
			name:        "missing metric assumes target usage to prevent incorrect scale up",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 150},
				{ReadyAt: readyAt, CPUUsageMilli: 0}, // missing metric
			},
			expectedInstances: 2,
		},
		{
			// Recently started instances (within initial readiness delay) are excluded from scaling decisions
			// This gives new instances time to stabilize before affecting scaling
			name:        "excludes recently started instances from scaling decision",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 150},    // included in calculation
				{ReadyAt: time.Now(), CPUUsageMilli: 150}, // excluded (too new)
			},
			expectedInstances: 2,
		},
		{
			// When no metrics are available for any instance, maintain current count
			// This prevents scaling decisions based on incomplete data
			name:        "maintains current count when no metrics available",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*function.Instance{
				{ReadyAt: readyAt, CPUUsageMilli: 0},
				{ReadyAt: readyAt, CPUUsageMilli: 0},
			},
			expectedInstances: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Configure target usage for each instance
			for _, pm := range tc.podMetrics {
				if pm.Function == nil {
					pm.Function = &function.Function{Scale: &function.Scale{}}
				}
				switch tc.metricName {
				case MetricCPU:
					pm.Scale.TargetCPUUsageMilli = tc.targetUsage
				case MetricMemory:
					pm.Scale.TargetMemoryUsageMiB = tc.targetUsage
				}
			}

			instances, _ := calculateDesiredInstancesForMetric(t.Context(), cfg, tc.metricName, tc.podMetrics)
			assert.Assert(t, instances == tc.expectedInstances)
		})
	}
}

// TestHeartbeat tests the heartbeat update and garbage collection logic.
// The heartbeat function updates router heartbeats when newer timestamps are received
// and garbage collects expired heartbeats.
func TestHeartbeat(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	fn := fixture.NewFunction(t)

	testCases := []struct {
		name           string
		existingRouter string
		existingTime   time.Time
		newRouter      string
		newTime        time.Time
		expectedCount  int
		shouldUpdate   bool
	}{
		{
			// New heartbeat from a router we haven't seen before
			name:          "stores heartbeat from new router",
			newRouter:     fixture.RouterIP,
			newTime:       time.Now(),
			expectedCount: 1,
			shouldUpdate:  true,
		},
		{
			// Newer heartbeat from an existing router updates the stored value
			name:           "updates heartbeat when newer timestamp received",
			existingRouter: fixture.RouterIP,
			existingTime:   time.Now().Add(-time.Minute),
			newRouter:      fixture.RouterIP,
			newTime:        time.Now(),
			expectedCount:  1,
			shouldUpdate:   true,
		},
		{
			// Older heartbeat from an existing router is ignored
			name:           "ignores heartbeat when older timestamp received",
			existingRouter: fixture.RouterIP,
			existingTime:   time.Now(),
			newRouter:      fixture.RouterIP,
			newTime:        time.Now().Add(-time.Minute),
			expectedCount:  1,
			shouldUpdate:   false,
		},
		{
			// Heartbeat from a different router is stored separately
			name:           "stores heartbeat from different router",
			existingRouter: fixture.RouterIP,
			existingTime:   time.Now(),
			newRouter:      fixture.RouterIP2,
			newTime:        time.Now(),
			expectedCount:  2,
			shouldUpdate:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			ctrl := New(cfg, nil, fakeKubernetes, nil)
			supervisor := ctrl.supervisor(fn)

			// Set up existing heartbeat if specified
			if tc.existingRouter != "" {
				supervisor.routerHeartbeats.Store(tc.existingRouter, &function.Heartbeat{
					Function:         fn,
					Timestamp:        tc.existingTime,
					InFlightRequests: 10,
				})
			}

			// Send new heartbeat
			newHeartbeat := &function.Heartbeat{
				Function:         fn,
				Timestamp:        tc.newTime,
				InFlightRequests: 20,
			}
			supervisor.heartbeat(tc.newRouter, newHeartbeat)

			// Verify heartbeat count
			count := 0
			supervisor.routerHeartbeats.Range(func(_ string, _ *function.Heartbeat) bool {
				count++
				return true
			})
			assert.Equal(t, tc.expectedCount, count)

			// Verify whether the heartbeat was updated
			stored, ok := supervisor.routerHeartbeats.Load(tc.newRouter)
			assert.Assert(t, ok)
			if tc.shouldUpdate {
				assert.Assert(t, stored.Timestamp.Equal(tc.newTime))
				assert.Equal(t, 20, stored.InFlightRequests)
			} else {
				assert.Assert(t, stored.Timestamp.Equal(tc.existingTime))
				assert.Equal(t, 10, stored.InFlightRequests)
			}
		})
	}
}

// TestHeartbeatGarbageCollection tests that expired router heartbeats are garbage collected.
func TestHeartbeatGarbageCollection(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HeartbeatTimeout = 30 * time.Second

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
	ctrl := New(cfg, nil, fakeKubernetes, nil)
	supervisor := ctrl.supervisor(fn)

	// Store an expired heartbeat
	expiredTime := time.Now().Add(-cfg.HeartbeatTimeout - time.Second)
	supervisor.routerHeartbeats.Store(fixture.RouterIP, &function.Heartbeat{
		Function:  fn,
		Timestamp: expiredTime,
	})

	// Store a fresh heartbeat from a different router
	freshTime := time.Now()
	supervisor.routerHeartbeats.Store(fixture.RouterIP2, &function.Heartbeat{
		Function:  fn,
		Timestamp: freshTime,
	})

	// Trigger garbage collection by sending any heartbeat
	supervisor.heartbeat("127.0.1.99", &function.Heartbeat{
		Function:  fn,
		Timestamp: time.Now(),
	})

	// Verify expired heartbeat was removed
	_, expiredExists := supervisor.routerHeartbeats.Load(fixture.RouterIP)
	assert.Assert(t, !expiredExists, "expired heartbeat should be garbage collected")

	// Verify fresh heartbeat remains
	_, freshExists := supervisor.routerHeartbeats.Load(fixture.RouterIP2)
	assert.Assert(t, freshExists, "fresh heartbeat should remain")
}

// TestCombinedHeartbeat tests the aggregation of heartbeats from multiple routers.
// The combinedHeartbeat function sums in-flight requests from all routers and uses
// the most recent timestamp from either router heartbeats or instance assignments.
func TestCombinedHeartbeat(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	fn := fixture.NewFunction(t)

	testCases := []struct {
		name                     string
		routerHeartbeats         map[string]*function.Heartbeat
		instances                []*function.Instance
		expectedInFlightRequests int
		expectedTimestampSource  string // "router1", "router2", "instance"
	}{
		{
			// Single router heartbeat
			name: "single router heartbeat",
			routerHeartbeats: map[string]*function.Heartbeat{
				fixture.RouterIP: {
					Function:         fn,
					Timestamp:        time.Now(),
					InFlightRequests: 10,
				},
			},
			instances:                nil,
			expectedInFlightRequests: 10,
			expectedTimestampSource:  "router1",
		},
		{
			// Multiple routers: sums in-flight requests
			name: "sums in-flight requests from multiple routers",
			routerHeartbeats: map[string]*function.Heartbeat{
				fixture.RouterIP: {
					Function:         fn,
					Timestamp:        time.Now().Add(-time.Minute),
					InFlightRequests: 10,
				},
				fixture.RouterIP2: {
					Function:         fn,
					Timestamp:        time.Now(),
					InFlightRequests: 15,
				},
			},
			instances:                nil,
			expectedInFlightRequests: 25,
			expectedTimestampSource:  "router2",
		},
		{
			// Instance AssignedAt is newer than router heartbeats
			name: "uses instance AssignedAt when newer than router heartbeats",
			routerHeartbeats: map[string]*function.Heartbeat{
				fixture.RouterIP: {
					Function:         fn,
					Timestamp:        time.Now().Add(-time.Minute),
					InFlightRequests: 5,
				},
			},
			instances: []*function.Instance{
				{Function: fn, AssignedAt: time.Now()},
			},
			expectedInFlightRequests: 5,
			expectedTimestampSource:  "instance",
		},
		{
			// Router heartbeat is newer than instance AssignedAt
			name: "uses router heartbeat timestamp when newer than instance",
			routerHeartbeats: map[string]*function.Heartbeat{
				fixture.RouterIP: {
					Function:         fn,
					Timestamp:        time.Now(),
					InFlightRequests: 8,
				},
			},
			instances: []*function.Instance{
				{Function: fn, AssignedAt: time.Now().Add(-time.Minute)},
			},
			expectedInFlightRequests: 8,
			expectedTimestampSource:  "router1",
		},
		{
			// No heartbeats or instances: zero values
			name:                     "returns zero values when no heartbeats",
			routerHeartbeats:         nil,
			instances:                nil,
			expectedInFlightRequests: 0,
			expectedTimestampSource:  "zero",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			ctrl := New(cfg, nil, fakeKubernetes, nil)
			supervisor := ctrl.supervisor(fn)

			// Set up router heartbeats
			var router1Time, router2Time time.Time
			for routerIP, hb := range tc.routerHeartbeats {
				supervisor.routerHeartbeats.Store(routerIP, hb)
				switch routerIP {
				case fixture.RouterIP:
					router1Time = hb.Timestamp
				case fixture.RouterIP2:
					router2Time = hb.Timestamp
				}
			}

			// Get instance timestamp if present
			var instanceTime time.Time
			if len(tc.instances) > 0 {
				instanceTime = tc.instances[0].AssignedAt
			}

			combined := supervisor.combinedHeartbeat(tc.instances)

			// Verify in-flight requests sum
			assert.Equal(t, tc.expectedInFlightRequests, combined.InFlightRequests)

			// Verify timestamp source
			switch tc.expectedTimestampSource {
			case "router1":
				assert.Assert(t, combined.Timestamp.Equal(router1Time))
			case "router2":
				assert.Assert(t, combined.Timestamp.Equal(router2Time))
			case "instance":
				assert.Assert(t, combined.Timestamp.Equal(instanceTime))
			case "zero":
				assert.Assert(t, combined.Timestamp.IsZero())
			}

			// Verify function is set
			assert.Assert(t, combined.Function == fn)
		})
	}
}

// TestGetReadyInstance tests the instance selection logic including filtering,
// scaling up when no instances are available, and respecting max instances.
func TestGetReadyInstance(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *function.Function
		fakeKubernetes *fake.Clientset
		instance       *function.Instance
	}

	testCases := []struct {
		name         string
		excludeNames []string
		setup        func(*testing.T, *testState)
		check        func(*testing.T, *testState)
		err          error
	}{
		{
			// Returns a ready instance when available
			name: "returns ready instance when available",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.instance != nil)
				assert.Assert(t, state.instance.Function.Equal(state.fn))
			},
		},
		{
			// Scales up when no instances are available
			name: "scales up when no instances available",
			setup: func(t *testing.T, state *testState) {
				// Add an available pod that can be assigned
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.instance != nil)
			},
		},
		{
			// Filters instances by excludeNames
			name:         "filters instances by excludeNames",
			excludeNames: []string{"excluded-pod"},
			setup: func(t *testing.T, state *testState) {
				// Add excluded pod
				excludedPod := fixture.NewAssignedPod(t, state.fn, nil)
				excludedPod.Name = "excluded-pod"
				state.fakeKubernetes.Tracker().Add(excludedPod)

				// Add included pod
				includedPod := fixture.NewAssignedPod(t, state.fn, nil)
				includedPod.Name = "included-pod"
				state.fakeKubernetes.Tracker().Add(includedPod)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.instance != nil)
				assert.Equal(t, "included-pod", state.instance.Name)
			},
		},
		{
			// Falls back to all instances when all would be excluded
			name:         "falls back to all instances when all excluded",
			excludeNames: []string{"pod-1", "pod-2"},
			setup: func(t *testing.T, state *testState) {
				// Add pods that would all be excluded
				pod1 := fixture.NewAssignedPod(t, state.fn, nil)
				pod1.Name = "pod-1"
				state.fakeKubernetes.Tracker().Add(pod1)

				pod2 := fixture.NewAssignedPod(t, state.fn, nil)
				pod2.Name = "pod-2"
				state.fakeKubernetes.Tracker().Add(pod2)
			},
			check: func(t *testing.T, state *testState) {
				// Should still return an instance (fallback behavior)
				assert.Assert(t, state.instance != nil)
				assert.Assert(t, state.instance.Name == "pod-1" || state.instance.Name == "pod-2")
			},
		},
		{
			// Limits instances to maxInstances when more are available
			name: "limits instances to maxInstances",
			setup: func(t *testing.T, state *testState) {
				// Set max instances to 2
				state.fn.Scale.MaxInstances = 2

				// Add more pods than max instances
				for i := range 5 {
					pod := fixture.NewAssignedPod(t, state.fn, nil)
					// Set AssignedAt to different times so newest are kept
					pod.Annotations[key.AssignedAt.Annotation] = time.Now().Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)
					state.fakeKubernetes.Tracker().Add(pod)
				}
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.instance != nil)
			},
		},
		{
			// Times out when no pods available to scale up
			name: "times out when no pods available",
			setup: func(t *testing.T, state *testState) {
				// No pods available
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, state.instance == nil)
			},
			err: context.DeadlineExceeded,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			state := &testState{
				fn:             fixture.NewFunction(t),
				fakeKubernetes: fake.NewClientset(fixture.NewControllerPod()),
			}

			tc.setup(t, state)

			ctrl := New(testConfig(), nil, state.fakeKubernetes, nil)
			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			state.instance, err = ctrl.supervisor(state.fn).getReadyInstance(ctx, tc.excludeNames)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, state)
		})
	}
}

// TestCalculateDesiredInstances tests the multi-metric scaling decision logic.
// The function considers heartbeat timeout, in-flight requests, CPU, and memory metrics,
// taking the max across all metrics and applying min/max clamping.
func TestCalculateDesiredInstances(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.HeartbeatTimeout = 30 * time.Second

	// Instances must be ready past the initial readiness delay to be included in scaling decisions
	readyAt := time.Now().Add(-cfg.HPAInitialReadinessDelay)

	testCases := []struct {
		name                     string
		heartbeat                *function.Heartbeat
		instances                []*function.Instance
		expectedDesiredInstances int
		expectedUnclampedDesired int
		expectedReason           ScalingReason
	}{
		{
			// Heartbeat timeout triggers scale to 0
			name: "scales to zero on heartbeat timeout",
			heartbeat: &function.Heartbeat{
				Function: &function.Function{
					Scale: &function.Scale{
						MinInstances:           0,
						MaxInstances:           5,
						TargetInFlightRequests: 10,
					},
				},
				Timestamp:        time.Now().Add(-cfg.HeartbeatTimeout - time.Second),
				InFlightRequests: 5,
			},
			instances: []*function.Instance{
				{ReadyAt: readyAt},
			},
			expectedDesiredInstances: 0,
			expectedUnclampedDesired: 0,
			expectedReason:           ScalingReasonHeartbeatTimeout,
		},
		{
			// In-flight requests scaling: 25 requests / 10 target = 3 instances
			name: "scales based on in-flight requests",
			heartbeat: &function.Heartbeat{
				Function: &function.Function{
					Scale: &function.Scale{
						MinInstances:           0,
						MaxInstances:           10,
						TargetInFlightRequests: 10,
					},
				},
				Timestamp:        time.Now(),
				InFlightRequests: 25,
			},
			instances: []*function.Instance{
				{ReadyAt: readyAt},
			},
			expectedDesiredInstances: 3,
			expectedUnclampedDesired: 3,
			expectedReason:           ScalingReasonInFlightRequests,
		},
		{
			// Multiple metrics: takes the max across all
			// In-flight: 15 / 10 = 2
			// CPU: avg 200m / 100m = 2
			// Result: max(2, 2) = 2
			name: "takes max across multiple metrics",
			heartbeat: &function.Heartbeat{
				Function: &function.Function{
					Scale: &function.Scale{
						MinInstances:           0,
						MaxInstances:           10,
						TargetInFlightRequests: 10,
						TargetCPUUsageMilli:    100,
					},
				},
				Timestamp:        time.Now(),
				InFlightRequests: 15,
			},
			instances: []*function.Instance{
				{
					Function: &function.Function{
						Scale: &function.Scale{TargetCPUUsageMilli: 100},
					},
					ReadyAt:       readyAt,
					CPUUsageMilli: 200,
				},
			},
			expectedDesiredInstances: 2,
			expectedUnclampedDesired: 2,
			expectedReason:           ScalingReasonInFlightRequests, // or CPU, both equal
		},
		{
			// Max clamping: would scale to 10 but max is 5
			name: "applies max instances clamping",
			heartbeat: &function.Heartbeat{
				Function: &function.Function{
					Scale: &function.Scale{
						MinInstances:           0,
						MaxInstances:           5,
						TargetInFlightRequests: 10,
					},
				},
				Timestamp:        time.Now(),
				InFlightRequests: 100,
			},
			instances: []*function.Instance{
				{ReadyAt: readyAt},
			},
			expectedDesiredInstances: 5,
			expectedUnclampedDesired: 10,
			expectedReason:           ScalingReasonInFlightRequests,
		},
		{
			// Min clamping: unclamped would be 1 (baseline without heartbeat timeout) but min is 2
			name: "applies min instances clamping",
			heartbeat: &function.Heartbeat{
				Function: &function.Function{
					Scale: &function.Scale{
						MinInstances:           2,
						MaxInstances:           10,
						TargetInFlightRequests: 10,
					},
				},
				Timestamp:        time.Now(),
				InFlightRequests: 0,
			},
			instances: []*function.Instance{
				{ReadyAt: readyAt},
			},
			expectedDesiredInstances: 2,
			expectedUnclampedDesired: 1,
			expectedReason:           "",
		},
		{
			// No scaling metrics configured: maintains at least 1 instance
			name: "maintains one instance when no scaling metrics configured",
			heartbeat: &function.Heartbeat{
				Function: &function.Function{
					Scale: &function.Scale{
						MinInstances: 0,
						MaxInstances: 10,
					},
				},
				Timestamp:        time.Now(),
				InFlightRequests: 0,
			},
			instances: []*function.Instance{
				{ReadyAt: readyAt},
			},
			expectedDesiredInstances: 1,
			expectedUnclampedDesired: 1,
			expectedReason:           "",
		},
		{
			// CPU scaling dominates over in-flight requests
			// In-flight: 5 / 10 = 1
			// CPU: avg 300m / 100m = 3
			// Result: max(1, 3) = 3
			name: "CPU scaling dominates when higher",
			heartbeat: &function.Heartbeat{
				Function: &function.Function{
					Scale: &function.Scale{
						MinInstances:           0,
						MaxInstances:           10,
						TargetInFlightRequests: 10,
						TargetCPUUsageMilli:    100,
					},
				},
				Timestamp:        time.Now(),
				InFlightRequests: 5,
			},
			instances: []*function.Instance{
				{
					Function: &function.Function{
						Scale: &function.Scale{TargetCPUUsageMilli: 100},
					},
					ReadyAt:       readyAt,
					CPUUsageMilli: 300,
				},
			},
			expectedDesiredInstances: 3,
			expectedUnclampedDesired: 3,
			expectedReason:           ScalingReasonCPU,
		},
		{
			// Memory scaling
			name: "scales based on memory usage",
			heartbeat: &function.Heartbeat{
				Function: &function.Function{
					Scale: &function.Scale{
						MinInstances:         0,
						MaxInstances:         10,
						TargetMemoryUsageMiB: 100,
					},
				},
				Timestamp: time.Now(),
			},
			instances: []*function.Instance{
				{
					Function: &function.Function{
						Scale: &function.Scale{TargetMemoryUsageMiB: 100},
					},
					ReadyAt:        readyAt,
					MemoryUsageMiB: 250,
				},
			},
			expectedDesiredInstances: 3,
			expectedUnclampedDesired: 3,
			expectedReason:           ScalingReasonMemory,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			decision := calculateDesiredInstances(t.Context(), cfg, tc.heartbeat, tc.instances)

			assert.Equal(t, tc.expectedDesiredInstances, decision.DesiredInstances)
			assert.Equal(t, tc.expectedUnclampedDesired, decision.UnclampedDesiredInstances)
			assert.Equal(t, tc.expectedReason, decision.Reason)
		})
	}
}

// TestIsValidScalingReason tests the scaling reason validation function.
func TestIsValidScalingReason(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		reason   string
		expected bool
	}{
		// Valid scaling reasons
		{reason: ScalingReasonCPU, expected: true},
		{reason: ScalingReasonHeartbeatTimeout, expected: true},
		{reason: ScalingReasonInFlightRequests, expected: true},
		{reason: ScalingReasonMemory, expected: true},
		{reason: ScalingReasonNoReadyInstances, expected: true},
		{reason: ScalingReasonUnknown, expected: true},

		// Invalid scaling reasons
		{reason: "", expected: false},
		{reason: "invalid", expected: false},
		{reason: "CPU", expected: false},  // case-sensitive
		{reason: "cpu ", expected: false}, // trailing space
		{reason: " cpu", expected: false}, // leading space
		{reason: "random", expected: false},
	}

	for _, tc := range testCases {
		t.Run(tc.reason, func(t *testing.T) {
			t.Parallel()

			result := isValidScalingReason(tc.reason)
			assert.Equal(t, tc.expected, result)
		})
	}
}
