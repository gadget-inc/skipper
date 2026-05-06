package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

func testState() *skipper.ClusterState {
	fn := skipper.Assignment_builder{
		Namespace:  new("default"),
		Deployment: new("web-app"),
		Tenant:     new("tenant-1"),
		Scale: skipper.Scale_builder{
			MinInstances:           proto.Uint32(1),
			MaxInstances:           proto.Uint32(10),
			TargetCpuUsageMilli:    proto.Uint32(200),
			TargetMemoryUsageMib:   proto.Uint32(256),
			TargetInFlightRequests: proto.Uint32(50),
		}.Build(),
	}.Build()

	inst := skipper.Instance_builder{
		Assignment:     fn,
		Name:           new("web-app-abc123"),
		Addr:           new("10.0.1.1:8080"),
		ReplicaSet:     new("web-app-rs-1"),
		AssignedAt:     timestamppb.Now(),
		ReadyAt:        timestamppb.Now(),
		CpuUsageMilli:  proto.Uint32(50),
		MemoryUsageMib: proto.Uint32(128),
	}.Build()

	sup := &skipper.SupervisorState{}
	sup.SetAssignment(fn)
	sup.SetInstances([]*skipper.Instance{inst})
	sup.SetResponsibleControllerIp("10.0.0.1")
	sup.SetActiveReplicaSet("web-app-rs-1")

	state := &skipper.ClusterState{}
	state.SetPodIp("10.0.0.1")
	state.SetStartedAt(timestamppb.Now())
	state.SetControllerIps([]string{"10.0.0.1"})
	state.SetSupervisors([]*skipper.SupervisorState{sup})

	return state
}

func testServer() *Server {
	return New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})
}

func TestHandlers(t *testing.T) {
	t.Parallel()
	srv := testServer()

	routes := []struct {
		name     string
		path     string
		status   int
		contains string
	}{
		{name: "dashboard", path: "/", status: 200, contains: "Dashboard"},
		{name: "functions", path: "/functions", status: 200, contains: "Functions"},
		{name: "function detail", path: "/functions/default%3Aweb-app%3Atenant-1", status: 200, contains: "web-app"},
		{name: "function not found", path: "/functions/nonexistent", status: 200, contains: "Function not found"},
		{name: "controllers", path: "/controllers", status: 200, contains: "Controllers"},
		{name: "controller detail", path: "/controllers/10.0.0.1", status: 200, contains: "10.0.0.1"},
		{name: "controller not found", path: "/controllers/10.0.0.99", status: 200, contains: "Controller not found"},
		{name: "routers", path: "/routers", status: 200, contains: "Routers"},
		{name: "router detail", path: "/routers/10.0.0.1", status: 200, contains: "Router"},
		{name: "deployments", path: "/deployments", status: 200, contains: "Deployments"},
		{name: "deployment detail", path: "/deployments/web-app", status: 200, contains: "Deployment web-app"},
		{name: "deployment not found", path: "/deployments/nonexistent", status: 200, contains: ""},
		{name: "tenants", path: "/tenants", status: 200, contains: "Tenants"},
		{name: "tenant detail", path: "/tenants/tenant-1", status: 200, contains: "tenant-1"},
		{name: "tenant not found", path: "/tenants/nonexistent", status: 200, contains: "Tenant not found"},
		{name: "events", path: "/events", status: 200, contains: "Events"},
		{name: "config", path: "/config", status: 200, contains: "Configuration"},
		{name: "healthz", path: "/healthz", status: 200, contains: "ok"},
		{name: "404", path: "/nonexistent", status: 404, contains: ""},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			assert.Equal(t, w.Code, tc.status)
			if tc.contains != "" {
				assert.Assert(t, strings.Contains(w.Body.String(), tc.contains), "body should contain %q", tc.contains)
			}
		})
	}
}

func TestDashboardContent(t *testing.T) {
	t.Parallel()
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Assert(t, strings.Contains(body, "skipper"))
	assert.Assert(t, strings.Contains(body, "Tenants"))
	assert.Assert(t, strings.Contains(body, "Functions"))
	assert.Assert(t, strings.Contains(body, "Deployments"))
	assert.Assert(t, strings.Contains(body, "Instances"))
	assert.Assert(t, strings.Contains(body, "Uptime"))
	assert.Assert(t, strings.Contains(body, "Top Deployments"))
	assert.Assert(t, strings.Contains(body, "data-on-interval"))
}

func TestFunctionDetail(t *testing.T) {
	t.Parallel()
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/functions/default%3Aweb-app%3Atenant-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Assert(t, strings.Contains(body, "web-app"))
	assert.Assert(t, strings.Contains(body, "tenant-1"))
	assert.Assert(t, strings.Contains(body, "Scale Targets"))
	assert.Assert(t, strings.Contains(body, "web-app-abc123"))
}

func TestInstanceDetail(t *testing.T) {
	t.Parallel()
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/instances/web-app-abc123", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Equal(t, w.Code, 200)
	assert.Assert(t, strings.Contains(body, "web-app-abc123"))
	assert.Assert(t, strings.Contains(body, "10.0.1.1:8080"))
	assert.Assert(t, strings.Contains(body, "Assigned"))
	assert.Assert(t, strings.Contains(body, "Ready"))
	assert.Assert(t, strings.Contains(body, "Serving"))
}

func TestInstanceNotFound(t *testing.T) {
	t.Parallel()
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/instances/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, w.Code, 200)
	assert.Assert(t, strings.Contains(w.Body.String(), "Instance not found"))
}

func TestContentType(t *testing.T) {
	t.Parallel()
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, w.Header().Get("Content-Type"), "text/html; charset=utf-8")
}

func TestEventsFiltering(t *testing.T) {
	t.Parallel()

	fn := skipper.Assignment_builder{
		Namespace:  new("default"),
		Deployment: new("web-app"),
		Tenant:     new("tenant-1"),
	}.Build()

	event1 := &skipper.Event{}
	event1.SetAssignment(fn)
	event1.SetType(skipper.EventType_EVENT_TYPE_SCALE_UP)
	event1.SetSeverity(skipper.EventSeverity_EVENT_SEVERITY_INFO)
	event1.SetMessage("scaled up")
	event1.SetTimestamp(timestamppb.Now())

	event2 := &skipper.Event{}
	event2.SetAssignment(fn)
	event2.SetType(skipper.EventType_EVENT_TYPE_HEARTBEAT_TIMEOUT)
	event2.SetSeverity(skipper.EventSeverity_EVENT_SEVERITY_WARN)
	event2.SetMessage("timeout")
	event2.SetTimestamp(timestamppb.Now())

	events := []*skipper.Event{event1, event2}

	t.Run("no filter", func(t *testing.T) {
		t.Parallel()
		filtered := filterEvents(events, "", "")
		assert.Equal(t, len(filtered), 2)
	})

	t.Run("filter by severity info", func(t *testing.T) {
		t.Parallel()
		filtered := filterEvents(events, "", "1")
		assert.Equal(t, len(filtered), 1)
		assert.Equal(t, filtered[0].GetMessage(), "scaled up")
	})

	t.Run("filter by severity warn", func(t *testing.T) {
		t.Parallel()
		filtered := filterEvents(events, "", "2")
		assert.Equal(t, len(filtered), 1)
		assert.Equal(t, filtered[0].GetMessage(), "timeout")
	})

	t.Run("filter by function", func(t *testing.T) {
		t.Parallel()
		filtered := filterEvents(events, "web-app", "")
		assert.Equal(t, len(filtered), 2)
	})

	t.Run("filter by function no match", func(t *testing.T) {
		t.Parallel()
		filtered := filterEvents(events, "nonexistent", "")
		assert.Equal(t, len(filtered), 0)
	})
}

func TestCountInstances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		state     *skipper.ClusterState
		wantReady int
		wantTotal int
	}{
		{
			name:      "populated state",
			state:     testState(),
			wantReady: 1,
			wantTotal: 1,
		},
		{
			name:      "empty state",
			state:     &skipper.ClusterState{},
			wantReady: 0,
			wantTotal: 0,
		},
		{
			name: "non-ready instance",
			state: func() *skipper.ClusterState {
				fn := skipper.Assignment_builder{
					Namespace:  new("default"),
					Deployment: new("app"),
					Tenant:     new("t1"),
				}.Build()
				inst := skipper.Instance_builder{
					Assignment: fn,
					Name:       new("app-xyz"),
					Addr:       new("10.0.0.2:8080"),
					AssignedAt: timestamppb.Now(),
					// No ReadyAt — pending instance
				}.Build()
				sup := &skipper.SupervisorState{}
				sup.SetAssignment(fn)
				sup.SetInstances([]*skipper.Instance{inst})
				state := &skipper.ClusterState{}
				state.SetSupervisors([]*skipper.SupervisorState{sup})
				return state
			}(),
			wantReady: 0,
			wantTotal: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ready, total := countInstances(tc.state)
			assert.Equal(t, ready, tc.wantReady)
			assert.Equal(t, total, tc.wantTotal)
		})
	}
}

func TestCountReady(t *testing.T) {
	t.Parallel()

	readyInst := skipper.Instance_builder{
		Name:    new("ready"),
		ReadyAt: timestamppb.Now(),
	}.Build()
	pendingInst := skipper.Instance_builder{
		Name: new("pending"),
	}.Build()

	tests := []struct {
		name      string
		instances []*skipper.Instance
		want      int
	}{
		{name: "nil slice", instances: nil, want: 0},
		{name: "one ready", instances: []*skipper.Instance{readyInst}, want: 1},
		{name: "one pending", instances: []*skipper.Instance{pendingInst}, want: 0},
		{name: "mixed", instances: []*skipper.Instance{readyInst, pendingInst}, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, countReady(tc.instances), tc.want)
		})
	}
}

func TestFindSupervisor(t *testing.T) {
	t.Parallel()
	state := testState()

	tests := []struct {
		name  string
		key   string
		found bool
	}{
		{name: "found", key: "default:web-app:tenant-1", found: true},
		{name: "not found", key: "nonexistent", found: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sup := findSupervisor(state, tc.key)
			if tc.found {
				assert.Assert(t, sup != nil)
			} else {
				assert.Assert(t, sup == nil)
			}
		})
	}
}

func TestStaleInstances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func() *skipper.SupervisorState
		wantCount int
	}{
		{
			name: "all match active RS",
			setup: func() *skipper.SupervisorState {
				inst := skipper.Instance_builder{
					Name:       new("pod-1"),
					ReplicaSet: new("rs-1"),
				}.Build()
				sup := &skipper.SupervisorState{}
				sup.SetInstances([]*skipper.Instance{inst})
				sup.SetActiveReplicaSet("rs-1")
				return sup
			},
			wantCount: 0,
		},
		{
			name: "one stale instance",
			setup: func() *skipper.SupervisorState {
				inst := skipper.Instance_builder{
					Name:       new("pod-1"),
					ReplicaSet: new("rs-old"),
				}.Build()
				sup := &skipper.SupervisorState{}
				sup.SetInstances([]*skipper.Instance{inst})
				sup.SetActiveReplicaSet("rs-new")
				return sup
			},
			wantCount: 1,
		},
		{
			name: "mixed stale and current",
			setup: func() *skipper.SupervisorState {
				current := skipper.Instance_builder{
					Name:       new("pod-current"),
					ReplicaSet: new("rs-new"),
				}.Build()
				stale := skipper.Instance_builder{
					Name:       new("pod-stale"),
					ReplicaSet: new("rs-old"),
				}.Build()
				sup := &skipper.SupervisorState{}
				sup.SetInstances([]*skipper.Instance{current, stale})
				sup.SetActiveReplicaSet("rs-new")
				return sup
			},
			wantCount: 1,
		},
		{
			name: "no active RS set",
			setup: func() *skipper.SupervisorState {
				inst := skipper.Instance_builder{
					Name:       new("pod-1"),
					ReplicaSet: new("rs-1"),
				}.Build()
				sup := &skipper.SupervisorState{}
				sup.SetInstances([]*skipper.Instance{inst})
				return sup
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := staleInstances(tc.setup())
			assert.Equal(t, len(result), tc.wantCount)
		})
	}
}

func TestBuildControllerData(t *testing.T) {
	t.Parallel()
	state := testState()

	rows, dist, imbalanced := buildControllerData(state)

	assert.Equal(t, len(rows), 1)
	assert.Equal(t, rows[0].IP, "10.0.0.1")
	assert.Assert(t, rows[0].IsSelf)
	assert.Equal(t, rows[0].Functions, 1)
	assert.Equal(t, rows[0].ReadyInstances, 1)
	assert.Equal(t, rows[0].TotalInstances, 1)

	assert.Equal(t, len(dist), 1)
	assert.Assert(t, !imbalanced)
}

func TestBuildControllerDataImbalanced(t *testing.T) {
	t.Parallel()

	fn1 := skipper.Assignment_builder{
		Namespace: new("default"), Deployment: new("app1"), Tenant: new("t1"),
	}.Build()
	fn2 := skipper.Assignment_builder{
		Namespace: new("default"), Deployment: new("app2"), Tenant: new("t1"),
	}.Build()
	fn3 := skipper.Assignment_builder{
		Namespace: new("default"), Deployment: new("app3"), Tenant: new("t1"),
	}.Build()

	sup1 := &skipper.SupervisorState{}
	sup1.SetAssignment(fn1)
	sup1.SetResponsibleControllerIp("10.0.0.1")

	sup2 := &skipper.SupervisorState{}
	sup2.SetAssignment(fn2)
	sup2.SetResponsibleControllerIp("10.0.0.1")

	sup3 := &skipper.SupervisorState{}
	sup3.SetAssignment(fn3)
	sup3.SetResponsibleControllerIp("10.0.0.1")

	state := &skipper.ClusterState{}
	state.SetControllerIps([]string{"10.0.0.1", "10.0.0.2"})
	state.SetPodIp("10.0.0.1")
	state.SetSupervisors([]*skipper.SupervisorState{sup1, sup2, sup3})

	rows, _, imbalanced := buildControllerData(state)

	assert.Equal(t, len(rows), 2)
	assert.Assert(t, imbalanced, "3 functions on one controller, 0 on another should be imbalanced")
}

func TestBuildRouterRows(t *testing.T) {
	t.Parallel()

	t.Run("empty state", func(t *testing.T) {
		t.Parallel()
		state := &skipper.ClusterState{}
		rows := buildRouterRows(state)
		assert.Equal(t, len(rows), 0)
	})

	t.Run("with heartbeats", func(t *testing.T) {
		t.Parallel()
		fn := skipper.Assignment_builder{
			Namespace: new("default"), Deployment: new("app"), Tenant: new("t1"),
		}.Build()
		hb := &skipper.HeartbeatState{}
		hb.SetRouterIp("10.0.1.1")
		hb.SetHeartbeat(skipper.Heartbeat_builder{InFlightRequests: proto.Uint32(5)}.Build())

		sup := &skipper.SupervisorState{}
		sup.SetAssignment(fn)
		sup.SetRouterHeartbeats([]*skipper.HeartbeatState{hb})

		state := &skipper.ClusterState{}
		state.SetSupervisors([]*skipper.SupervisorState{sup})

		rows := buildRouterRows(state)
		assert.Equal(t, len(rows), 1)
		assert.Equal(t, rows[0].IP, "10.0.1.1")
		assert.Equal(t, rows[0].Functions, 1)
		assert.Equal(t, rows[0].InFlight, uint32(5))
	})
}

func TestBuildRouterEntries(t *testing.T) {
	t.Parallel()

	t.Run("empty state", func(t *testing.T) {
		t.Parallel()
		state := &skipper.ClusterState{}
		entries, inFlight := buildRouterEntries(state, "10.0.0.1")
		assert.Equal(t, len(entries), 0)
		assert.Equal(t, inFlight, uint32(0))
	})

	t.Run("matching IP", func(t *testing.T) {
		t.Parallel()
		fn := skipper.Assignment_builder{
			Namespace: new("default"), Deployment: new("app"), Tenant: new("t1"),
		}.Build()
		hb := &skipper.HeartbeatState{}
		hb.SetRouterIp("10.0.1.1")
		hb.SetHeartbeat(skipper.Heartbeat_builder{InFlightRequests: proto.Uint32(3)}.Build())

		sup := &skipper.SupervisorState{}
		sup.SetAssignment(fn)
		sup.SetRouterHeartbeats([]*skipper.HeartbeatState{hb})

		state := &skipper.ClusterState{}
		state.SetSupervisors([]*skipper.SupervisorState{sup})

		entries, inFlight := buildRouterEntries(state, "10.0.1.1")
		assert.Equal(t, len(entries), 1)
		assert.Equal(t, inFlight, uint32(3))
	})

	t.Run("non-matching IP", func(t *testing.T) {
		t.Parallel()
		hb := &skipper.HeartbeatState{}
		hb.SetRouterIp("10.0.1.1")

		sup := &skipper.SupervisorState{}
		sup.SetRouterHeartbeats([]*skipper.HeartbeatState{hb})

		state := &skipper.ClusterState{}
		state.SetSupervisors([]*skipper.SupervisorState{sup})

		entries, inFlight := buildRouterEntries(state, "10.0.1.2")
		assert.Equal(t, len(entries), 0)
		assert.Equal(t, inFlight, uint32(0))
	})
}

func TestEmptyState(t *testing.T) {
	t.Parallel()

	srv := New(func(ctx context.Context) *skipper.ClusterState {
		state := &skipper.ClusterState{}
		state.SetPodIp("10.0.0.1")
		state.SetStartedAt(timestamppb.Now())
		return state
	})

	tests := []struct {
		name     string
		path     string
		status   int
		contains string
	}{
		{name: "dashboard", path: "/", status: 200, contains: ""},
		{name: "functions", path: "/functions", status: 200, contains: "No functions"},
		{name: "events", path: "/events", status: 200, contains: "No events"},
		{name: "controllers", path: "/controllers", status: 200, contains: "No controllers"},
		{name: "routers", path: "/routers", status: 200, contains: "No routers"},
		{name: "deployments", path: "/deployments", status: 200, contains: ""},
		{name: "tenants", path: "/tenants", status: 200, contains: "No tenants"},
		{name: "function not found", path: "/functions/nonexistent", status: 200, contains: "Function not found"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			assert.Equal(t, w.Code, tc.status)
			if tc.contains != "" {
				assert.Assert(t, strings.Contains(w.Body.String(), tc.contains),
					"body should contain %q", tc.contains)
			}
		})
	}
}

func TestBuildTenantRows(t *testing.T) {
	t.Parallel()

	t.Run("empty state", func(t *testing.T) {
		t.Parallel()
		state := &skipper.ClusterState{}
		rows := buildTenantRows(state)
		assert.Equal(t, len(rows), 0)
	})

	t.Run("single tenant", func(t *testing.T) {
		t.Parallel()
		state := testState()
		rows := buildTenantRows(state)
		assert.Equal(t, len(rows), 1)
		assert.Equal(t, rows[0].Tenant, "tenant-1")
		assert.Equal(t, rows[0].Functions, 1)
		assert.Equal(t, rows[0].ReadyInstances, 1)
		assert.Equal(t, rows[0].TotalInstances, 1)
		assert.DeepEqual(t, rows[0].Deployments, []string{"web-app"})
	})

	t.Run("multiple tenants sorted", func(t *testing.T) {
		t.Parallel()

		fn1 := skipper.Assignment_builder{
			Namespace:  new("default"),
			Deployment: new("web-app"),
			Tenant:     new("tenant-1"),
		}.Build()
		inst1 := skipper.Instance_builder{
			Assignment: fn1,
			Name:       new("web-app-abc123"),
			Addr:       new("10.0.1.1:8080"),
			AssignedAt: timestamppb.Now(),
			ReadyAt:    timestamppb.Now(),
		}.Build()
		sup1 := &skipper.SupervisorState{}
		sup1.SetAssignment(fn1)
		sup1.SetInstances([]*skipper.Instance{inst1})

		fn2 := skipper.Assignment_builder{
			Namespace:  new("default"),
			Deployment: new("api-server"),
			Tenant:     new("tenant-2"),
		}.Build()
		inst2 := skipper.Instance_builder{
			Assignment: fn2,
			Name:       new("api-server-def456"),
			Addr:       new("10.0.1.2:8080"),
			AssignedAt: timestamppb.Now(),
			ReadyAt:    timestamppb.Now(),
		}.Build()
		inst3 := skipper.Instance_builder{
			Assignment: fn2,
			Name:       new("api-server-ghi789"),
			Addr:       new("10.0.1.3:8080"),
			AssignedAt: timestamppb.Now(),
			// No ReadyAt — pending instance
		}.Build()
		sup2 := &skipper.SupervisorState{}
		sup2.SetAssignment(fn2)
		sup2.SetInstances([]*skipper.Instance{inst2, inst3})

		state := &skipper.ClusterState{}
		state.SetSupervisors([]*skipper.SupervisorState{sup1, sup2})

		rows := buildTenantRows(state)
		assert.Equal(t, len(rows), 2)

		// Should be sorted by tenant name
		assert.Equal(t, rows[0].Tenant, "tenant-1")
		assert.Equal(t, rows[0].Functions, 1)
		assert.Equal(t, rows[0].ReadyInstances, 1)
		assert.Equal(t, rows[0].TotalInstances, 1)
		assert.DeepEqual(t, rows[0].Deployments, []string{"web-app"})

		assert.Equal(t, rows[1].Tenant, "tenant-2")
		assert.Equal(t, rows[1].Functions, 1)
		assert.Equal(t, rows[1].ReadyInstances, 1)
		assert.Equal(t, rows[1].TotalInstances, 2)
		assert.DeepEqual(t, rows[1].Deployments, []string{"api-server"})
	})
}

func TestBuildDeploymentRows(t *testing.T) {
	t.Parallel()

	t.Run("empty state", func(t *testing.T) {
		t.Parallel()
		state := &skipper.ClusterState{}
		rows := buildDeploymentRows(state)
		assert.Equal(t, len(rows), 0)
	})

	t.Run("single deployment", func(t *testing.T) {
		t.Parallel()
		state := testState()
		rows := buildDeploymentRows(state)
		assert.Equal(t, len(rows), 1)
		assert.Equal(t, rows[0].Name, "web-app")
		assert.Equal(t, rows[0].Namespace, "default")
		assert.Equal(t, rows[0].Tenants, 1)
		assert.Equal(t, rows[0].ReadyInstances, 1)
		assert.Equal(t, rows[0].TotalInstances, 1)
	})

	t.Run("multiple deployments shared tenant", func(t *testing.T) {
		t.Parallel()

		fn1 := skipper.Assignment_builder{
			Namespace:  new("default"),
			Deployment: new("api"),
			Tenant:     new("t1"),
		}.Build()
		sup1 := &skipper.SupervisorState{}
		sup1.SetAssignment(fn1)

		fn2 := skipper.Assignment_builder{
			Namespace:  new("default"),
			Deployment: new("web"),
			Tenant:     new("t1"),
		}.Build()
		sup2 := &skipper.SupervisorState{}
		sup2.SetAssignment(fn2)

		state := &skipper.ClusterState{}
		state.SetSupervisors([]*skipper.SupervisorState{sup1, sup2})

		rows := buildDeploymentRows(state)
		assert.Equal(t, len(rows), 2)
		assert.Equal(t, rows[0].Name, "api")
		assert.Equal(t, rows[0].Tenants, 1)
		assert.Equal(t, rows[1].Name, "web")
		assert.Equal(t, rows[1].Tenants, 1)
	})
}

func TestBuildTenantData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		state           *skipper.ClusterState
		tenant          string
		wantFound       bool
		wantReady       int
		wantTotal       int
		wantBreakdowns  int
		checkBreakdowns func(t *testing.T, breakdowns []tenantDeploymentBreakdown)
	}{
		{
			name:           "single deployment",
			state:          testState(),
			tenant:         "tenant-1",
			wantFound:      true,
			wantReady:      1,
			wantTotal:      1,
			wantBreakdowns: 1,
			checkBreakdowns: func(t *testing.T, breakdowns []tenantDeploymentBreakdown) {
				t.Helper()
				assert.Equal(t, breakdowns[0].Deployment, "web-app")
				assert.Equal(t, breakdowns[0].ReadyInstances, 1)
				assert.Equal(t, breakdowns[0].PendingInstances, 0)
				assert.Equal(t, breakdowns[0].TotalInstances, 1)
			},
		},
		{
			name: "multiple deployments",
			state: func() *skipper.ClusterState {
				fn1 := skipper.Assignment_builder{
					Namespace:  new("default"),
					Deployment: new("web-app"),
					Tenant:     new("tenant-1"),
				}.Build()
				inst1 := skipper.Instance_builder{
					Assignment: fn1,
					Name:       new("web-app-abc"),
					Addr:       new("10.0.1.1:8080"),
					AssignedAt: timestamppb.Now(),
					ReadyAt:    timestamppb.Now(),
				}.Build()
				sup1 := &skipper.SupervisorState{}
				sup1.SetAssignment(fn1)
				sup1.SetInstances([]*skipper.Instance{inst1})

				fn2 := skipper.Assignment_builder{
					Namespace:  new("default"),
					Deployment: new("api-server"),
					Tenant:     new("tenant-1"),
				}.Build()
				inst2 := skipper.Instance_builder{
					Assignment: fn2,
					Name:       new("api-server-def"),
					Addr:       new("10.0.1.2:8080"),
					AssignedAt: timestamppb.Now(),
					// No ReadyAt — pending
				}.Build()
				sup2 := &skipper.SupervisorState{}
				sup2.SetAssignment(fn2)
				sup2.SetInstances([]*skipper.Instance{inst2})

				state := &skipper.ClusterState{}
				state.SetSupervisors([]*skipper.SupervisorState{sup1, sup2})
				return state
			}(),
			tenant:         "tenant-1",
			wantFound:      true,
			wantReady:      1,
			wantTotal:      2,
			wantBreakdowns: 2,
			checkBreakdowns: func(t *testing.T, breakdowns []tenantDeploymentBreakdown) {
				t.Helper()
				// Sorted alphabetically: api-server before web-app
				assert.Equal(t, breakdowns[0].Deployment, "api-server")
				assert.Equal(t, breakdowns[0].ReadyInstances, 0)
				assert.Equal(t, breakdowns[0].PendingInstances, 1)
				assert.Equal(t, breakdowns[0].TotalInstances, 1)

				assert.Equal(t, breakdowns[1].Deployment, "web-app")
				assert.Equal(t, breakdowns[1].ReadyInstances, 1)
				assert.Equal(t, breakdowns[1].PendingInstances, 0)
				assert.Equal(t, breakdowns[1].TotalInstances, 1)
			},
		},
		{
			name:           "tenant not found",
			state:          testState(),
			tenant:         "unknown-tenant",
			wantFound:      false,
			wantReady:      0,
			wantTotal:      0,
			wantBreakdowns: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := buildTenantData(tc.state, tc.tenant)
			assert.Equal(t, data.Found, tc.wantFound)
			assert.Equal(t, data.ReadyInstances, tc.wantReady)
			assert.Equal(t, data.TotalInstances, tc.wantTotal)
			assert.Equal(t, len(data.DeploymentBreakdowns), tc.wantBreakdowns)
			if tc.checkBreakdowns != nil {
				tc.checkBreakdowns(t, data.DeploymentBreakdowns)
			}
		})
	}
}

func TestTenantDetail(t *testing.T) {
	t.Parallel()
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/tenants/tenant-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Equal(t, w.Code, 200)
	assert.Assert(t, strings.Contains(body, "tenant-1"))
	assert.Assert(t, strings.Contains(body, "web-app"))
}

func makeSup(ns, deploy, tenant string, instanceCount int) *skipper.SupervisorState {
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

func multiSupServer() *Server {
	return New(func(ctx context.Context) *skipper.ClusterState {
		state := &skipper.ClusterState{}
		state.SetPodIp("10.0.0.1")
		state.SetStartedAt(timestamppb.Now())
		state.SetControllerIps([]string{"10.0.0.1"})
		state.SetSupervisors([]*skipper.SupervisorState{
			makeSup("default", "web-app", "tenant-1", 3),
			makeSup("staging", "api-server", "tenant-2", 1),
			makeSup("production", "worker", "tenant-1", 5),
		})
		return state
	})
}

func TestFunctionsQueryParams(t *testing.T) {
	t.Parallel()
	srv := multiSupServer()

	t.Run("search filters supervisors", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/functions?search=web", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, "web-app"), "body should contain matching supervisor")
		assert.Assert(t, !strings.Contains(body, "api-server"), "body should not contain non-matching supervisor")
	})

	t.Run("sort by instances desc", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/functions?sort=instances&dir=desc", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		// "worker" has 5 instances, should appear before "web-app" (3) and "api-server" (1)
		workerIdx := strings.Index(body, "worker")
		webIdx := strings.Index(body, "web-app")
		apiIdx := strings.Index(body, "api-server")
		assert.Assert(t, workerIdx >= 0, "body should contain worker")
		assert.Assert(t, workerIdx < webIdx, "worker (5 instances) should appear before web-app (3 instances)")
		assert.Assert(t, workerIdx < apiIdx, "worker (5 instances) should appear before api-server (1 instance)")
	})

	t.Run("search nonexistent shows empty", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/functions?search=nonexistent", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, "No functions"), "body should show empty state")
	})

	t.Run("no params uses defaults", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/functions", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		// All three supervisors should be present
		assert.Assert(t, strings.Contains(body, "web-app"), "body should contain web-app")
		assert.Assert(t, strings.Contains(body, "api-server"), "body should contain api-server")
		assert.Assert(t, strings.Contains(body, "worker"), "body should contain worker")
		// Default sort is deployment asc: api-server < web-app < worker
		apiIdx := strings.Index(body, "api-server")
		webIdx := strings.Index(body, "web-app")
		workerIdx := strings.Index(body, "worker")
		assert.Assert(t, apiIdx < webIdx, "api-server should appear before web-app (deployment asc)")
		assert.Assert(t, webIdx < workerIdx, "web-app should appear before worker (deployment asc)")
	})

	t.Run("signal initialization", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/functions?search=web&sort=tenant&dir=desc", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		// data-signals is rendered as JSON inside an HTML attribute, so the
		// double-quotes around keys/values arrive as `&#34;` -- the browser
		// decodes them before Datastar parses the expression.
		assert.Assert(t, strings.Contains(body, `&#34;fnSearch&#34;:&#34;web&#34;`), "data-signals should contain fnSearch")
		assert.Assert(t, strings.Contains(body, `&#34;fnSort&#34;:&#34;tenant&#34;`), "data-signals should contain fnSort")
		assert.Assert(t, strings.Contains(body, `&#34;fnSortDir&#34;:&#34;desc&#34;`), "data-signals should contain fnSortDir")
	})

	t.Run("signal initialization escapes quote and ampersand in user input", func(t *testing.T) {
		t.Parallel()
		// A bare apostrophe in `search` would close the inline string
		// `'{{.FnSearch}}'` mid-attribute and make Datastar fail to
		// parse the entire data-signals expression. JSON-encoding the
		// signal escapes the apostrophe so the rendered attribute is
		// parseable after the browser HTML-decodes it.
		req := httptest.NewRequest(http.MethodGet, "/functions?search=it%27s+%26+more", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		// `'` is rendered as the HTML entity `&#39;`; `&` is JSON-escaped
		// to the 6-character sequence `&` (encoding/json defaults
		// to HTML-safe output) which the JS parser decodes back to `&`.
		assert.Assert(t, strings.Contains(body, "&#34;fnSearch&#34;:&#34;it&#39;s \\u0026 more&#34;"),
			"data-signals must safely encode apostrophe and ampersand, got: %s", body)
	})
}

func TestTenantsQueryParams(t *testing.T) {
	t.Parallel()
	srv := multiSupServer()

	t.Run("search filters tenants", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/tenants?search=tenant-2", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, "tenant-2"), "body should contain matching tenant")
		// tenant-1 has deployments web-app and worker; searching "tenant-2" should exclude it
		// But we need to be careful — tenant-1 appears in the page title or nav?
		// Check that tenant-1 is not in the tenant rows by looking for its deployments
		assert.Assert(t, !strings.Contains(body, "worker"), "body should not contain non-matching tenant's deployment")
	})

	t.Run("sort by functions desc", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/tenants?sort=functions&dir=desc", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		// tenant-1 has 2 functions (web-app + worker), tenant-2 has 1 (api-server)
		t1Idx := strings.Index(body, "tenant-1")
		t2Idx := strings.Index(body, "tenant-2")
		assert.Assert(t, t1Idx >= 0, "body should contain tenant-1")
		assert.Assert(t, t2Idx >= 0, "body should contain tenant-2")
		assert.Assert(t, t1Idx < t2Idx, "tenant-1 (2 functions) should appear before tenant-2 (1 function)")
	})

	t.Run("signal initialization", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/tenants?search=acme", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, `&#34;tenantSearch&#34;:&#34;acme&#34;`), "data-signals should contain tenantSearch")
	})
}

func TestFilterSupervisors(t *testing.T) {
	t.Parallel()

	sups := []*skipper.SupervisorState{
		makeSup("default", "web-app", "tenant-1", 1),
		makeSup("staging", "api-server", "tenant-2", 2),
		makeSup("production", "worker", "tenant-1", 3),
	}

	tests := []struct {
		name   string
		search string
		want   int
	}{
		{name: "empty search returns all", search: "", want: 3},
		{name: "deployment match", search: "web", want: 1},
		{name: "deployment match case-insensitive", search: "WEB", want: 1},
		{name: "namespace match", search: "staging", want: 1},
		{name: "tenant match", search: "tenant-1", want: 2},
		{name: "no match", search: "nonexistent", want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := filterSupervisors(sups, tc.search)
			assert.Equal(t, len(result), tc.want)
		})
	}
}

func TestSortSupervisors(t *testing.T) {
	t.Parallel()

	sups := []*skipper.SupervisorState{
		makeSup("default", "charlie", "tenant-b", 3),
		makeSup("staging", "alpha", "tenant-a", 1),
		makeSup("production", "bravo", "tenant-c", 2),
	}

	tests := []struct {
		name      string
		col       string
		dir       string
		wantFirst string
		wantLast  string
	}{
		{name: "deployment asc", col: "deployment", dir: "asc", wantFirst: "alpha", wantLast: "charlie"},
		{name: "deployment desc", col: "deployment", dir: "desc", wantFirst: "charlie", wantLast: "alpha"},
		{name: "instances desc", col: "instances", dir: "desc", wantFirst: "charlie", wantLast: "alpha"},
		{name: "unknown col defaults to deployment", col: "unknown", dir: "asc", wantFirst: "alpha", wantLast: "charlie"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := sortSupervisors(sups, tc.col, tc.dir)
			assert.Equal(t, result[0].GetAssignment().GetDeployment(), tc.wantFirst)
			assert.Equal(t, result[len(result)-1].GetAssignment().GetDeployment(), tc.wantLast)
		})
	}
}

func eventsServer() *Server {
	return New(func(ctx context.Context) *skipper.ClusterState {
		fn1 := skipper.Assignment_builder{
			Namespace:  new("default"),
			Deployment: new("web-app"),
			Tenant:     new("tenant-1"),
		}.Build()
		fn2 := skipper.Assignment_builder{
			Namespace:  new("staging"),
			Deployment: new("api-server"),
			Tenant:     new("tenant-2"),
		}.Build()

		event1 := &skipper.Event{}
		event1.SetAssignment(fn1)
		event1.SetType(skipper.EventType_EVENT_TYPE_SCALE_UP)
		event1.SetSeverity(skipper.EventSeverity_EVENT_SEVERITY_INFO)
		event1.SetMessage("scaled up web-app")
		event1.SetTimestamp(timestamppb.Now())

		event2 := &skipper.Event{}
		event2.SetAssignment(fn1)
		event2.SetType(skipper.EventType_EVENT_TYPE_HEARTBEAT_TIMEOUT)
		event2.SetSeverity(skipper.EventSeverity_EVENT_SEVERITY_WARN)
		event2.SetMessage("timeout web-app")
		event2.SetTimestamp(timestamppb.Now())

		event3 := &skipper.Event{}
		event3.SetAssignment(fn2)
		event3.SetType(skipper.EventType_EVENT_TYPE_SCALE_UP)
		event3.SetSeverity(skipper.EventSeverity_EVENT_SEVERITY_INFO)
		event3.SetMessage("scaled up api-server")
		event3.SetTimestamp(timestamppb.Now())

		state := &skipper.ClusterState{}
		state.SetPodIp("10.0.0.1")
		state.SetStartedAt(timestamppb.Now())
		state.SetControllerIps([]string{"10.0.0.1"})
		state.SetEvents([]*skipper.Event{event1, event2, event3})
		return state
	})
}

func TestEventsQueryParams(t *testing.T) {
	t.Parallel()
	srv := eventsServer()

	t.Run("severity filters events", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/events?severity=1", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, "scaled up web-app"), "body should contain info event for web-app")
		assert.Assert(t, strings.Contains(body, "scaled up api-server"), "body should contain info event for api-server")
		assert.Assert(t, !strings.Contains(body, "timeout web-app"), "body should not contain warn event")
	})

	t.Run("function filters events", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/events?function=web", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, "scaled up web-app"), "body should contain web-app events")
		assert.Assert(t, strings.Contains(body, "timeout web-app"), "body should contain web-app timeout event")
		assert.Assert(t, !strings.Contains(body, "scaled up api-server"), "body should not contain api-server events")
	})

	t.Run("combined filters", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/events?severity=2&function=web", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, "timeout web-app"), "body should contain warn event for web-app")
		assert.Assert(t, !strings.Contains(body, "scaled up web-app"), "body should not contain info event")
		assert.Assert(t, !strings.Contains(body, "scaled up api-server"), "body should not contain api-server events")
	})

	t.Run("no params shows all events", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/events", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, "scaled up web-app"), "body should contain all events")
		assert.Assert(t, strings.Contains(body, "timeout web-app"), "body should contain all events")
		assert.Assert(t, strings.Contains(body, "scaled up api-server"), "body should contain all events")
	})

	t.Run("signal initialization with severity", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/events?severity=2&function=web", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, `&#34;eventSeverity&#34;:&#34;2&#34;`), "data-signals should contain eventSeverity")
		assert.Assert(t, strings.Contains(body, `&#34;eventFunction&#34;:&#34;web&#34;`), "data-signals should contain eventFunction")
	})

	t.Run("signal initialization defaults", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/events", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, w.Code, 200)
		assert.Assert(t, strings.Contains(body, `&#34;eventSeverity&#34;:&#34;all&#34;`), "data-signals should default eventSeverity to 'all'")
		assert.Assert(t, strings.Contains(body, `&#34;eventFunction&#34;:&#34;&#34;`), "data-signals should default eventFunction to empty")
	})
}

func TestFilterTenantRows(t *testing.T) {
	t.Parallel()

	rows := []tenantRow{
		{Tenant: "acme-corp", Deployments: []string{"web-app", "api"}},
		{Tenant: "globex", Deployments: []string{"worker"}},
		{Tenant: "initech", Deployments: []string{"api", "dashboard"}},
	}

	tests := []struct {
		name   string
		search string
		want   int
	}{
		{name: "empty search returns all", search: "", want: 3},
		{name: "tenant name match", search: "acme", want: 1},
		{name: "deployment name match", search: "dashboard", want: 1},
		{name: "no match", search: "nonexistent", want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := filterTenantRows(rows, tc.search)
			assert.Equal(t, len(result), tc.want)
		})
	}
}

func TestSortTenantRows(t *testing.T) {
	t.Parallel()

	rows := []tenantRow{
		{Tenant: "charlie", Functions: 3, ReadyInstances: 5},
		{Tenant: "alpha", Functions: 1, ReadyInstances: 2},
		{Tenant: "bravo", Functions: 2, ReadyInstances: 10},
	}

	tests := []struct {
		name      string
		col       string
		dir       string
		wantFirst string
		wantLast  string
	}{
		{name: "tenant asc", col: "tenant", dir: "asc", wantFirst: "alpha", wantLast: "charlie"},
		{name: "tenant desc", col: "tenant", dir: "desc", wantFirst: "charlie", wantLast: "alpha"},
		{name: "functions desc", col: "functions", dir: "desc", wantFirst: "charlie", wantLast: "alpha"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := sortTenantRows(rows, tc.col, tc.dir)
			assert.Equal(t, result[0].Tenant, tc.wantFirst)
			assert.Equal(t, result[len(result)-1].Tenant, tc.wantLast)
		})
	}
}
