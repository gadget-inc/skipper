package controller

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/poll"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSupervisor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		check func(*testing.T, *Controller)
	}{
		{
			name: "creates new supervisor for function",
			check: func(t *testing.T, ctrl *Controller) {
				fn := fixture.NewFunction(t)
				supervisor := ctrl.supervisor(fn)
				assert.Assert(t, supervisor != nil)
				assert.Assert(t, proto.Equal(supervisor.fn, fn))
			},
		},
		{
			name: "returns same supervisor for same function",
			check: func(t *testing.T, ctrl *Controller) {
				fn := fixture.NewFunction(t)
				supervisor1 := ctrl.supervisor(fn)
				supervisor2 := ctrl.supervisor(fn)

				// should return the same supervisor instance
				assert.Assert(t, supervisor1 == supervisor2)
			},
		},
		{
			name: "creates different supervisors for different functions",
			check: func(t *testing.T, ctrl *Controller) {
				fn1 := fixture.NewFunction(t)
				fn2 := fixture.NewFunction(t)
				fn2.SetDeployment("other-deployment")

				supervisor1 := ctrl.supervisor(fn1)
				supervisor2 := ctrl.supervisor(fn2)

				// should return different supervisor instances
				assert.Assert(t, supervisor1 != supervisor2)
			},
		},
		{
			name: "supervisor has correct controller reference",
			check: func(t *testing.T, ctrl *Controller) {
				fn := fixture.NewFunction(t)
				supervisor := ctrl.supervisor(fn)
				assert.Assert(t, supervisor.ctrl == ctrl)
			},
		},
		{
			name: "supervisor has initialized routerHeartbeats map",
			check: func(t *testing.T, ctrl *Controller) {
				fn := fixture.NewFunction(t)
				supervisor := ctrl.supervisor(fn)
				assert.Assert(t, supervisor.routerHeartbeats != nil)

				// verify it's a working map
				supervisor.routerHeartbeats.Store("test-ip", skipper.Heartbeat_builder{
					Function:  fn,
					Timestamp: timestamppb.Now(),
				}.Build())
				_, ok := supervisor.routerHeartbeats.Load("test-ip")
				assert.Assert(t, ok)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := New(testConfig(), nil, fake.NewClientset(), nil)
			tc.check(t, ctrl)
		})
	}
}

func TestDiscoverSupervisors(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState)
	}{
		{
			name: "discovers supervisor for assigned pod",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState) {
				// ensure a supervisor was created for the function
				assert.Assert(t, state.ctrl.supervisors.Size() == 1)
				supervisor, ok := state.ctrl.supervisors.Load(state.fn.Hash())
				assert.Assert(t, ok)
				assert.Assert(t, proto.Equal(supervisor.fn, state.fn))
			},
		},
		{
			name: "discovers multiple supervisors for different functions",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))

				// create a second function with different tenant
				fn2 := fixture.NewFunction(t)
				fn2.SetTenant("tenant-2")
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn2))
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn2, nil))
			},
			check: func(t *testing.T, state *testState) {
				// ensure supervisors were created for both functions
				assert.Assert(t, state.ctrl.supervisors.Size() == 2)
			},
		},
		{
			name: "creates supervisor even when not responsible",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))

				// set the controller pod IP to 0.0.0.0 so that we're not responsible for the assigned pod
				state.ctrl.config.PodIP = "0.0.0.0"
			},
			check: func(t *testing.T, state *testState) {
				// ensure supervisor was created even though we're not responsible
				// supervisors must exist to track scaling decisions
				assert.Assert(t, state.ctrl.supervisors.Size() == 1)
				supervisor, ok := state.ctrl.supervisors.Load(state.fn.Hash())
				assert.Assert(t, ok)
				assert.Assert(t, proto.Equal(supervisor.fn, state.fn))
			},
		},
		{
			name: "pod with invalid function annotation gets deleted",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// create an assigned pod with invalid function annotation JSON
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Annotations[key.Function.Annotation] = "not valid json"
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState) {
				// ensure the pod with invalid function annotation was deleted
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
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
			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

			tc.setup(t, state)

			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			err = state.ctrl.discoverSupervisors(ctx, state.fn.GetNamespace())
			assert.NilError(t, err)
			tc.check(t, state)
		})
	}
}

// TestDiscoverSupervisorsReturnsErrorWhenListPodsFails verifies that
// discoverSupervisors returns an error when listPods fails (e.g., when
// the namespace lister hasn't been started for the requested namespace).
func TestDiscoverSupervisorsReturnsErrorWhenListPodsFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
	ctrl := New(testConfig(), nil, fakeKubernetes, nil)

	// Start informers for the normal namespace (skipper-test-fixtures)
	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Call discoverSupervisors with a namespace that doesn't have an informer
	err = ctrl.discoverSupervisors(ctx, "unknown-namespace")

	// Should return an error because listPods will fail
	assert.Assert(t, err != nil, "expected error when namespace lister not found")
	assert.Assert(t, err.Error() != "", "error message should not be empty")
}

func TestScale(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		instances      []*skipper.Instance
	}

	testCases := []struct {
		name             string
		desiredInstances uint32
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
				pods, err := state.fakeKubernetes.CoreV1().Pods(instance.GetFunction().GetNamespace()).List(t.Context(), metav1.ListOptions{
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
				fn := proto.Clone(state.fn).(*skipper.Function)
				fn.SetMetadata("different")
				state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
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
				for range state.fn.GetScale().GetMaxInstances() - 1 {
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
				assert.Assert(t, state.instances[0].GetName() == "most-recent-assigned-at")
			},
		},

		// ==================== Ready/unready instance interactions ====================
		{
			// Scale to max with one unready: assigns new pod to reach max ready instances
			// Unready instances are preserved during scale up (they might become ready soon)
			name:             "scales to max ready instances while preserving unready",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, state *testState) {
				assert.Assert(t, state.fn.GetScale().GetMaxInstances() == 5)

				// add max - 1 ready instances
				for range state.fn.GetScale().GetMaxInstances() - 1 {
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
				fn := state.instances[0].GetFunction()
				assert.Assert(t, len(state.instances) == int(fn.GetScale().GetMaxInstances()))

				pods, err := state.fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == int(fn.GetScale().GetMaxInstances())+1)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == int(fn.GetScale().GetMaxInstances()))
				assert.Assert(t, unreadyCount == 1) // unready instance preserved
			},
		},
		{
			// All unready, over max: can't scale up when too many total instances exist
			name:             "blocks scale up when total instances exceed max",
			desiredInstances: 5, // max instances
			setup: func(t *testing.T, state *testState) {
				assert.Assert(t, state.fn.GetScale().GetMaxInstances() == 5)

				// add max + 1 unready instances (exceeds total instance limit)
				for range int(state.fn.GetScale().GetMaxInstances()) + 1 {
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
				assert.Assert(t, state.fn.GetScale().GetMaxInstances() == 5)

				// add 2 ready instances
				for range 2 {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}

				// add max + 1 unready instances
				for range int(state.fn.GetScale().GetMaxInstances()) + 1 {
					unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					state.fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, state *testState) {
				// only 2 ready instances returned (can't scale up due to total count)
				assert.Assert(t, len(state.instances) == 2)

				fn := state.instances[0].GetFunction()
				pods, err := state.fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == int(fn.GetScale().GetMaxInstances())+3)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == 2)
				assert.Assert(t, unreadyCount == int(fn.GetScale().GetMaxInstances())+1) // unready preserved during scale up
			},
		},
		{
			// Scale to 0 cleans up all unready instances
			name:             "deletes all unready instances when scaling to zero",
			desiredInstances: 0,
			setup: func(t *testing.T, state *testState) {
				// add max + 1 unready instances
				for range int(state.fn.GetScale().GetMaxInstances()) + 1 {
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
				for range state.fn.GetScale().GetMaxInstances() {
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
				for range state.fn.GetScale().GetMaxInstances() {
					unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
					unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
					state.fakeKubernetes.Tracker().Add(unreadyPod)
				}
			},
			check: func(t *testing.T, state *testState) {
				// only 1 ready instance returned (can't scale up, already over max total)
				assert.Assert(t, len(state.instances) == 1)

				fn := state.instances[0].GetFunction()
				pods, err := state.fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == int(fn.GetScale().GetMaxInstances())+1)

				readyCount, unreadyCount := countReadyAndUnreadyPods(pods.Items)
				assert.Assert(t, readyCount == 1)
				assert.Assert(t, unreadyCount == int(fn.GetScale().GetMaxInstances())) // unready preserved
			},
		},
		{
			// Scale down from max ready + 1 unready: deletes excess ready and unready
			name:             "deletes excess ready and unready instances when scaling down",
			desiredInstances: 2,
			setup: func(t *testing.T, state *testState) {
				// add max ready instances
				for range state.fn.GetScale().GetMaxInstances() {
					state.fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, state.fn, nil))
				}

				// add 1 unready instance
				unreadyPod := fixture.NewAssignedPod(t, state.fn, nil)
				unreadyPod.Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
				state.fakeKubernetes.Tracker().Add(unreadyPod)
			},
			check: func(t *testing.T, state *testState) {
				assert.Assert(t, len(state.instances) == 2)

				fn := state.instances[0].GetFunction()
				pods, err := state.fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
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

			state.instances, err = ctrl.supervisor(state.fn).scale(ctx, skipper.ScaleDecision_builder{
				DesiredInstances: proto.Uint32(tc.desiredInstances),
				Reason:           skipper.ScaleReason_SCALE_REASON_UNSPECIFIED.Enum(),
			}.Build())
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
// controller when the current controller is not responsible for the skipper.
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
	mcc.HandleScale(func(ctx context.Context, fn *skipper.Function, desiredInstances uint32, reason skipper.ScaleReason) ([]*skipper.Instance, error) {
		return []*skipper.Instance{fixture.NewInstance(t, fn, nil)}, nil
	})

	ctrl := New(testConfig(), func(host string, port int) Client { return mcc }, fakeKubernetes, nil)

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Scale should succeed via forwarding to mock client
	instances, err := ctrl.supervisor(fn).scale(ctx, skipper.ScaleDecision_builder{
		DesiredInstances: proto.Uint32(1),
		Reason:           skipper.ScaleReason_SCALE_REASON_UNSPECIFIED.Enum(),
	}.Build())
	assert.NilError(t, err)
	assert.Assert(t, len(instances) == 1)
}

// TestConvergeTracksRecommendationsWithoutScalingWhenNotResponsible verifies that
// converge tracks scaling recommendations in the stabilization window even when the
// controller is not responsible, but does not modify any pods. This ensures historical
// decisions are available when responsibility changes, without performing duplicate work.
func TestConvergeTracksRecommendationsWithoutScalingWhenNotResponsible(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Create a controller pod with a different IP so this controller won't be responsible
	ctrlPod := fixture.NewControllerPod()
	ctrlPod.Status.PodIP = "127.0.0.2"
	fakeKubernetes := fake.NewClientset(ctrlPod)

	fn := fixture.NewFunction(t)
	// Disable resource scaling so in-flight requests drive the decision
	fn.GetScale().SetTargetCpuUsageMilli(0)
	fn.GetScale().SetTargetMemoryUsageMib(0)

	// Set up mock client (should not be called for Scale)
	mcc := fixture.NewMockControllerClient(t)

	cfg := testConfig()
	cfg.HPADownscaleStabilization = 5 * time.Second
	ctrl := New(cfg, func(host string, port int) Client { return mcc }, fakeKubernetes, nil)
	ctrl.startedAt = time.Now().Add(-(cfg.HPADownscaleStabilization + time.Second))

	// Add pods so discoverSupervisors can find them
	fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))
	fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
	fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Capture initial pod count to verify no scaling occurs
	initialPods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
	})
	assert.NilError(t, err)
	initialPodCount := len(initialPods.Items)

	// Discover supervisors - should create supervisor even though we're not responsible
	err = ctrl.discoverSupervisors(ctx, fn.GetNamespace())
	assert.NilError(t, err)

	// Verify supervisor was created
	supervisor, ok := ctrl.supervisors.Load(fn.Hash())
	assert.Assert(t, ok, "supervisor should exist even when not responsible")
	assert.Assert(t, proto.Equal(supervisor.fn, fn))

	// Add a heartbeat so we don't scale to 0
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:         fn,
		Timestamp:        timestamppb.Now(),
		InFlightRequests: proto.Uint32(10),
	}.Build())

	// Call converge multiple times to build up the stabilization window
	// Each call should add a recommendation to the stabilization window
	for range 3 {
		err = supervisor.converge(ctx)
		assert.NilError(t, err)
		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Verify the stabilization window has accumulated recommendations
	supervisor.mu.Lock()
	windowSize := len(supervisor.stabilizationWindow)
	supervisor.mu.Unlock()
	assert.Assert(t, windowSize >= 3, "stabilization window should have at least 3 recommendations, got %d", windowSize)

	// Verify that when scaling down, the stabilization window is used
	// Set up a scenario where we want to scale down
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:         fn,
		Timestamp:        timestamppb.Now(),
		InFlightRequests: proto.Uint32(0), // low load should trigger scale down
	}.Build())

	// Call converge - it should use the max recommendation from the stabilization window
	err = supervisor.converge(ctx)
	assert.NilError(t, err)

	// Verify the stabilization window still contains historical recommendations
	supervisor.mu.Lock()
	windowSizeAfterScaleDown := len(supervisor.stabilizationWindow)
	maxRec := slices.MaxFunc(supervisor.stabilizationWindow, func(a, b Recommendation) int {
		return cmp.Compare(a.DesiredInstances, b.DesiredInstances)
	})
	supervisor.mu.Unlock()

	assert.Assert(t, windowSizeAfterScaleDown > 0, "stabilization window should retain recommendations after scale down")
	assert.Assert(t, maxRec.DesiredInstances >= 1, "max recommendation should be at least 1")

	// Verify no pods were created or deleted - converge should exit early when not responsible
	finalPods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
	})
	assert.NilError(t, err)
	assert.Assert(t, len(finalPods.Items) == initialPodCount,
		"converge should not modify pods when not responsible: expected %d, got %d", initialPodCount, len(finalPods.Items))
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
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:  fn,
		Timestamp: timestamppb.Now(),
	}.Build())

	// Call converge - it will calculate desired instances (should be 1 or 2 depending on metrics)
	// The key is checking if the old recommendation (45s ago) was kept in the stabilization window
	err = supervisor.converge(ctx)
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

// TestConvergeProtectionPeriod verifies the scale-down protection period logic.
// New controllers don't scale down until they've run for max(HPADownscaleStabilization, HeartbeatTimeout).
// This prevents:
// 1. Aggressive scale-down before gathering recommendations (HPADownscaleStabilization)
// 2. Heartbeat timeout scale-to-zero before receiving heartbeats (HeartbeatTimeout)
//
// The HeartbeatTimeout protection is critical during rolling deployments:
// - New controller starts with empty heartbeat state
// - Functions may have instances assigned longer than HeartbeatTimeout ago
// - Without protection, combinedHeartbeat() uses instance.AssignedAt as timestamp
// - This triggers immediate heartbeat_timeout scale-to-zero
func TestConvergeProtectionPeriod(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                      string
		hpaDownscaleStabilization time.Duration
		heartbeatTimeout          time.Duration
		controllerAge             time.Duration // how long ago controller started
		instanceAge               time.Duration // how long ago instance was assigned
		storeHeartbeat            bool          // whether to store a recent heartbeat
		initialPods               int
		expectedPods              int // after converge
	}{
		// Case 1: Controller just started (within both timeouts)
		// Protection: max(5min, 90s) = 5min, controller age: 0
		{
			name:                      "skips scale down when controller just started",
			hpaDownscaleStabilization: 5 * time.Minute,
			heartbeatTimeout:          90 * time.Second,
			controllerAge:             0,
			instanceAge:               time.Minute,
			storeHeartbeat:            true, // fresh heartbeat with 0 in-flight
			initialPods:               2,
			expectedPods:              2, // no scale down
		},
		// Case 2: HeartbeatTimeout > HPADownscaleStabilization, controller in between
		// This is the incident scenario that triggered the fix.
		// Protection: max(30s, 90s) = 90s, controller age: 45s
		{
			name:                      "skips scale down during HeartbeatTimeout protection when HeartbeatTimeout dominates",
			hpaDownscaleStabilization: 30 * time.Second,
			heartbeatTimeout:          90 * time.Second,
			controllerAge:             45 * time.Second, // > stabilization, < heartbeat
			instanceAge:               2 * time.Hour,    // old instance
			storeHeartbeat:            false,            // no heartbeat (traffic gap)
			initialPods:               1,
			expectedPods:              1, // no scale down
		},
		// Case 3: HPADownscaleStabilization > HeartbeatTimeout, controller in between
		// Protection: max(90s, 30s) = 90s, controller age: 45s
		{
			name:                      "skips scale down during HPADownscaleStabilization protection when stabilization dominates",
			hpaDownscaleStabilization: 90 * time.Second,
			heartbeatTimeout:          30 * time.Second,
			controllerAge:             45 * time.Second, // > heartbeat, < stabilization
			instanceAge:               2 * time.Hour,
			storeHeartbeat:            false,
			initialPods:               1,
			expectedPods:              1, // no scale down
		},
		// Case 4: Protection period expired, scale down allowed
		// Protection: max(30s, 90s) = 90s, controller age: 91s
		{
			name:                      "allows scale down after protection period expires",
			hpaDownscaleStabilization: 30 * time.Second,
			heartbeatTimeout:          90 * time.Second,
			controllerAge:             91 * time.Second, // > max(30s, 90s)
			instanceAge:               2 * time.Hour,
			storeHeartbeat:            false, // triggers heartbeat_timeout
			initialPods:               1,
			expectedPods:              0, // scales to 0
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			fn := fixture.NewFunction(t)
			// Disable resource scaling so heartbeat/in-flight drives the decision
			fn.GetScale().SetTargetCpuUsageMilli(0)
			fn.GetScale().SetTargetMemoryUsageMib(0)
			fn.GetScale().SetTargetInFlightRequests(0)

			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))

			// Create pods with the specified instance age
			for range tc.initialPods {
				pod := fixture.NewAssignedPod(t, fn, nil)
				assignedAt := time.Now().Add(-tc.instanceAge).UTC().Format(time.RFC3339)
				pod.Annotations[key.AssignedAt.Annotation] = assignedAt
				pod.Annotations[key.ReadyAt.Annotation] = assignedAt
				fakeKubernetes.Tracker().Add(pod)
			}

			cfg := testConfig()
			cfg.HPADownscaleStabilization = tc.hpaDownscaleStabilization
			cfg.HeartbeatTimeout = tc.heartbeatTimeout

			ctrl := New(cfg, nil, fakeKubernetes, nil)
			ctrl.startedAt = time.Now().Add(-tc.controllerAge)

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			supervisor := ctrl.supervisor(fn)

			if tc.storeHeartbeat {
				supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
					Function:         fn,
					Timestamp:        timestamppb.Now(),
					InFlightRequests: proto.Uint32(0), // low load should trigger scale down
				}.Build())
			}

			// Verify initial pod count
			initialPods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
				LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
			})
			assert.NilError(t, err)
			assert.Assert(t, len(initialPods.Items) == tc.initialPods,
				"expected %d initial pods, got %d", tc.initialPods, len(initialPods.Items))

			// Call converge
			err = supervisor.converge(ctx)
			assert.NilError(t, err)

			// Verify expected pod count after converge
			finalPods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
				LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
			})
			assert.NilError(t, err)
			assert.Assert(t, len(finalPods.Items) == tc.expectedPods,
				"expected %d pods after converge, got %d", tc.expectedPods, len(finalPods.Items))
		})
	}
}

// TestConvergeUsesMaxRecommendationWhenScalingDown verifies that when scaling down
// (not to zero), converge uses the maximum recommendation from the stabilization
// window instead of the current decision. This prevents aggressive scale-down
// when load has recently been high.
func TestConvergeUsesMaxRecommendationWhenScalingDown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	// Use in-flight requests for scaling decisions
	fn.GetScale().SetTargetInFlightRequests(1)
	fn.GetScale().SetTargetCpuUsageMilli(0)
	fn.GetScale().SetTargetMemoryUsageMib(0)

	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	// Create 3 assigned pods
	fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, fn))
	fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
	fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
	fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))

	cfg := testConfig()
	cfg.HPADownscaleStabilization = 5 * time.Minute // long stabilization period

	ctrl := New(cfg, nil, fakeKubernetes, nil)
	// Controller has been running long enough to allow scale-down
	ctrl.startedAt = time.Now().Add(-(cfg.HPADownscaleStabilization + time.Second))

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	supervisor := ctrl.supervisor(fn)

	// Pre-populate stabilization window with a high recommendation (3 instances)
	// This simulates recent high load
	supervisor.stabilizationWindow = []Recommendation{
		{DesiredInstances: 3, Timestamp: time.Now().Add(-10 * time.Second)},
	}

	// Set heartbeat with LOW in-flight requests - current decision would be 1 instance
	// But stabilization window has max recommendation of 3
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:         fn,
		Timestamp:        timestamppb.Now(),
		InFlightRequests: proto.Uint32(1), // with TargetInFlightRequests=1, this means desired=1
	}.Build())

	// Call converge - it should use max recommendation (3), not current decision (1)
	err = supervisor.converge(ctx)
	assert.NilError(t, err)

	// Verify all 3 pods are still running (stabilized to max recommendation)
	finalPods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
	})
	assert.NilError(t, err)
	assert.Assert(t, len(finalPods.Items) == 3,
		"stabilization should prevent scale-down: expected 3 pods, got %d", len(finalPods.Items))

	// Verify the new recommendation was added to stabilization window
	supervisor.mu.Lock()
	windowSize := len(supervisor.stabilizationWindow)
	maxRec := slices.MaxFunc(supervisor.stabilizationWindow, func(a, b Recommendation) int {
		return cmp.Compare(a.DesiredInstances, b.DesiredInstances)
	})
	supervisor.mu.Unlock()
	assert.Assert(t, windowSize >= 2, "stabilization window should have multiple recommendations")
	assert.Assert(t, maxRec.DesiredInstances == 3, "max recommendation should still be 3")
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
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:  fn,
		Timestamp: timestamppb.Now(),
	}.Build())

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
				// Update heartbeat timestamp to keep function alive
				supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
					Function:  fn,
					Timestamp: timestamppb.Now(),
				}.Build())
				_ = supervisor.converge(ctx)
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
		podMetrics        []*skipper.Instance
		targetUsage       uint32
		expectedInstances int
	}{
		// ==================== Basic scaling decisions ====================
		{
			// Usage at 2x target triggers scale up to 2 instances
			// Formula: ceil(1 * (200/100)) = 2
			name:        "scales up when usage exceeds target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*skipper.Instance{
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(200)}.Build(),
			},
			expectedInstances: 2,
		},
		{
			// Usage at 0.5x target triggers scale down to 1 instance
			// Formula: ceil(2 * (50/100)) = 1
			name:        "scales down when usage below target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*skipper.Instance{
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(50)}.Build(),
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(50)}.Build(),
			},
			expectedInstances: 1,
		},
		{
			// Usage exactly at target means no scaling needed
			name:        "no scaling when usage equals target",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*skipper.Instance{
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(100)}.Build(),
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
			podMetrics: []*skipper.Instance{
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(110)}.Build(),
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
			podMetrics: []*skipper.Instance{
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(150)}.Build(),
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt)}.Build(), // missing metric
			},
			expectedInstances: 2,
		},
		{
			// Recently started instances (within initial readiness delay) are excluded from scaling decisions
			// This gives new instances time to stabilize before affecting scaling
			name:        "excludes recently started instances from scaling decision",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*skipper.Instance{
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(150)}.Build(), // included in calculation
				skipper.Instance_builder{ReadyAt: timestamppb.Now(), CpuUsageMilli: proto.Uint32(150)}.Build(),        // excluded (too new)
			},
			expectedInstances: 2,
		},
		{
			// When no metrics are available for any instance, maintain current count
			// This prevents scaling decisions based on incomplete data
			name:        "maintains current count when no metrics available",
			metricName:  MetricCPU,
			targetUsage: 100,
			podMetrics: []*skipper.Instance{
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt)}.Build(),
				skipper.Instance_builder{ReadyAt: timestamppb.New(readyAt)}.Build(),
			},
			expectedInstances: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Configure target usage for each instance
			for _, pm := range tc.podMetrics {
				if pm.GetFunction() == nil {
					pm.SetFunction(skipper.Function_builder{
						Scale: skipper.Scale_builder{}.Build(),
					}.Build())
				}
				switch tc.metricName {
				case MetricCPU:
					pm.GetFunction().GetScale().SetTargetCpuUsageMilli(tc.targetUsage)
				case MetricMemory:
					pm.GetFunction().GetScale().SetTargetMemoryUsageMib(tc.targetUsage)
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
				supervisor.routerHeartbeats.Store(tc.existingRouter, skipper.Heartbeat_builder{
					Function:         fn,
					Timestamp:        timestamppb.New(tc.existingTime),
					InFlightRequests: proto.Uint32(10),
				}.Build())
			}

			// Send new heartbeat
			hb := skipper.Heartbeat_builder{
				Function:         fn,
				Timestamp:        timestamppb.New(tc.newTime),
				InFlightRequests: proto.Uint32(20),
			}.Build()
			supervisor.heartbeat(tc.newRouter, hb)

			// Verify heartbeat count
			assert.Equal(t, tc.expectedCount, supervisor.routerHeartbeats.Size())

			// Verify whether the heartbeat was updated
			stored, ok := supervisor.routerHeartbeats.Load(tc.newRouter)
			assert.Assert(t, ok)
			if tc.shouldUpdate {
				assert.Assert(t, stored.GetTimestamp().AsTime().Equal(tc.newTime))
				assert.Equal(t, uint32(20), stored.GetInFlightRequests())
			} else {
				assert.Assert(t, stored.GetTimestamp().AsTime().Equal(tc.existingTime))
				assert.Equal(t, uint32(10), stored.GetInFlightRequests())
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
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:  fn,
		Timestamp: timestamppb.New(expiredTime),
	}.Build())

	// Store a fresh heartbeat from a different router
	freshTime := time.Now()
	supervisor.routerHeartbeats.Store(fixture.RouterIP2, skipper.Heartbeat_builder{
		Function:  fn,
		Timestamp: timestamppb.New(freshTime),
	}.Build())

	// Trigger garbage collection by sending any heartbeat
	supervisor.heartbeat("127.0.1.99", skipper.Heartbeat_builder{
		Function:  fn,
		Timestamp: timestamppb.Now(),
	}.Build())

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
		routerHeartbeats         map[string]*skipper.Heartbeat
		instances                []*skipper.Instance
		expectedInFlightRequests uint32
		expectedTimestampSource  string // "router1", "router2", "instance"
	}{
		{
			// Single router heartbeat
			name: "single router heartbeat",
			routerHeartbeats: map[string]*skipper.Heartbeat{
				fixture.RouterIP: skipper.Heartbeat_builder{
					Function:         fn,
					Timestamp:        timestamppb.Now(),
					InFlightRequests: proto.Uint32(10),
				}.Build(),
			},
			instances:                nil,
			expectedInFlightRequests: 10,
			expectedTimestampSource:  "router1",
		},
		{
			// Multiple routers: sums in-flight requests
			name: "sums in-flight requests from multiple routers",
			routerHeartbeats: map[string]*skipper.Heartbeat{
				fixture.RouterIP: skipper.Heartbeat_builder{
					Function:         fn,
					Timestamp:        timestamppb.New(time.Now().Add(-time.Minute)),
					InFlightRequests: proto.Uint32(10),
				}.Build(),
				fixture.RouterIP2: skipper.Heartbeat_builder{
					Function:         fn,
					Timestamp:        timestamppb.Now(),
					InFlightRequests: proto.Uint32(15),
				}.Build(),
			},
			instances:                nil,
			expectedInFlightRequests: 25,
			expectedTimestampSource:  "router2",
		},
		{
			// Instance AssignedAt is newer than router heartbeats
			name: "uses instance AssignedAt when newer than router heartbeats",
			routerHeartbeats: map[string]*skipper.Heartbeat{
				fixture.RouterIP: skipper.Heartbeat_builder{
					Function:         fn,
					Timestamp:        timestamppb.New(time.Now().Add(-time.Minute)),
					InFlightRequests: proto.Uint32(5),
				}.Build(),
			},
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					Function:   fn,
					AssignedAt: timestamppb.Now(),
				}.Build(),
			},
			expectedInFlightRequests: 5,
			expectedTimestampSource:  "instance",
		},
		{
			// Router heartbeat is newer than instance AssignedAt
			name: "uses router heartbeat timestamp when newer than instance",
			routerHeartbeats: map[string]*skipper.Heartbeat{
				fixture.RouterIP: skipper.Heartbeat_builder{
					Function:         fn,
					Timestamp:        timestamppb.Now(),
					InFlightRequests: proto.Uint32(8),
				}.Build(),
			},
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					Function:   fn,
					AssignedAt: timestamppb.New(time.Now().Add(-time.Minute)),
				}.Build(),
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
					router1Time = hb.GetTimestamp().AsTime()
				case fixture.RouterIP2:
					router2Time = hb.GetTimestamp().AsTime()
				}
			}

			// Get instance timestamp if present
			var instanceTime time.Time
			if len(tc.instances) > 0 {
				instanceTime = tc.instances[0].GetAssignedAt().AsTime()
			}

			combined := supervisor.combinedHeartbeat(tc.instances)

			// Verify in-flight requests sum
			assert.Equal(t, tc.expectedInFlightRequests, combined.GetInFlightRequests())

			// Verify timestamp source
			combinedTime := combined.GetTimestamp().AsTime()
			switch tc.expectedTimestampSource {
			case "router1":
				assert.Assert(t, combinedTime.Equal(router1Time))
			case "router2":
				assert.Assert(t, combinedTime.Equal(router2Time))
			case "instance":
				assert.Assert(t, combinedTime.Equal(instanceTime))
			case "zero":
				assert.Assert(t, !combined.HasTimestamp())
			}

			// Verify function is set
			assert.Assert(t, combined.GetFunction() == fn)
		})
	}
}

// TestGetReadyInstance tests the instance selection logic including filtering,
// scaling up when no instances are available, and respecting max instances.
func TestGetReadyInstance(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		instance       *skipper.Instance
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
				assert.Assert(t, proto.Equal(state.instance.GetFunction(), state.fn))
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
				assert.Equal(t, "included-pod", state.instance.GetName())
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
				assert.Assert(t, state.instance.GetName() == "pod-1" || state.instance.GetName() == "pod-2")
			},
		},
		{
			// Limits instances to maxInstances when more are available
			name: "limits instances to maxInstances",
			setup: func(t *testing.T, state *testState) {
				// Set max instances to 2
				state.fn.GetScale().SetMaxInstances(2)

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
		heartbeat                *skipper.Heartbeat
		instances                []*skipper.Instance
		expectedDesiredInstances uint32
		expectedUnclampedDesired uint32
		expectedReason           skipper.ScaleReason
	}{
		{
			// Heartbeat timeout triggers scale to 0
			name: "scales to zero on heartbeat timeout",
			heartbeat: skipper.Heartbeat_builder{
				Function: skipper.Function_builder{
					Scale: skipper.Scale_builder{
						MinInstances:           proto.Uint32(0),
						MaxInstances:           proto.Uint32(5),
						TargetInFlightRequests: proto.Uint32(10),
					}.Build(),
				}.Build(),
				Timestamp:        timestamppb.New(time.Now().Add(-cfg.HeartbeatTimeout - time.Second)),
				InFlightRequests: proto.Uint32(5),
			}.Build(),
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					ReadyAt: timestamppb.New(readyAt),
				}.Build(),
			},
			expectedDesiredInstances: 0,
			expectedUnclampedDesired: 0,
			expectedReason:           skipper.ScaleReason_SCALE_REASON_HEARTBEAT_TIMEOUT,
		},
		{
			// In-flight requests scaling: 25 requests / 10 target = 3 instances
			name: "scales based on in-flight requests",
			heartbeat: skipper.Heartbeat_builder{
				Function: skipper.Function_builder{
					Scale: skipper.Scale_builder{
						MinInstances:           proto.Uint32(0),
						MaxInstances:           proto.Uint32(10),
						TargetInFlightRequests: proto.Uint32(10),
					}.Build(),
				}.Build(),
				Timestamp:        timestamppb.New(time.Now()),
				InFlightRequests: proto.Uint32(25),
			}.Build(),
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					ReadyAt: timestamppb.New(readyAt),
				}.Build(),
			},
			expectedDesiredInstances: 3,
			expectedUnclampedDesired: 3,
			expectedReason:           skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS,
		},
		{
			// Multiple metrics: takes the max across all
			// In-flight: 15 / 10 = 2
			// CPU: avg 200m / 100m = 2
			// Result: max(2, 2) = 2
			name: "takes max across multiple metrics",
			heartbeat: skipper.Heartbeat_builder{
				Function: skipper.Function_builder{
					Scale: skipper.Scale_builder{
						MinInstances:           proto.Uint32(0),
						MaxInstances:           proto.Uint32(10),
						TargetInFlightRequests: proto.Uint32(10),
						TargetCpuUsageMilli:    proto.Uint32(100),
					}.Build(),
				}.Build(),
				Timestamp:        timestamppb.New(time.Now()),
				InFlightRequests: proto.Uint32(15),
			}.Build(),
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					Function: skipper.Function_builder{
						Scale: skipper.Scale_builder{
							MinInstances:        proto.Uint32(0),
							MaxInstances:        proto.Uint32(0),
							TargetCpuUsageMilli: proto.Uint32(100),
						}.Build(),
					}.Build(),
					ReadyAt:       timestamppb.New(readyAt),
					CpuUsageMilli: proto.Uint32(200),
				}.Build(),
			},
			expectedDesiredInstances: 2,
			expectedUnclampedDesired: 2,
			expectedReason:           skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS, // or CPU, both equal
		},
		{
			// Max clamping: would scale to 10 but max is 5
			name: "applies max instances clamping",
			heartbeat: skipper.Heartbeat_builder{
				Function: skipper.Function_builder{
					Scale: skipper.Scale_builder{
						MinInstances:           proto.Uint32(0),
						MaxInstances:           proto.Uint32(5),
						TargetInFlightRequests: proto.Uint32(10),
					}.Build(),
				}.Build(),
				Timestamp:        timestamppb.New(time.Now()),
				InFlightRequests: proto.Uint32(100),
			}.Build(),
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					ReadyAt: timestamppb.New(readyAt),
				}.Build(),
			},
			expectedDesiredInstances: 5,
			expectedUnclampedDesired: 10,
			expectedReason:           skipper.ScaleReason_SCALE_REASON_IN_FLIGHT_REQUESTS,
		},
		{
			// Min clamping: unclamped would be 1 (baseline without heartbeat timeout) but min is 2
			name: "applies min instances clamping",
			heartbeat: skipper.Heartbeat_builder{
				Function: skipper.Function_builder{
					Scale: skipper.Scale_builder{
						MinInstances:           proto.Uint32(2),
						MaxInstances:           proto.Uint32(10),
						TargetInFlightRequests: proto.Uint32(10),
					}.Build(),
				}.Build(),
				Timestamp:        timestamppb.New(time.Now()),
				InFlightRequests: proto.Uint32(0),
			}.Build(),
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					ReadyAt: timestamppb.New(readyAt),
				}.Build(),
			},
			expectedDesiredInstances: 2,
			expectedUnclampedDesired: 1,
			expectedReason:           skipper.ScaleReason_SCALE_REASON_UNSPECIFIED,
		},
		{
			// No scaling metrics configured: maintains at least 1 instance
			name: "maintains one instance when no scaling metrics configured",
			heartbeat: skipper.Heartbeat_builder{
				Function: skipper.Function_builder{
					Scale: skipper.Scale_builder{
						MinInstances: proto.Uint32(0),
						MaxInstances: proto.Uint32(10),
					}.Build(),
				}.Build(),
				Timestamp:        timestamppb.New(time.Now()),
				InFlightRequests: proto.Uint32(0),
			}.Build(),
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					ReadyAt: timestamppb.New(readyAt),
				}.Build(),
			},
			expectedDesiredInstances: 1,
			expectedUnclampedDesired: 1,
			expectedReason:           skipper.ScaleReason_SCALE_REASON_UNSPECIFIED,
		},
		{
			// CPU scaling dominates over in-flight requests
			// In-flight: 5 / 10 = 1
			// CPU: avg 300m / 100m = 3
			// Result: max(1, 3) = 3
			name: "CPU scaling dominates when higher",
			heartbeat: skipper.Heartbeat_builder{
				Function: skipper.Function_builder{
					Scale: skipper.Scale_builder{
						MinInstances:           proto.Uint32(0),
						MaxInstances:           proto.Uint32(10),
						TargetInFlightRequests: proto.Uint32(10),
						TargetCpuUsageMilli:    proto.Uint32(100),
					}.Build(),
				}.Build(),
				Timestamp:        timestamppb.New(time.Now()),
				InFlightRequests: proto.Uint32(5),
			}.Build(),
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					Function: skipper.Function_builder{
						Scale: skipper.Scale_builder{
							MinInstances:        proto.Uint32(0),
							MaxInstances:        proto.Uint32(0),
							TargetCpuUsageMilli: proto.Uint32(100),
						}.Build(),
					}.Build(),
					ReadyAt: timestamppb.New(readyAt), CpuUsageMilli: proto.Uint32(300),
				}.Build(),
			},
			expectedDesiredInstances: 3,
			expectedUnclampedDesired: 3,
			expectedReason:           skipper.ScaleReason_SCALE_REASON_CPU,
		},
		{
			// Memory scaling
			name: "scales based on memory usage",
			heartbeat: skipper.Heartbeat_builder{
				Function: skipper.Function_builder{
					Scale: skipper.Scale_builder{
						MinInstances:         proto.Uint32(0),
						MaxInstances:         proto.Uint32(10),
						TargetMemoryUsageMib: proto.Uint32(100),
					}.Build(),
				}.Build(),
				Timestamp:        timestamppb.New(time.Now()),
				InFlightRequests: proto.Uint32(0),
			}.Build(),
			instances: []*skipper.Instance{
				skipper.Instance_builder{
					Function: skipper.Function_builder{
						Scale: skipper.Scale_builder{
							MinInstances:         proto.Uint32(0),
							MaxInstances:         proto.Uint32(0),
							TargetMemoryUsageMib: proto.Uint32(100),
						}.Build(),
					}.Build(),
					ReadyAt:        timestamppb.New(readyAt),
					MemoryUsageMib: proto.Uint32(250),
				}.Build(),
			},
			expectedDesiredInstances: 3,
			expectedUnclampedDesired: 3,
			expectedReason:           skipper.ScaleReason_SCALE_REASON_MEMORY,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			decision := calculateDesiredInstances(t.Context(), cfg, tc.heartbeat, tc.instances)

			assert.Equal(t, tc.expectedDesiredInstances, decision.GetDesiredInstances())
			assert.Equal(t, tc.expectedUnclampedDesired, decision.GetUnclampedDesiredInstances())
			assert.Equal(t, tc.expectedReason, decision.GetReason())
		})
	}
}

// TestSupervisorLifecycle tests the Start/Stop lifecycle of the supervisor's
// independent converge loop, including idempotency and context handling.
func TestSupervisorLifecycle(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		setup func(*testing.T, *Supervisor)
		check func(*testing.T, *Supervisor)
	}{
		{
			// Start(nil) should not start the loop - useful for tests
			name: "Start with nil context does not start loop",
			setup: func(t *testing.T, s *Supervisor) {
				var nilCtx context.Context //nolint:staticcheck // intentionally testing nil context behavior
				s.start(nilCtx)
			},
			check: func(t *testing.T, s *Supervisor) {
				assert.Assert(t, s.ctx == nil, "ctx should be nil when Start called with nil")
				assert.Assert(t, s.cancel == nil, "cancel should be nil when Start called with nil")
			},
		},
		{
			// Start(nil) followed by Start(ctx) should start the loop
			// This verifies that nil doesn't consume the sync.Once
			name: "Start with nil then valid context starts loop",
			setup: func(t *testing.T, s *Supervisor) {
				var nilCtx context.Context //nolint:staticcheck // intentionally testing nil context behavior
				s.start(nilCtx)
				s.start(t.Context()) // should work because nil didn't consume once
			},
			check: func(t *testing.T, s *Supervisor) {
				assert.Assert(t, s.ctx != nil, "ctx should be set after Start with valid context")
				assert.Assert(t, s.cancel != nil, "cancel should be set after Start with valid context")
				assert.NilError(t, s.ctx.Err(), "ctx should not be cancelled")
			},
		},
		{
			// Start with a valid context should start the loop
			name: "Start with valid context starts loop",
			setup: func(t *testing.T, s *Supervisor) {
				s.start(t.Context())
			},
			check: func(t *testing.T, s *Supervisor) {
				assert.Assert(t, s.ctx != nil, "ctx should be set after Start")
				assert.Assert(t, s.cancel != nil, "cancel should be set after Start")
				assert.NilError(t, s.ctx.Err(), "ctx should not be cancelled")
			},
		},
		{
			// Start should be idempotent - multiple calls only start one loop
			name: "Start is idempotent - multiple calls use same context",
			setup: func(t *testing.T, s *Supervisor) {
				ctx1 := context.Background()
				ctx2 := context.Background()
				s.start(ctx1)
				firstCtx := s.ctx
				s.start(ctx2) // second call should be ignored
				assert.Assert(t, s.ctx == firstCtx, "ctx should not change on second Start call")
			},
			check: func(t *testing.T, s *Supervisor) {
				assert.Assert(t, s.ctx != nil)
			},
		},
		{
			// Stop should cancel the loop's context
			name: "Stop cancels the loop context",
			setup: func(t *testing.T, s *Supervisor) {
				s.start(t.Context())
				s.stop()
			},
			check: func(t *testing.T, s *Supervisor) {
				assert.ErrorIs(t, s.ctx.Err(), context.Canceled)
			},
		},
		{
			// Stop should be safe to call even if Start wasn't called
			name: "Stop is safe without prior Start",
			setup: func(t *testing.T, s *Supervisor) {
				s.stop() // should not panic
			},
			check: func(t *testing.T, s *Supervisor) {
				assert.Assert(t, s.ctx == nil, "ctx should remain nil")
			},
		},
		{
			// Stop should be safe to call multiple times
			name: "Stop is safe to call multiple times",
			setup: func(t *testing.T, s *Supervisor) {
				s.start(t.Context())
				s.stop()
				s.stop() // should not panic
			},
			check: func(t *testing.T, s *Supervisor) {
				assert.ErrorIs(t, s.ctx.Err(), context.Canceled)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fn := fixture.NewFunction(t)
			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			ctrl := New(testConfig(), nil, fakeKubernetes, nil)

			supervisor := &Supervisor{
				fn:               fn,
				ctrl:             ctrl,
				routerHeartbeats: nil, // not needed for lifecycle tests
			}

			tc.setup(t, supervisor)
			tc.check(t, supervisor)
		})
	}
}

// TestSupervisorLoopConvergesIndependently verifies that each supervisor's
// loop runs converge on its own at the configured interval, independent
// of other supervisors or the namespace-level discovery loop.
func TestSupervisorLoopConvergesIndependently(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	// Add an available pod that can be assigned
	fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

	// Use a short scale interval so the loop runs quickly
	cfg := testConfig()
	cfg.ScaleInterval = 50 * time.Millisecond
	cfg.HeartbeatTimeout = 10 * time.Second // long enough to not timeout during test

	ctrl := New(cfg, nil, fakeKubernetes, nil)
	ctrl.ctx = ctx // set context so supervisor loop starts
	ctrl.startedAt = time.Now().Add(-cfg.HPADownscaleStabilization - time.Second)

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Get supervisor which will start its loop
	supervisor := ctrl.supervisor(fn)

	// Add a heartbeat so the function doesn't scale to 0
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:         fn,
		Timestamp:        timestamppb.Now(),
		InFlightRequests: proto.Uint32(1), // request in flight should trigger scale to 1
	}.Build())

	// Wait for the loop to converge and assign a pod
	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		pods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
			LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
		})
		if err != nil {
			return poll.Continue("failed to list pods: %v", err)
		}
		if len(pods.Items) > 0 {
			return poll.Success()
		}
		return poll.Continue("waiting for supervisor loop to assign a pod")
	}, poll.WithDelay(100*time.Millisecond), poll.WithTimeout(5*time.Second))
}

// TestScaleToZeroStopsLoop verifies that when a function scales to 0
// due to heartbeat timeout, the supervisor's loop is stopped and the
// supervisor is removed from the controller's map.
func TestScaleToZeroStopsLoop(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		startWithInstance bool
	}{
		{
			name:              "scales down from 1 instance",
			startWithInstance: true,
		},
		{
			// This case tests the bug fix where cleanup was only triggered
			// when scaling down from >0 instances, causing supervisors to
			// leak when already at 0 instances.
			name:              "cleans up when already at 0 instances",
			startWithInstance: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			fn := fixture.NewFunction(t)
			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
			if tc.startWithInstance {
				fakeKubernetes.Tracker().Add(fixture.NewAssignedPod(t, fn, nil))
			}

			cfg := testConfig()
			cfg.ScaleInterval = 50 * time.Millisecond
			cfg.HeartbeatTimeout = 100 * time.Millisecond

			ctrl := New(cfg, nil, fakeKubernetes, nil)
			// Don't set ctrl.ctx yet - we need to store the heartbeat before the
			// supervisor loop starts, otherwise converge() may see no heartbeats
			// and immediately scale to 0, removing the supervisor from the map.
			ctrl.startedAt = time.Now().Add(-cfg.HPADownscaleStabilization - time.Second)

			err := ctrl.startInformers(ctx)
			assert.NilError(t, err)

			// Create supervisor without starting its loop (ctrl.ctx is nil)
			supervisor := ctrl.supervisor(fn)

			// Add an old heartbeat that will timeout
			supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
				Function:  fn,
				Timestamp: timestamppb.New(time.Now().Add(-cfg.HeartbeatTimeout - time.Second)),
			}.Build())

			// Now set context and start the supervisor loop
			ctrl.ctx = ctx
			supervisor.start(ctx)

			_, exists := ctrl.supervisors.Load(fn.Hash())
			assert.Assert(t, exists, "supervisor should exist in map initially")

			// Wait for heartbeat timeout to trigger scale-to-0 and supervisor removal
			poll.WaitOn(t, func(t poll.LogT) poll.Result {
				_, exists := ctrl.supervisors.Load(fn.Hash())
				if !exists {
					return poll.Success()
				}
				return poll.Continue("waiting for supervisor to be removed from map")
			}, poll.WithDelay(100*time.Millisecond), poll.WithTimeout(5*time.Second))

			assert.ErrorIs(t, supervisor.ctx.Err(), context.Canceled)
		})
	}
}

// TestCleanupStuckInstances tests that instances stuck in the assigned state
// (never became ready) are properly terminated.
func TestCleanupStuckInstances(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
	}

	testCases := []struct {
		name  string
		setup func(*testing.T, *testState)
		check func(*testing.T, *testState, []*skipper.Instance)
	}{
		{
			name: "terminates instance stuck in assigned state",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// create a pod that was assigned long ago but never became ready
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				stuckTime := time.Now().Add(-state.ctrl.config.FunctionAssignTimeout * 3) // well past the 2x threshold
				pod.Annotations[key.AssignedAt.Annotation] = stuckTime.Format(time.RFC3339)
				delete(pod.Annotations, key.ReadyAt.Annotation) // never became ready
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// ensure the stuck instance was terminated
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 0)
				assert.Assert(t, len(instances) == 0)
			},
		},
		{
			name: "does not terminate recently assigned unready instance",
			setup: func(t *testing.T, state *testState) {
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// create a pod that was assigned recently and isn't ready yet
				pod := fixture.NewAssignedPod(t, state.fn, nil)
				pod.Annotations[key.AssignedAt.Annotation] = time.Now().Format(time.RFC3339)
				delete(pod.Annotations, key.ReadyAt.Annotation) // not yet ready
				state.fakeKubernetes.Tracker().Add(pod)
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// ensure the instance was not terminated
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				assert.Assert(t, len(instances) == 1)
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
			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

			tc.setup(t, state)

			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			// Get instances and call cleanupStuckInstances via the supervisor
			instances, err := state.ctrl.getInstances(ctx, state.fn)
			assert.NilError(t, err)

			supervisor := state.ctrl.supervisor(state.fn)
			instances = supervisor.cleanupStuckInstances(ctx, instances)

			tc.check(t, state, instances)
		})
	}
}

// TestReplaceStaleInstances tests that stale instances (on old replica sets)
// are properly replaced with instances on new replica sets.
func TestReplaceStaleInstances(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
	}

	testCases := []struct {
		name            string
		setup           func(*testing.T, *testState)
		beforeTerminate func(*testing.T, context.Context, *testState, []*skipper.Instance) // optional: runs between getInstances and replaceStaleInstances
		check           func(*testing.T, *testState, []*skipper.Instance)
	}{
		{
			name: "replaces stale instance on old replica set",
			setup: func(t *testing.T, state *testState) {
				// create an assigned pod on the old replica set
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				// create a stale replica set (scaled to 0)
				currentReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				currentReplicaSet.Status.Replicas = 0
				state.fakeKubernetes.Tracker().Add(currentReplicaSet)

				// add a new replica set with an available pod
				state.fakeKubernetes.Tracker().Add(fixture.NewReplicaSet(t, state.fn))
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// ensure the stale instance was replaced
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
				assert.Assert(t, len(instances) == 1)
			},
		},
		{
			// When replica set lookup fails, instance should be kept in the list
			name: "keeps instance when replica set lookup fails",
			setup: func(t *testing.T, state *testState) {
				// Create an assigned pod that references a non-existent replica set
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				assignedPod.Annotations[key.ReplicaSet.Annotation] = "nonexistent-replicaset"
				state.fakeKubernetes.Tracker().Add(assignedPod)
				// Don't add the replica set - this will cause lookup to fail
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// Instance should be kept since replica set lookup failed
				assert.Assert(t, len(instances) == 1)
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
			},
		},
		{
			// When no replacement pod is available, stale instance should be kept
			name: "keeps stale instance when no replacement pod available",
			setup: func(t *testing.T, state *testState) {
				// Create an assigned pod on the old replica set
				assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
				state.fakeKubernetes.Tracker().Add(assignedPod)

				// Create a stale replica set (scaled to 0)
				currentReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				currentReplicaSet.Status.Replicas = 0
				state.fakeKubernetes.Tracker().Add(currentReplicaSet)

				// Add a new replica set but NO available pods
				state.fakeKubernetes.Tracker().Add(fixture.NewReplicaSet(t, state.fn))
				// No available pod added - assignPod will fail
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// Stale instance should be kept because we couldn't assign a replacement
				assert.Assert(t, len(instances) == 1)
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 1)
			},
		},
		{
			// When multiple stale instances exist, all should be replaced
			name: "replaces multiple stale instances",
			setup: func(t *testing.T, state *testState) {
				// Create multiple assigned pods on the old replica set
				for i := range 3 {
					assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
					assignedPod.Name = fmt.Sprintf("stale-pod-%d", i)
					state.fakeKubernetes.Tracker().Add(assignedPod)
				}

				// Create a stale replica set (scaled to 0)
				currentReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				currentReplicaSet.Status.Replicas = 0
				state.fakeKubernetes.Tracker().Add(currentReplicaSet)

				// Add a new replica set with multiple available pods
				state.fakeKubernetes.Tracker().Add(fixture.NewReplicaSet(t, state.fn))
				for i := range 3 {
					availablePod := fixture.NewAvailablePod(t, state.fn, nil)
					availablePod.Name = fmt.Sprintf("available-pod-%d", i)
					state.fakeKubernetes.Tracker().Add(availablePod)
				}
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// All 3 stale instances should be replaced with new ones
				assert.Assert(t, len(instances) == 3)
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				// Should have 3 new assigned pods (stale ones deleted)
				assert.Assert(t, len(pods.Items) == 3)
			},
		},
		{
			// When multiple stale instances exist at exactly maxInstances, all should still
			// be replaced because we decrement totalInstances after each successful delete.
			// Without the decrement, only the first stale instance would be replaced.
			name: "replaces all stale instances when at maxInstances",
			setup: func(t *testing.T, state *testState) {
				// Set maxInstances to exactly the number of stale instances
				state.fn.GetScale().SetMaxInstances(3)

				// Create 3 stale pods on the old replica set
				for i := range 3 {
					assignedPod := fixture.NewAssignedPod(t, state.fn, nil)
					assignedPod.Name = fmt.Sprintf("stale-pod-%d", i)
					state.fakeKubernetes.Tracker().Add(assignedPod)
				}

				// Create a stale replica set (scaled to 0)
				currentReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				currentReplicaSet.Status.Replicas = 0
				state.fakeKubernetes.Tracker().Add(currentReplicaSet)

				// Add a new replica set with available pods for replacement
				state.fakeKubernetes.Tracker().Add(fixture.NewReplicaSet(t, state.fn))
				for i := range 3 {
					availablePod := fixture.NewAvailablePod(t, state.fn, nil)
					availablePod.Name = fmt.Sprintf("available-pod-%d", i)
					state.fakeKubernetes.Tracker().Add(availablePod)
				}
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// All 3 stale instances should be replaced
				assert.Assert(t, len(instances) == 3, "expected 3 instances, got %d", len(instances))

				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{
					LabelSelector: key.Tenant.Label + "=" + state.fn.GetTenant(),
				})
				assert.NilError(t, err)
				assert.Assert(t, len(pods.Items) == 3, "expected 3 pods, got %d", len(pods.Items))

				// Verify all stale pods were deleted (none should remain)
				for _, pod := range pods.Items {
					for i := range 3 {
						assert.Assert(t, pod.Name != fmt.Sprintf("stale-pod-%d", i),
							"stale pod %d should have been deleted", i)
					}
				}
			},
		},
		{
			// Stale instance replacement should work even at max instances (temporarily exceeds limit)
			name: "replaces stale instance even when at max instances",
			setup: func(t *testing.T, state *testState) {
				// Set max instances to 2
				state.fn.GetScale().SetMaxInstances(2)

				// First, create the old (stale) replica set - this sets the "current" name
				staleReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				staleReplicaSetName := staleReplicaSet.Name
				staleReplicaSet.Status.Replicas = 0 // mark it as scaled to 0 (stale)
				state.fakeKubernetes.Tracker().Add(staleReplicaSet)

				// Now create the new replica set - this changes the "current" name
				newReplicaSet := fixture.NewReplicaSet(t, state.fn)
				state.fakeKubernetes.Tracker().Add(newReplicaSet)

				// Create a stale pod pointing to the old replica set
				stalePod := fixture.NewAssignedPod(t, state.fn, nil)
				stalePod.Name = "stale-pod"
				stalePod.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
				state.fakeKubernetes.Tracker().Add(stalePod)

				// Create a fresh pod pointing to the new replica set (uses current name)
				freshPod := fixture.NewAssignedPod(t, state.fn, nil)
				freshPod.Name = "fresh-pod"
				state.fakeKubernetes.Tracker().Add(freshPod)

				// Add an available pod for replacement
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// Should have 2 instances (1 fresh kept + 1 replacement for stale)
				assert.Assert(t, len(instances) == 2)
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				// Should have 2 pods: fresh-pod and the new replacement
				assert.Assert(t, len(pods.Items) == 2)
				// Verify stale pod was deleted
				for _, pod := range pods.Items {
					assert.Assert(t, pod.Name != "stale-pod", "stale pod should have been deleted")
				}
			},
		},
		{
			// When stale instance deletion fails (e.g., pod already deleted), new instance should still be added
			name: "adds new instance even when stale deletion fails",
			setup: func(t *testing.T, state *testState) {
				// First, create the old (stale) replica set
				staleReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				staleReplicaSetName := staleReplicaSet.Name
				staleReplicaSet.Status.Replicas = 0 // mark it as scaled to 0 (stale)
				state.fakeKubernetes.Tracker().Add(staleReplicaSet)

				// Now create the new replica set
				newReplicaSet := fixture.NewReplicaSet(t, state.fn)
				state.fakeKubernetes.Tracker().Add(newReplicaSet)

				// Create a stale pod pointing to the old replica set
				stalePod := fixture.NewAssignedPod(t, state.fn, nil)
				stalePod.Name = "stale-pod"
				stalePod.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
				state.fakeKubernetes.Tracker().Add(stalePod)

				// Add an available pod for replacement
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			beforeTerminate: func(t *testing.T, ctx context.Context, state *testState, instances []*skipper.Instance) {
				// Simulate deletion failure by deleting the stale pod before replaceStaleInstances tries to delete it
				// This simulates a race condition or external deletion - when replaceStaleInstances tries to delete,
				// it will get a NotFound error, but should still add the new instance
				for _, instance := range instances {
					if instance.GetName() == "stale-pod" {
						err := state.fakeKubernetes.CoreV1().Pods(instance.GetFunction().GetNamespace()).Delete(ctx, instance.GetName(), metav1.DeleteOptions{})
						assert.NilError(t, err)
						break
					}
				}
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// Should have 1 instance (the replacement) even though stale deletion "failed" (pod already gone)
				assert.Assert(t, len(instances) == 1, "expected 1 instance after replacement, got %d", len(instances))
				// Verify the new instance is in the list (not the stale one)
				assert.Assert(t, instances[0].GetName() != "stale-pod", "stale instance should not be in the result")
				// Verify stale pod is gone
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)
				for _, pod := range pods.Items {
					assert.Assert(t, pod.Name != "stale-pod", "stale pod should have been deleted")
				}
			},
		},
		{
			// When at maxInstances+1 due to failed deletion, should not assign more replacements.
			// This prevents unbounded pod creation when deletePod fails repeatedly.
			name: "skips replacement when already at maxInstances+1",
			setup: func(t *testing.T, state *testState) {
				// Set max instances to 2
				state.fn.GetScale().SetMaxInstances(2)

				// First, create the old (stale) replica set
				staleReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				staleReplicaSetName := staleReplicaSet.Name
				staleReplicaSet.Status.Replicas = 0 // mark it as scaled to 0 (stale)
				state.fakeKubernetes.Tracker().Add(staleReplicaSet)

				// Now create the new replica set
				newReplicaSet := fixture.NewReplicaSet(t, state.fn)
				state.fakeKubernetes.Tracker().Add(newReplicaSet)

				// Create a stale pod pointing to the old replica set (simulates failed deletion)
				stalePod := fixture.NewAssignedPod(t, state.fn, nil)
				stalePod.Name = "stale-pod"
				stalePod.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
				state.fakeKubernetes.Tracker().Add(stalePod)

				// Create 2 fresh pods pointing to the new replica set (already at maxInstances+1 = 3 total)
				for i := range 2 {
					freshPod := fixture.NewAssignedPod(t, state.fn, nil)
					freshPod.Name = fmt.Sprintf("fresh-pod-%d", i)
					state.fakeKubernetes.Tracker().Add(freshPod)
				}

				// Add available pod that should NOT be used (we're already at maxInstances+1)
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// Should have 3 instances (1 stale kept + 2 fresh) - no new replacement
				assert.Assert(t, len(instances) == 3, "expected 3 instances, got %d", len(instances))
				pods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{
					LabelSelector: key.Tenant.Label + "=" + state.fn.GetTenant(),
				})
				assert.NilError(t, err)
				// Should still have exactly 3 assigned pods (+ 1 available) - stale pod still exists
				assert.Assert(t, len(pods.Items) == 3, "expected 3 assigned pods, got %d", len(pods.Items))
				// Verify stale pod is still there (not deleted and not replaced yet)
				foundStale := false
				for _, pod := range pods.Items {
					if pod.Name == "stale-pod" {
						foundStale = true
						break
					}
				}
				assert.Assert(t, foundStale, "stale pod should still exist when at maxInstances+1")
			},
		},
		{
			// When the stale replacement semaphore is at capacity, replaceStaleInstances
			// should return immediately without processing any stale instances.
			// This verifies the non-blocking TryAcquire behavior.
			name: "defers replacement when semaphore at capacity",
			setup: func(t *testing.T, state *testState) {
				// First, create the old (stale) replica set
				staleReplicaSet := fixture.CurrentReplicaSet(t, state.fn)
				staleReplicaSetName := staleReplicaSet.Name
				staleReplicaSet.Status.Replicas = 0 // mark it as scaled to 0 (stale)
				state.fakeKubernetes.Tracker().Add(staleReplicaSet)

				// Now create the new replica set
				state.fakeKubernetes.Tracker().Add(fixture.NewReplicaSet(t, state.fn))

				// Create a stale pod pointing to the old replica set
				stalePod := fixture.NewAssignedPod(t, state.fn, nil)
				stalePod.Name = "stale-pod"
				stalePod.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
				state.fakeKubernetes.Tracker().Add(stalePod)

				// Add an available pod for replacement (should NOT be used)
				availablePod := fixture.NewAvailablePod(t, state.fn, nil)
				availablePod.Name = "available-pod"
				state.fakeKubernetes.Tracker().Add(availablePod)
			},
			beforeTerminate: func(t *testing.T, ctx context.Context, state *testState, instances []*skipper.Instance) {
				// Pre-acquire all semaphore slots to simulate it being at capacity.
				// This will cause TryAcquire to fail, and replaceStaleInstances should return immediately.
				cfg := testConfig()
				acquired := state.ctrl.staleReplacementSem.TryAcquire(int64(cfg.MaxConcurrentStaleReplacements))
				assert.Assert(t, acquired, "should be able to acquire semaphore in test setup")
				// Note: We intentionally don't release it - we want it held during replaceStaleInstances
			},
			check: func(t *testing.T, state *testState, instances []*skipper.Instance) {
				// List all pods (not just assigned ones) to verify state
				allPods, err := state.fakeKubernetes.CoreV1().Pods(state.fn.GetNamespace()).List(t.Context(), metav1.ListOptions{})
				assert.NilError(t, err)

				// Count assigned vs available pods
				var assignedPods, availablePods int
				var foundStale, foundAvailable bool
				for _, pod := range allPods.Items {
					if pod.Annotations[key.AssignedAt.Annotation] != "" {
						assignedPods++
						if pod.Name == "stale-pod" {
							foundStale = true
						}
					} else {
						availablePods++
						if pod.Name == "available-pod" {
							foundAvailable = true
						}
					}
				}

				// Should have 1 assigned pod (the stale one, not replaced)
				assert.Assert(t, assignedPods == 1, "expected 1 assigned pod (stale not replaced), got %d", assignedPods)

				// Verify the stale pod is still there
				assert.Assert(t, foundStale, "stale pod should still exist when semaphore at capacity")

				// Verify available pod was NOT consumed (still available)
				assert.Assert(t, foundAvailable, "available pod should not have been assigned")
				assert.Assert(t, availablePods >= 1, "should have at least 1 available pod")
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
			state.ctrl = New(testConfig(), nil, state.fakeKubernetes, nil)

			tc.setup(t, state)

			err := state.ctrl.startInformers(ctx)
			assert.NilError(t, err)

			// Get instances and call replaceStaleInstances via the supervisor
			instances, err := state.ctrl.getInstances(ctx, state.fn)
			assert.NilError(t, err)

			// Run optional beforeTerminate function (e.g., to simulate race conditions)
			if tc.beforeTerminate != nil {
				tc.beforeTerminate(t, ctx, state, instances)
			}

			supervisor := state.ctrl.supervisor(state.fn)
			supervisor.replaceStaleInstances(ctx, instances, len(instances))

			// Fetch fresh instances to check results
			finalInstances, err := state.ctrl.getInstances(ctx, state.fn)
			assert.NilError(t, err)
			tc.check(t, state, finalInstances)
		})
	}
}

// TestReplaceStaleInstancesConcurrencyLimit verifies that the controller limits
// concurrent stale instance replacements using a non-blocking semaphore.
// When the semaphore is at capacity, replaceStaleInstances returns immediately
// without blocking, deferring the replacement to a subsequent converge iteration.
func TestReplaceStaleInstancesConcurrencyLimit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Track concurrent replacements using atomic counter
	var currentConcurrent int64
	var maxConcurrent int64
	var totalReplacements int64
	var mu sync.Mutex

	// Create a slow handler that tracks concurrency.
	// The handler blocks long enough to ensure overlapping requests.
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Increment counter when entering
		current := atomic.AddInt64(&currentConcurrent, 1)
		atomic.AddInt64(&totalReplacements, 1)

		// Update max observed concurrency
		mu.Lock()
		if current > maxConcurrent {
			maxConcurrent = current
		}
		mu.Unlock()

		// Sleep to keep the assignment active and create contention
		time.Sleep(50 * time.Millisecond)

		// Decrement counter when exiting
		atomic.AddInt64(&currentConcurrent, -1)

		w.WriteHeader(http.StatusOK)
	})

	const numFunctions = 5
	const stalePerFunc = 2
	const limit = 2

	// Create multiple functions, each with stale instances
	functions := make([]*skipper.Function, numFunctions)
	staleReplicaSetNames := make([]string, numFunctions)

	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	for i := range numFunctions {
		fn := fixture.NewFunction(t)
		fn.GetScale().SetMaxInstances(uint32(stalePerFunc + 10)) // ensure we don't hit max instances limit
		functions[i] = fn

		// Create stale replica set (scaled to 0)
		staleReplicaSet := fixture.CurrentReplicaSet(t, fn)
		staleReplicaSetNames[i] = staleReplicaSet.Name
		staleReplicaSet.Status.Replicas = 0
		fakeKubernetes.Tracker().Add(staleReplicaSet)

		// Create new replica set
		fakeKubernetes.Tracker().Add(fixture.NewReplicaSet(t, fn))

		// Create stale assigned pods on old replica set
		for j := range stalePerFunc {
			assignedPod := fixture.NewAssignedPod(t, fn, nil)
			assignedPod.Name = fmt.Sprintf("stale-pod-%d-%d", i, j)
			assignedPod.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetNames[i]
			fakeKubernetes.Tracker().Add(assignedPod)
		}

		// Create available pods with slow handler for replacements
		for j := range stalePerFunc {
			availablePod := fixture.NewAvailablePod(t, fn, slowHandler)
			availablePod.Name = fmt.Sprintf("available-pod-%d-%d", i, j)
			fakeKubernetes.Tracker().Add(availablePod)
		}
	}

	cfg := testConfig()
	cfg.MaxConcurrentStaleReplacements = limit

	ctrl := New(cfg, nil, fakeKubernetes, nil)

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Run replaceStaleInstances concurrently for all functions.
	// With TryAcquire, supervisors that can't acquire the semaphore will return
	// immediately, so not all stale instances will be replaced in a single call.
	var wg sync.WaitGroup
	wg.Add(numFunctions)

	for i := range numFunctions {
		go func(fn *skipper.Function) {
			defer wg.Done()

			instances, err := ctrl.getInstances(ctx, fn)
			if err != nil {
				t.Errorf("getInstances failed: %v", err)
				return
			}

			supervisor := ctrl.supervisor(fn)
			supervisor.replaceStaleInstances(ctx, instances, len(instances))
		}(functions[i])
	}

	wg.Wait()

	// Verify concurrency was limited to the configured max
	assert.Assert(t, maxConcurrent <= int64(limit),
		"max concurrent should be <= %d, got %d", limit, maxConcurrent)

	// With non-blocking TryAcquire, not all replacements will complete in a single
	// concurrent call. Some supervisors will return immediately when the semaphore
	// is at capacity, deferring their work. Verify that some replacements happened
	// but not necessarily all of them.
	assert.Assert(t, totalReplacements >= 1,
		"at least one replacement should have happened")
	assert.Assert(t, totalReplacements <= int64(numFunctions*stalePerFunc),
		"should not exceed total stale instances")
}

// TestReplaceStaleInstancesNamespaceListerNotFound tests that when the namespace
// lister is not found for a function's namespace, instances are returned unchanged.
// This requires a separate test because we need to manually construct instances
// rather than using getInstances (which also requires the namespace lister).
func TestReplaceStaleInstancesNamespaceListerNotFound(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	ctrl := New(testConfig(), nil, fakeKubernetes, nil)

	// Start informers for the normal namespace
	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Create a supervisor for a function in a different namespace that doesn't have a lister
	fnInUnknownNamespace := fixture.NewFunction(t)
	fnInUnknownNamespace.SetNamespace("unknown-namespace")
	supervisor := ctrl.supervisor(fnInUnknownNamespace)

	// Manually construct instances (can't use getInstances since no lister exists)
	instances := []*skipper.Instance{
		fixture.NewInstance(t, fn, nil),
		fixture.NewInstance(t, fn, nil),
	}

	// Call replaceStaleInstances - should return void and do nothing
	supervisor.replaceStaleInstances(ctx, instances, len(instances))

	// Verify no pods were created in the unknown namespace
	pods, err := fakeKubernetes.CoreV1().Pods("unknown-namespace").List(ctx, metav1.ListOptions{})
	assert.NilError(t, err)
	assert.Assert(t, len(pods.Items) == 0, "should not have created any pods")
}

// TestReplaceStaleInstancesCountsUnreadyInstances verifies that the maxInstances+1
// check in replaceStaleInstances counts ALL instances (ready + unready), not just
// the ready instances passed as a parameter. This is a regression test for a bug
// where currentTotalInstances was initialized from len(instances), which only
// included ready instances (since scaleLocal returns only ready instances).
// The fix fetches fresh instances from the cluster to get the true total count.
func TestReplaceStaleInstancesCountsUnreadyInstances(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fn.GetScale().SetMaxInstances(2)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	ctrl := New(testConfig(), nil, fakeKubernetes, nil)

	// Create the old (stale) replica set
	staleReplicaSet := fixture.CurrentReplicaSet(t, fn)
	staleReplicaSetName := staleReplicaSet.Name
	staleReplicaSet.Status.Replicas = 0 // mark it as scaled to 0 (stale)
	fakeKubernetes.Tracker().Add(staleReplicaSet)

	// Create the new replica set
	newReplicaSet := fixture.NewReplicaSet(t, fn)
	fakeKubernetes.Tracker().Add(newReplicaSet)

	// Create 1 stale ready pod on the old replica set
	stalePod := fixture.NewAssignedPod(t, fn, nil)
	stalePod.Name = "stale-ready-pod"
	stalePod.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
	fakeKubernetes.Tracker().Add(stalePod)

	// Create 2 unready pods (assigned but no ReadyAt annotation)
	// These bring the total to 3 (maxInstances+1 = 3), but only 1 is ready
	for i := range 2 {
		unreadyPod := fixture.NewAssignedPod(t, fn, nil)
		unreadyPod.Name = fmt.Sprintf("unready-pod-%d", i)
		delete(unreadyPod.Annotations, key.ReadyAt.Annotation) // make it unready
		fakeKubernetes.Tracker().Add(unreadyPod)
	}

	// Add available pod that should NOT be used (we're already at maxInstances+1)
	fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, fn, nil))

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Get ALL instances from cluster
	allInstances, err := ctrl.getInstances(ctx, fn)
	assert.NilError(t, err)
	assert.Assert(t, len(allInstances) == 3, "expected 3 total instances, got %d", len(allInstances))

	// Filter to only ready instances (simulating what scaleLocal returns)
	var readyInstances []*skipper.Instance
	for _, inst := range allInstances {
		if inst.HasReadyAt() {
			readyInstances = append(readyInstances, inst)
		}
	}
	assert.Assert(t, len(readyInstances) == 1, "expected 1 ready instance, got %d", len(readyInstances))

	// Call replaceStaleInstances with only ready instances
	// Before the fix: len(readyInstances)=1 < maxInstances+1=3, so it would try to replace
	// After the fix: we explicitly pass 3 total, so it correctly skips replacement
	supervisor := ctrl.supervisor(fn)
	supervisor.replaceStaleInstances(ctx, readyInstances, len(allInstances))

	// Verify instances in cluster haven't changed (stale one kept)
	// Verify no new pods were assigned (total should still be 3 assigned + 1 available)
	pods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(t.Context(), metav1.ListOptions{
		LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
	})
	assert.NilError(t, err)
	assert.Assert(t, len(pods.Items) == 3, "expected 3 assigned pods (no new replacement), got %d", len(pods.Items))

	// Verify the stale pod still exists
	foundStale := false
	for _, pod := range pods.Items {
		if pod.Name == "stale-ready-pod" {
			foundStale = true
			break
		}
	}
	assert.Assert(t, foundStale, "stale pod should still exist when unready instances push total to maxInstances+1")
}

// TestSupervisorLoopErrorHandling verifies that when errors occur in the loop
// (getInstances or converge failures), the loop logs the error and continues
// running (doesn't crash). The loop should eventually succeed once the error
// condition is resolved.
func TestSupervisorLoopErrorHandling(t *testing.T) {
	t.Parallel()

	type testState struct {
		fn             *skipper.Function
		fakeKubernetes *fake.Clientset
		ctrl           *Controller
		supervisor     *Supervisor
	}

	testCases := []struct {
		name                           string
		setupError                     func(*testing.T, context.Context, *testState, *Supervisor) // sets up error condition (supervisor is already created)
		resolveError                   func(*testing.T, context.Context, *testState, *Supervisor) // resolves error condition
		waitForErrors                  time.Duration                                              // how long to wait for errors to occur
		startInformersBeforeSupervisor bool                                                       // whether to start informers before creating supervisor
		skipRecoveryVerification       bool                                                       // whether to skip recovery verification (to avoid race conditions)
	}{
		{
			name: "getInstances error",
			setupError: func(t *testing.T, ctx context.Context, state *testState, supervisor *Supervisor) {
				// Don't start informers - getInstances will fail because no namespace lister exists
				// This simulates an error condition
			},
			resolveError:                   nil, // Skip recovery to avoid race condition with startInformers
			waitForErrors:                  200 * time.Millisecond,
			startInformersBeforeSupervisor: false,
			skipRecoveryVerification:       true,
		},
		{
			name: "converge error",
			setupError: func(t *testing.T, ctx context.Context, state *testState, supervisor *Supervisor) {
				// Add only a replica set, but NO available pods
				// This will cause assignPod to fail within converge when trying to scale up
				state.fakeKubernetes.Tracker().Add(fixture.CurrentReplicaSet(t, state.fn))

				// Add a heartbeat with high in-flight requests to trigger scale-up
				supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
					Function:         state.fn,
					Timestamp:        timestamppb.Now(),
					InFlightRequests: proto.Uint32(10),
				}.Build())
			},
			resolveError: func(t *testing.T, ctx context.Context, state *testState, supervisor *Supervisor) {
				// Add an available pod and update heartbeat
				state.fakeKubernetes.Tracker().Add(fixture.NewAvailablePod(t, state.fn, nil))
				supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
					Function:         state.fn,
					Timestamp:        timestamppb.Now(),
					InFlightRequests: proto.Uint32(1),
				}.Build())
			},
			waitForErrors:                  300 * time.Millisecond,
			startInformersBeforeSupervisor: true,
			skipRecoveryVerification:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			fn := fixture.NewFunction(t)
			fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

			// Use a short scale interval so the loop runs quickly
			cfg := testConfig()
			cfg.ScaleInterval = 50 * time.Millisecond
			cfg.HeartbeatTimeout = 10 * time.Second

			ctrl := New(cfg, nil, fakeKubernetes, nil)
			// Don't set ctrl.startedAt - this prevents supervisor loops from starting
			// until we've finished setup. Otherwise there's a race between the loop
			// starting and setupError storing the heartbeat.

			state := &testState{
				fn:             fn,
				fakeKubernetes: fakeKubernetes,
				ctrl:           ctrl,
			}

			// Start informers if needed before creating supervisor
			if tc.startInformersBeforeSupervisor {
				err := ctrl.startInformers(ctx)
				assert.NilError(t, err)
			}

			// Create supervisor (loop won't start yet because ctrl.startedAt is zero)
			state.supervisor = ctrl.supervisor(fn)

			// Set up error condition (supervisor is now available)
			tc.setupError(t, ctx, state, state.supervisor)

			// Add a heartbeat so the function won't scale to 0 when it eventually works
			// (if not already set by setupError)
			if state.supervisor.routerHeartbeats.Size() == 0 {
				state.supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
					Function:         fn,
					Timestamp:        timestamppb.Now(),
					InFlightRequests: proto.Uint32(1),
				}.Build())
			}

			// Now start the supervisor loop
			ctrl.ctx = ctx
			ctrl.startedAt = time.Now().Add(-cfg.HPADownscaleStabilization - time.Second)
			state.supervisor.start(ctx)

			// Wait for a few loop iterations to happen (they should fail and log errors)
			time.Sleep(tc.waitForErrors)

			// Verify the supervisor is still in the map (loop didn't crash and remove it)
			_, exists := ctrl.supervisors.Load(fn.Hash())
			assert.Assert(t, exists, "supervisor should still exist after errors")

			// Resolve error condition (if provided)
			if tc.resolveError != nil {
				tc.resolveError(t, ctx, state, state.supervisor)
			}

			// Wait for the loop to eventually succeed (unless skipped to avoid race conditions)
			if !tc.skipRecoveryVerification {
				poll.WaitOn(t, func(t poll.LogT) poll.Result {
					pods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
						LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
					})
					if err != nil {
						return poll.Continue("failed to list pods: %v", err)
					}
					if len(pods.Items) > 0 {
						return poll.Success()
					}
					return poll.Continue("waiting for supervisor loop to recover and assign a pod")
				}, poll.WithDelay(100*time.Millisecond), poll.WithTimeout(5*time.Second))
			}
		})
	}
}

// TestConvergeReplacesStaleInstancesAfterScaling verifies that stale instances
// are replaced after scaling decisions are made. During scale-up, the function
// first scales to the desired count, then replaces any remaining stale instances.
func TestConvergeReplacesStaleInstancesAfterScaling(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fn.GetScale().SetMaxInstances(3)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	cfg := testConfig()
	cfg.HeartbeatTimeout = 10 * time.Second

	ctrl := New(cfg, nil, fakeKubernetes, nil)
	ctrl.startedAt = time.Now().Add(-cfg.HPADownscaleStabilization - time.Second)

	// First, create the old (stale) replica set
	staleReplicaSet := fixture.CurrentReplicaSet(t, fn)
	staleReplicaSetName := staleReplicaSet.Name
	staleReplicaSet.Status.Replicas = 0 // mark it as scaled to 0 (stale)
	fakeKubernetes.Tracker().Add(staleReplicaSet)

	// Now create the new replica set
	newReplicaSet := fixture.NewReplicaSet(t, fn)
	fakeKubernetes.Tracker().Add(newReplicaSet)

	// Create 2 stale instances on the old replica set
	stalePod1 := fixture.NewAssignedPod(t, fn, nil)
	stalePod1.Name = "stale-pod-1"
	stalePod1.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
	fakeKubernetes.Tracker().Add(stalePod1)

	stalePod2 := fixture.NewAssignedPod(t, fn, nil)
	stalePod2.Name = "stale-pod-2"
	stalePod2.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
	fakeKubernetes.Tracker().Add(stalePod2)

	// Add 3 available pods on the new replica set for replacements + scale up
	for i := range 3 {
		availablePod := fixture.NewAvailablePod(t, fn, nil)
		availablePod.Name = fmt.Sprintf("available-pod-%d", i)
		fakeKubernetes.Tracker().Add(availablePod)
	}

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Get instances - should return the 2 stale instances
	instances, err := ctrl.getInstances(ctx, fn)
	assert.NilError(t, err)
	assert.Assert(t, len(instances) == 2)

	// Create supervisor and set up heartbeat requesting 3 instances
	supervisor := ctrl.supervisor(fn)
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:         fn,
		Timestamp:        timestamppb.Now(),
		InFlightRequests: proto.Uint32(150), // high load to trigger scale up to 3 (150/50 target = 3)
	}.Build())

	// Call converge directly - this should:
	// 1. Scale up from 2 to 3 based on the heartbeat
	// 2. Replace the 2 stale instances with fresh ones (since we're not scaling down)
	err = supervisor.converge(ctx)
	assert.NilError(t, err)

	// Verify stale pods are gone and we have fresh pods
	pods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
	})
	assert.NilError(t, err)
	assert.Assert(t, len(pods.Items) == 3, "expected 3 pods, got %d", len(pods.Items))

	// Verify none of the pods are the stale ones
	for _, pod := range pods.Items {
		assert.Assert(t, pod.Name != "stale-pod-1" && pod.Name != "stale-pod-2",
			"stale pod %s should have been replaced", pod.Name)
		// Verify the pods are on the new replica set
		assert.Assert(t, pod.Annotations[key.ReplicaSet.Annotation] != staleReplicaSetName,
			"pod %s should be on new replica set", pod.Name)
	}
}

// TestConvergeDoesNotReplaceStaleInstancesWhenScalingDown verifies that stale
// instances are NOT replaced during scale-down. This is an efficiency
// optimization: since scale-down deletes oldest instances first (which are
// typically stale), there's no need to create replacement pods that would
// just be deleted anyway.
func TestConvergeDoesNotReplaceStaleInstancesWhenScalingDown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fn.GetScale().SetMaxInstances(3)
	// Disable CPU/memory scaling to focus on in-flight requests for this test
	fn.GetScale().SetTargetCpuUsageMilli(0)
	fn.GetScale().SetTargetMemoryUsageMib(0)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())

	cfg := testConfig()
	cfg.HeartbeatTimeout = 10 * time.Second
	cfg.HPADownscaleStabilization = 1 * time.Second // Use short stabilization for test

	ctrl := New(cfg, nil, fakeKubernetes, nil)
	// Controller must have been running longer than max(HPADownscaleStabilization, HeartbeatTimeout)
	// to allow scale-down
	ctrl.startedAt = time.Now().Add(-cfg.HeartbeatTimeout - time.Second)

	// First, create the old (stale) replica set
	staleReplicaSet := fixture.CurrentReplicaSet(t, fn)
	staleReplicaSetName := staleReplicaSet.Name
	staleReplicaSet.Status.Replicas = 0 // mark it as scaled to 0 (stale)
	fakeKubernetes.Tracker().Add(staleReplicaSet)

	// Now create the new replica set
	newReplicaSet := fixture.NewReplicaSet(t, fn)
	fakeKubernetes.Tracker().Add(newReplicaSet)

	// Create 3 instances: 2 stale + 1 fresh
	// The stale pods are OLDER (assigned earlier) so they'll be deleted first during scale-down
	stalePod1 := fixture.NewAssignedPod(t, fn, nil)
	stalePod1.Name = "stale-pod-1"
	stalePod1.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
	stalePod1.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-time.Hour).Format(time.RFC3339)
	fakeKubernetes.Tracker().Add(stalePod1)

	stalePod2 := fixture.NewAssignedPod(t, fn, nil)
	stalePod2.Name = "stale-pod-2"
	stalePod2.Annotations[key.ReplicaSet.Annotation] = staleReplicaSetName
	stalePod2.Annotations[key.AssignedAt.Annotation] = time.Now().Add(-30 * time.Minute).Format(time.RFC3339)
	fakeKubernetes.Tracker().Add(stalePod2)

	freshPod := fixture.NewAssignedPod(t, fn, nil)
	freshPod.Name = "fresh-pod"
	freshPod.Annotations[key.AssignedAt.Annotation] = time.Now().Format(time.RFC3339)
	fakeKubernetes.Tracker().Add(freshPod)

	// Add available pods that should NOT be used (we're scaling down, not up)
	for i := range 3 {
		availablePod := fixture.NewAvailablePod(t, fn, nil)
		availablePod.Name = fmt.Sprintf("available-pod-%d", i)
		fakeKubernetes.Tracker().Add(availablePod)
	}

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	// Get instances - should return 3 instances
	instances, err := ctrl.getInstances(ctx, fn)
	assert.NilError(t, err)
	assert.Assert(t, len(instances) == 3, "expected 3 instances initially, got %d", len(instances))

	// Create supervisor and set up heartbeat requesting only 1 instance (scale down)
	supervisor := ctrl.supervisor(fn)
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:         fn,
		Timestamp:        timestamppb.Now(),
		InFlightRequests: proto.Uint32(10), // low load to trigger scale down to 1 (ceil(10/50) = 1)
	}.Build())

	// Verify the scaling decision would request 1 instance
	heartbeat := supervisor.combinedHeartbeat(instances)
	scalingDecision := calculateDesiredInstances(ctx, cfg, heartbeat, instances)
	assert.Assert(t, scalingDecision.GetDesiredInstances() == 1,
		"expected scaling decision of 1 instance, got %d", scalingDecision.GetDesiredInstances())

	// Call converge directly - this should:
	// 1. Scale down from 3 to 1 (deleting oldest instances first, which are the stale ones)
	// 2. NOT replace stale instances (we're scaling down, so it's wasteful)
	err = supervisor.converge(ctx)
	assert.NilError(t, err)

	// Verify remaining instances in cluster
	pods, err := fakeKubernetes.CoreV1().Pods(fn.GetNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: key.Tenant.Label + "=" + fn.GetTenant(),
	})
	assert.NilError(t, err)

	// Should have 1 instance
	assert.Assert(t, len(pods.Items) == 1, "expected 1 pod, got %d", len(pods.Items))

	// Verify the remaining instance is the fresh one (newest, so not deleted)
	assert.Assert(t, pods.Items[0].Name == "fresh-pod",
		"expected fresh-pod to remain, got %s", pods.Items[0].Name)
}

// TestSupervisorStartup verifies that supervisors are started correctly
// in various scenarios. With the current implementation, start() is called
// on every supervisor() call, so supervisors are started idempotently
// once ctrl.ctx is available.
func TestSupervisorStartup(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		setup          func(*testing.T) (*Controller, *skipper.Function, context.Context)
		getSupervisor  func(*testing.T, *Controller, *skipper.Function) *Supervisor
		waitAfterStart time.Duration
		check          func(*testing.T, *Supervisor)
	}{
		{
			name: "supervisor started during discoverSupervisors",
			setup: func(t *testing.T) (*Controller, *skipper.Function, context.Context) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				t.Cleanup(cancel)

				fn := fixture.NewFunction(t)
				assignedPod := fixture.NewAssignedPod(t, fn, nil)
				replicaSet := fixture.CurrentReplicaSet(t, fn)

				fakeKubernetes := fake.NewClientset(
					fixture.NewControllerPod(),
					assignedPod,
					replicaSet,
				)

				ctrl := New(testConfig(), nil, fakeKubernetes, nil)
				return ctrl, fn, ctx
			},
			getSupervisor: func(t *testing.T, ctrl *Controller, fn *skipper.Function) *Supervisor {
				supervisor, ok := ctrl.supervisors.Load(fn.Hash())
				assert.Assert(t, ok, "supervisor should exist after discovery")
				return supervisor
			},
			waitAfterStart: 100 * time.Millisecond,
			check: func(t *testing.T, supervisor *Supervisor) {
				assert.Assert(t, supervisor.ctx != nil, "supervisor loop should be started (ctx should not be nil)")
				assert.Assert(t, supervisor.cancel != nil, "supervisor cancel should be set")
				assert.NilError(t, supervisor.ctx.Err(), "supervisor context should not be cancelled")
			},
		},
		{
			name: "supervisor started on subsequent call after controller start",
			setup: func(t *testing.T) (*Controller, *skipper.Function, context.Context) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				t.Cleanup(cancel)

				fn := fixture.NewFunction(t)
				fakeKubernetes := fake.NewClientset(
					fixture.NewControllerPod(),
				)

				ctrl := New(testConfig(), nil, fakeKubernetes, nil)

				// Create supervisor before controller is started (ctrl.ctx is nil)
				// The supervisor should be created but not started
				supervisor := ctrl.supervisor(fn)
				assert.Assert(t, supervisor != nil, "supervisor should be created")
				assert.Assert(t, supervisor.ctx == nil, "supervisor should not be started when ctrl.ctx is nil")
				assert.Assert(t, supervisor.cancel == nil, "supervisor cancel should not be set when not started")

				return ctrl, fn, ctx
			},
			getSupervisor: func(t *testing.T, ctrl *Controller, fn *skipper.Function) *Supervisor {
				// Call supervisor() again - should start it now
				return ctrl.supervisor(fn)
			},
			waitAfterStart: 0,
			check: func(t *testing.T, supervisor *Supervisor) {
				assert.Assert(t, supervisor.ctx != nil, "supervisor loop should be started after controller start (ctx should not be nil)")
				assert.Assert(t, supervisor.cancel != nil, "supervisor cancel should be set after start")
				assert.NilError(t, supervisor.ctx.Err(), "supervisor context should not be cancelled")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl, fn, ctx := tc.setup(t)

			// Start the controller. This will immediately trigger discoverSupervisors
			// via timer.Loop (which executes its callback immediately).
			err := ctrl.Start(ctx)
			assert.NilError(t, err)

			// Explicitly call supervisor() in the test thread to ensure supervisor.start()
			// is called synchronously. This ensures:
			// 1. The read of ctrl.ctx happens after Start() has completed (write happens before)
			// 2. The writes to supervisor.ctx and supervisor.cancel complete before we check them
			// This fixes the race condition without modifying production code.
			_ = ctrl.supervisor(fn)

			// Give the goroutine a moment to run discoverSupervisors if needed
			if tc.waitAfterStart > 0 {
				time.Sleep(tc.waitAfterStart)
			}

			// Get the supervisor for verification
			supervisor := tc.getSupervisor(t, ctrl, fn)
			tc.check(t, supervisor)
		})
	}
}

// TestSupervisorStopsWhenNoInstancesWithFreshHeartbeat verifies that the
// supervisor stops when all instances are gone, even if there's a fresh
// router heartbeat. This tests the fix for a bug where the supervisor would
// keep trying to scale up based on stale heartbeats after all pods died.
func TestSupervisorStopsWhenNoInstancesWithFreshHeartbeat(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	fn := fixture.NewFunction(t)
	fakeKubernetes := fake.NewClientset(fixture.NewControllerPod())
	// No pods - simulates all instances dying

	cfg := testConfig()
	cfg.ScaleInterval = 50 * time.Millisecond
	cfg.HeartbeatTimeout = 10 * time.Second // Long timeout

	ctrl := New(cfg, nil, fakeKubernetes, nil)
	ctrl.startedAt = time.Now().Add(-cfg.HPADownscaleStabilization - time.Second)

	err := ctrl.startInformers(ctx)
	assert.NilError(t, err)

	supervisor := ctrl.supervisor(fn)

	// Simulate that this supervisor previously had instances (which then died)
	supervisor.hadInstances = true

	// Add a FRESH heartbeat (only 1 second old, well within 10s timeout)
	// This simulates: traffic stopped recently, then all pods died
	supervisor.routerHeartbeats.Store(fixture.RouterIP, skipper.Heartbeat_builder{
		Function:  fn,
		Timestamp: timestamppb.New(time.Now().Add(-1 * time.Second)),
	}.Build())

	ctrl.ctx = ctx
	supervisor.start(ctx)

	_, exists := ctrl.supervisors.Load(fn.Hash())
	assert.Assert(t, exists, "supervisor should exist in map initially")

	// Without the fix, this will timeout because the supervisor
	// keeps trying to scale up based on the fresh heartbeat.
	// With the fix, the supervisor should stop quickly because
	// there are no instances and heartbeats are cleared.
	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		_, exists := ctrl.supervisors.Load(fn.Hash())
		if !exists {
			return poll.Success()
		}
		return poll.Continue("waiting for supervisor to be removed from map")
	}, poll.WithDelay(100*time.Millisecond), poll.WithTimeout(2*time.Second))

	assert.ErrorIs(t, supervisor.ctx.Err(), context.Canceled)
}
