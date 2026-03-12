package web

import (
	"time"

	"github.com/gadget-inc/skipper/internal/skipper"
)

type healthIssue struct {
	Color string
	Label string
	Count int
	Link  string
}

func computeHealthIssues(state *skipper.ClusterState) []healthIssue {
	var issues []healthIssue
	now := time.Now()

	// Stuck instances: assigned but not ready > 60s
	var stuckCount int
	for _, sup := range state.GetSupervisors() {
		for _, inst := range sup.GetInstances() {
			if !inst.HasReadyAt() && inst.HasAssignedAt() {
				if now.Sub(inst.GetAssignedAt().AsTime()) > 60*time.Second {
					stuckCount++
				}
			}
		}
	}
	if stuckCount > 0 {
		issues = append(issues, healthIssue{Color: "red", Label: "Stuck instances", Count: stuckCount, Link: "/functions"})
	}

	// Functions waiting for pods: has instances but none ready
	var waitingCount int
	for _, sup := range state.GetSupervisors() {
		instances := sup.GetInstances()
		if len(instances) > 0 {
			allPending := true
			for _, inst := range instances {
				if inst.HasReadyAt() {
					allPending = false
					break
				}
			}
			if allPending {
				waitingCount++
			}
		}
	}
	if waitingCount > 0 {
		issues = append(issues, healthIssue{Color: "yellow", Label: "Functions waiting for pods", Count: waitingCount, Link: "/functions"})
	}

	// Stale heartbeats: > 60s old
	var staleHeartbeatCount int
	for _, sup := range state.GetSupervisors() {
		for _, hb := range sup.GetRouterHeartbeats() {
			if hb.GetHeartbeat() != nil && hb.GetHeartbeat().HasTimestamp() {
				if now.Sub(hb.GetHeartbeat().GetTimestamp().AsTime()) > 60*time.Second {
					staleHeartbeatCount++
				}
			}
		}
	}
	if staleHeartbeatCount > 0 {
		issues = append(issues, healthIssue{Color: "yellow", Label: "Stale heartbeats", Count: staleHeartbeatCount, Link: "/routers"})
	}

	// Stale instances: on non-active replica sets
	var staleInstanceCount int
	for _, sup := range state.GetSupervisors() {
		activeRS := sup.GetActiveReplicaSet()
		if activeRS != "" {
			for _, inst := range sup.GetInstances() {
				if inst.GetReplicaSet() != activeRS {
					staleInstanceCount++
				}
			}
		}
	}
	if staleInstanceCount > 0 {
		issues = append(issues, healthIssue{Color: "yellow", Label: "Stale instances", Count: staleInstanceCount, Link: "/functions"})
	}

	return issues
}
