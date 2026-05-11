package web

import (
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

func makeState(supervisors ...*skipper.SupervisorState) *skipper.ClusterState {
	state := &skipper.ClusterState{}
	state.SetSupervisors(supervisors)
	return state
}

func makeSupervisor(instances []*skipper.Instance, heartbeats []*skipper.HeartbeatState, activeRS string) *skipper.SupervisorState {
	sup := &skipper.SupervisorState{}
	a := skipper.Assignment_builder{
		Namespace:  new("default"),
		Deployment: new("web-app"),
		Tenant:     new("tenant-1"),
	}.Build()
	sup.SetAssignment(a)
	sup.SetInstances(instances)
	sup.SetRouterHeartbeats(heartbeats)
	if activeRS != "" {
		sup.SetActiveReplicaSet(activeRS)
	}
	return sup
}

func makeInstance(name string, assignedAgo, readyAgo time.Duration, replicaSet string) *skipper.Instance {
	inst := skipper.Instance_builder{
		Name:       new(name),
		ReplicaSet: new(replicaSet),
		AssignedAt: timestamppb.New(time.Now().Add(-assignedAgo)),
	}.Build()
	if readyAgo >= 0 {
		inst.SetReadyAt(timestamppb.New(time.Now().Add(-readyAgo)))
	}
	return inst
}

func TestComputeHealthIssues(t *testing.T) {
	t.Parallel()

	t.Run("healthy cluster", func(t *testing.T) {
		t.Parallel()
		inst := makeInstance("pod-1", 30*time.Second, 10*time.Second, "rs-1")
		sup := makeSupervisor([]*skipper.Instance{inst}, nil, "rs-1")
		issues := computeHealthIssues(makeState(sup))
		assert.Equal(t, len(issues), 0)
	})

	t.Run("stuck instances", func(t *testing.T) {
		t.Parallel()
		inst := makeInstance("pod-1", 2*time.Minute, -1, "rs-1")
		inst.ClearReadyAt()
		sup := makeSupervisor([]*skipper.Instance{inst}, nil, "")
		issues := computeHealthIssues(makeState(sup))
		assert.Assert(t, len(issues) >= 1)
		assert.Equal(t, issues[0].Label, "Stuck instances")
		assert.Equal(t, issues[0].Color, "red")
	})

	t.Run("assignments waiting for pods", func(t *testing.T) {
		t.Parallel()
		inst := makeInstance("pod-1", 10*time.Second, -1, "rs-1")
		inst.ClearReadyAt()
		sup := makeSupervisor([]*skipper.Instance{inst}, nil, "")
		issues := computeHealthIssues(makeState(sup))
		// Should have "Assignments waiting for pods" but not "Stuck" (< 60s)
		found := false
		for _, issue := range issues {
			if issue.Label == "Assignments waiting for pods" {
				found = true
				assert.Equal(t, issue.Color, "yellow")
			}
		}
		assert.Assert(t, found)
	})

	t.Run("stale instances", func(t *testing.T) {
		t.Parallel()
		inst := makeInstance("pod-1", 30*time.Second, 10*time.Second, "rs-old")
		sup := makeSupervisor([]*skipper.Instance{inst}, nil, "rs-new")
		issues := computeHealthIssues(makeState(sup))
		found := false
		for _, issue := range issues {
			if issue.Label == "Stale instances" {
				found = true
				assert.Equal(t, issue.Color, "yellow")
			}
		}
		assert.Assert(t, found)
	})

	t.Run("empty cluster is healthy", func(t *testing.T) {
		t.Parallel()
		issues := computeHealthIssues(makeState())
		assert.Equal(t, len(issues), 0)
	})
}
