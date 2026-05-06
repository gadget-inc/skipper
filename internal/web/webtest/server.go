// Package webtest exposes the in-process *web.Server fixture used by
// the chromedp-driven URL-state suite in internal/web. The fixture
// seeds a controller stub with a deterministic snapshot (functions,
// instances, controller peers, events, config) so URL-state and
// rendering assertions are stable run to run.
package webtest

import (
	"context"
	"fmt"

	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/gadget-inc/skipper/internal/web"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// New returns a *web.Server seeded with three supervisors:
//   - default/web-app/tenant-1 (3 instances)
//   - staging/api-server/tenant-2 (1 instance)
//   - production/worker/tenant-1 (5 instances)
func New() *web.Server {
	return web.New(func(context.Context) *skipper.ClusterState {
		state := &skipper.ClusterState{}
		state.SetPodIp("10.0.0.1")
		state.SetStartedAt(timestamppb.Now())
		state.SetControllerIps([]string{"10.0.0.1"})
		state.SetSupervisors([]*skipper.SupervisorState{
			supervisor("default", "web-app", "tenant-1", 3),
			supervisor("staging", "api-server", "tenant-2", 1),
			supervisor("production", "worker", "tenant-1", 5),
		})
		return state
	})
}

func supervisor(ns, deploy, tenant string, instanceCount int) *skipper.SupervisorState {
	fn := skipper.Assignment_builder{
		Namespace:  new(ns),
		Deployment: new(deploy),
		Tenant:     new(tenant),
	}.Build()
	instances := make([]*skipper.Instance, instanceCount)
	for i := range instances {
		instances[i] = skipper.Instance_builder{
			Assignment: fn,
			Name:       new(fmt.Sprintf("%s-%d", deploy, i)),
			Addr:       new(fmt.Sprintf("10.0.0.%d:8080", i)),
			ReadyAt:    timestamppb.Now(),
		}.Build()
	}
	sup := &skipper.SupervisorState{}
	sup.SetAssignment(fn)
	sup.SetInstances(instances)
	return sup
}
