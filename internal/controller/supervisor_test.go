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
	testCases := []struct {
		name             string
		desiredInstances int
		err              error
		setup            func(*testing.T, *fake.Clientset, function.Function)
		check            func(*testing.T, *fake.Clientset, []*function.Instance)
	}{
		// ==================== Basic scaling operations ====================
		{
			// Basic scale up: assign an available pod to reach desired instance count
			name:             "scales up by assigning available pod",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)
			},
		},
		{
			// Scale up with surplus pods: only assigns needed pods, leaves extras unassigned
			name:             "only assigns needed pods when extras available",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				for range 5 {
					fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)

				// verify 4 pods remain unassigned
				instance := instances[0]
				pods, err := fakeKubernetes.CoreV1().Pods(instance.Namespace).List(t.Context(), metav1.ListOptions{
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
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// intentionally empty - no pods available
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 0)
			},
		},
		{
			// Metadata mismatch: assigned pod has different metadata, can't be reused
			name:             "ignores pods with different metadata",
			desiredInstances: 1,
			err:              context.DeadlineExceeded,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fn.Metadata = "different"
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 0)
			},
		},
		{
			// Already at desired count: no scaling needed, returns existing instances
			name:             "returns existing instances when already at desired count",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)
			},
		},

		// ==================== Scale down operations ====================
		{
			// Scale down: keeps most recently assigned instance (likely has warmest cache)
			name:             "keeps most recently assigned instance when scaling down",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add max - 1 instances with older assignment times
				for range fn.Scale.MaxInstances - 1 {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add one instance with the most recent assignment time
				pod := fixture.NewAssignedPod(t, fn, nil)
				pod.Name = "most-recent-assigned-at"
				pod.Annotations[key.AssignedAt.Label] = time.Now().Add(time.Second).UTC().Format(time.RFC3339)
				fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)
				assert.Assert(t, instances[0].Name == "most-recent-assigned-at")
			},
		},

		// ==================== Ready/unready instance interactions ====================
		{
			// Scale to max with one unready: assigns new pod to reach max ready instances
			// Unready instances are preserved during scale up (they might become ready soon)
			name:             "scales to max ready instances while preserving unready",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				assert.Assert(t, fn.Scale.MaxInstances == 5)

				// add max - 1 ready instances
				for range fn.Scale.MaxInstances - 1 {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add 1 unready instance
				unreadyPod := fixture.NewAssignedPod(t, fn, nil)
				unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				fakeKubernetes.Tracker().Add(unreadyPod)

				// add 1 available pod for scaling
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				fn := instances[0].Function
				assert.Assert(t, len(instances) == fn.Scale.MaxInstances)

				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
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
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				assert.Assert(t, fn.Scale.MaxInstances == 5)

				// add max + 1 unready instances (exceeds total instance limit)
				for range fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}

				// available pod exists but shouldn't be used
				fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// no ready instances returned because total count exceeds max
				assert.Assert(t, len(instances) == 0)
			},
		},
		{
			// Some ready, many unready: returns ready instances but can't scale up further
			name:             "returns existing ready instances when blocked by total count",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				assert.Assert(t, fn.Scale.MaxInstances == 5)

				// add 2 ready instances
				for range 2 {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add max + 1 unready instances
				for range fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// only 2 ready instances returned (can't scale up due to total count)
				assert.Assert(t, len(instances) == 2)

				fn := instances[0].Function
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
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
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add max + 1 unready instances
				for range fn.Scale.MaxInstances + 1 {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 0)

				// all unready instances should be deleted
				pods, err := fakeKubernetes.CoreV1().Pods(fixture.FunctionNamespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
			},
		},
		{
			// Scale down deletes all unready instances when maintaining one ready
			name:             "deletes all unready instances when scaling down to one",
			desiredInstances: 1,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add 1 ready instance
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))

				// add max unready instances
				for range fn.Scale.MaxInstances {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 1)

				// all unready instances should be deleted
				pods, err := fakeKubernetes.CoreV1().Pods(fixture.FunctionNamespace).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
			},
		},
		{
			// Can't scale up when total instances at limit (preserves unready during scale up)
			name:             "preserves unready instances during blocked scale up",
			desiredInstances: 2,
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add 1 ready instance
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))

				// add max unready instances (1 ready + max unready = max+1 total)
				for range fn.Scale.MaxInstances {
					unreadyPod := fixture.NewAssignedPod(t, fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				// only 1 ready instance returned (can't scale up, already over max total)
				assert.Assert(t, len(instances) == 1)

				fn := instances[0].Function
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
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
			setup: func(t *testing.T, fakeKubernetes *fake.Clientset, fn function.Function) {
				// add max ready instances
				for range fn.Scale.MaxInstances {
					fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
				}

				// add 1 unready instance
				unreadyPod := fixture.NewAssignedPod(t, fn, nil)
				unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				fakeKubernetes.Tracker().Add(unreadyPod)
			},
			check: func(t *testing.T, fakeKubernetes *fake.Clientset, instances []*function.Instance) {
				assert.Assert(t, len(instances) == 2)

				fn := instances[0].Function
				pods, err := fakeKubernetes.CoreV1().Pods(fn.Namespace).List(t.Context(), metav1.ListOptions{})
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
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			fn := fixture.NewFunction()

			tc.setup(t, fakeKubernetes, fn)

			ctrl := New(nil, fakeKubernetes, nil)
			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			instances, err := ctrl.supervisor(fn).scale(ctx, ScalingDecision{
				DesiredInstances: tc.desiredInstances,
				Reason:           "test",
			})
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				assert.NilError(t, err)
			}

			tc.check(t, fakeKubernetes, instances)
		})
	}
}

// TestScaleForwarding verifies that scale requests are forwarded to the responsible
// controller when the current controller is not responsible for the function.
// This happens in multi-controller deployments where functions are sharded across controllers.
func TestScaleForwarding(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	// Create a controller pod with a different IP so this controller won't be responsible
	ctrlPod := fixture.NewControllerPod()
	ctrlPod.Status.PodIP = "127.0.0.2"
	fakeKubernetes := fake.NewClientset(ctrlPod)

	fn := fixture.NewFunction()

	// Set up mock client to handle forwarded scale request
	mcc := fixture.NewMockControllerClient(t)
	mcc.HandleScale(func(ctx context.Context, fn function.Function, desiredInstances int, reason string) ([]*function.Instance, error) {
		return []*function.Instance{fixture.NewInstance(t, fn, nil)}, nil
	})

	ctrl := New(func(host string, port int) Client { return mcc }, fakeKubernetes, nil)

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
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	// Set different values for the two flags to distinguish which one is being used
	// HeartbeatTimeout: 30s (shorter) - used for scaling to 0 when no heartbeats
	// DownscaleStabilization: 90s (longer) - used for stabilization window
	originalHeartbeatTimeout := FlagHeartbeatTimeout.Value()
	originalDownscaleStabilization := FlagHPADownscaleStabilization.Value()
	t.Cleanup(func() {
		_ = FlagHeartbeatTimeout.SetValue(originalHeartbeatTimeout)
		_ = FlagHPADownscaleStabilization.SetValue(originalDownscaleStabilization)
	})

	_ = FlagHeartbeatTimeout.SetValue(30 * time.Second)
	_ = FlagHPADownscaleStabilization.SetValue(90 * time.Second)

	fn := fixture.NewFunction()
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	// Create 2 assigned pods
	pod1 := fixture.NewAssignedPod(t, fn, nil)
	pod2 := fixture.NewAssignedPod(t, fn, nil)
	fakeKubernetes.Tracker().Add(pod1)
	fakeKubernetes.Tracker().Add(pod2)

	ctrl := New(nil, fakeKubernetes, nil)
	ctrl.startedAt = time.Now().Add(-(FlagHPADownscaleStabilization.Value() + time.Second))
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
	supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{
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
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction()
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	// Create assigned pods for the function
	for range 3 {
		fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
	}

	ctrl := New(nil, fakeKubernetes, nil)
	ctrl.startedAt = time.Now().Add(-(FlagHPADownscaleStabilization.Value() + time.Second))
	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	supervisor := ctrl.supervisor(fn)

	// Add a heartbeat so we don't scale to 0
	supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{
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
				supervisor.routerHeartbeats.Store(fixture.RouterIP, function.Heartbeat{
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
	// Instances must be ready past the initial readiness delay to be included in scaling decisions
	readyAt := time.Now().Add(-FlagHPAInitialReadinessDelay.Value())

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
			// Configure target usage for each instance
			for _, pm := range tc.podMetrics {
				switch tc.metricName {
				case MetricCPU:
					pm.Scale.TargetCPUUsageMilli = tc.targetUsage
				case MetricMemory:
					pm.Scale.TargetMemoryUsageMiB = tc.targetUsage
				}
			}

			instances, _ := calculateDesiredInstancesForMetric(t.Context(), tc.metricName, tc.podMetrics)
			assert.Assert(t, instances == tc.expectedInstances)
		})
	}
}
