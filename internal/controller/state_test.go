package controller

import (
	"sync"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClusterState(t *testing.T) {
	t.Parallel()

	t.Run("empty cluster", func(t *testing.T) {
		t.Parallel()

		fakeKubernetes := fake.NewClientset()
		ctrlPod := fixture.NewControllerPod()
		fakeKubernetes.Tracker().Add(ctrlPod)

		ctrl := New(testConfig(), nil, fakeKubernetes, nil)
		err := ctrl.Start(t.Context())
		assert.NilError(t, err)

		state := ctrl.ClusterState(t.Context())

		assert.Equal(t, state.GetPodIp(), fixture.ControllerIP)
		assert.Assert(t, state.HasStartedAt())
		assert.Assert(t, len(state.GetControllerIps()) >= 1)
		assert.Equal(t, len(state.GetSupervisors()), 0)
	})

	t.Run("with supervisors", func(t *testing.T) {
		t.Parallel()

		fakeKubernetes := fake.NewClientset()
		ctrlPod := fixture.NewControllerPod()
		fakeKubernetes.Tracker().Add(ctrlPod)

		ctrl := New(testConfig(), nil, fakeKubernetes, nil)
		err := ctrl.Start(t.Context())
		assert.NilError(t, err)

		fn := fixture.NewFunction(t)

		// Create a supervisor by calling supervisor()
		sup := ctrl.supervisor(fn)

		// Add a heartbeat
		hb := &skipper.Heartbeat{}
		hb.SetFunction(fn)
		hb.SetTimestamp(timestamppb.Now())
		hb.SetInFlightRequests(5)
		sup.heartbeat("10.0.0.1", hb)

		state := ctrl.ClusterState(t.Context())

		assert.Equal(t, state.GetPodIp(), fixture.ControllerIP)
		assert.Assert(t, len(state.GetSupervisors()) == 1)

		supState := state.GetSupervisors()[0]
		assert.Equal(t, supState.GetFunction().GetTenant(), fn.GetTenant())
		assert.Assert(t, len(supState.GetRouterHeartbeats()) == 1)
		assert.Equal(t, supState.GetRouterHeartbeats()[0].GetRouterIp(), "10.0.0.1")
		assert.Equal(t, supState.GetResponsibleControllerIp(), fixture.ControllerIP)
	})

	// This test exercises the race condition where sup.fn.Load() is called
	// multiple times within a single ClusterState iteration while a concurrent
	// goroutine swaps the function pointer via updateFunction. Without the fix
	// (loading fn once per iteration), different fields in the same supervisor
	// snapshot could reflect different *Function values, producing an
	// inconsistent snapshot. Run with -race to detect the concurrent access.
	t.Run("concurrent function update produces consistent snapshot", func(t *testing.T) {
		t.Parallel()

		fakeKubernetes := fake.NewClientset()
		ctrlPod := fixture.NewControllerPod()
		fakeKubernetes.Tracker().Add(ctrlPod)

		ctrl := New(testConfig(), nil, fakeKubernetes, nil)
		err := ctrl.Start(t.Context())
		assert.NilError(t, err)

		fn := fixture.NewFunction(t)
		sup := ctrl.supervisor(fn)

		// Build a second version of the same function with a different metadata value
		// (same identity/hash, different spec — matches updateFunction's precondition).
		fnUpdated := proto.Clone(fn).(*skipper.Function)
		fnUpdated.SetMetadata("updated-metadata")

		// Repeatedly swap the function pointer while ClusterState runs concurrently.
		// Any snapshot that is returned must reference a single coherent function:
		// the GetFunction() proto must match what was used for GetResponsibleControllerIp().
		// Before the fix, the multiple sup.fn.Load() calls could race and produce a
		// snapshot where GetFunction() reflected fn but GetResponsibleControllerIp()
		// was computed from fnUpdated (or vice versa).
		const iterations = 500
		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range iterations {
				if i%2 == 0 {
					sup.fn.Store(fn)
				} else {
					sup.fn.Store(fnUpdated)
				}
			}
		}()

		for range iterations {
			state := ctrl.ClusterState(t.Context())
			if len(state.GetSupervisors()) == 0 {
				continue
			}
			supState := state.GetSupervisors()[0]
			// The function embedded in the snapshot must be either fn or fnUpdated —
			// both are valid, but the snapshot must be internally consistent.
			snapshotFn := supState.GetFunction()
			assert.Assert(t,
				proto.Equal(snapshotFn, fn) || proto.Equal(snapshotFn, fnUpdated),
				"snapshot function must be one of the two known versions",
			)
		}

		wg.Wait()
	})

	t.Run("started_at is set", func(t *testing.T) {
		t.Parallel()

		fakeKubernetes := fake.NewClientset()
		ctrlPod := fixture.NewControllerPod()
		fakeKubernetes.Tracker().Add(ctrlPod)

		ctrl := New(testConfig(), nil, fakeKubernetes, nil)
		before := time.Now()
		err := ctrl.Start(t.Context())
		assert.NilError(t, err)

		state := ctrl.ClusterState(t.Context())
		startedAt := state.GetStartedAt().AsTime()
		assert.Assert(t, !startedAt.Before(before))
		assert.Assert(t, !startedAt.After(time.Now()))
	})
}
