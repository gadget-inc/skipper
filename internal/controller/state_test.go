package controller

import (
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/skipper"
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
