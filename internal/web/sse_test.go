package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gadget-inc/skipper/internal/skipper"
	"gotest.tools/v3/assert"
)

func TestSSEEndpoints(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	sseRoutes := []struct {
		name string
		path string
	}{
		{name: "dashboard", path: "/sse/dashboard"},
		{name: "assignments", path: "/sse/assignments"},
		{name: "assignment", path: "/sse/assignment/default%3Aweb-app%3Atenant-1"},
		{name: "controllers", path: "/sse/controllers"},
		{name: "controller", path: "/sse/controller/10.0.0.1"},
		{name: "routers", path: "/sse/routers"},
		{name: "instance", path: "/sse/instance/web-app-abc123"},
		{name: "tenants", path: "/sse/tenants"},
		{name: "tenant", path: "/sse/tenant/tenant-1"},
		{name: "deployments", path: "/sse/deployments"},
		{name: "deployment", path: "/sse/deployment/web-app"},
		{name: "events", path: "/sse/events"},
	}

	for _, tc := range sseRoutes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			assert.Equal(t, w.Code, 200)
			// SSE responses should have content-type text/event-stream
			ct := w.Header().Get("Content-Type")
			assert.Equal(t, ct, "text/event-stream")
		})
	}
}

// TestSSELegacyRoutesRedirect ensures the legacy /sse/functions and
// /sse/function/{key} paths return 301 to their /sse/assignments and
// /sse/assignment/{key} counterparts. Datastar's EventSource follows the
// 30x on the initial handshake, so existing browser sessions keep
// streaming through the rollover.
func TestSSELegacyRoutesRedirect(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	cases := []struct {
		name   string
		path   string
		target string
	}{
		{name: "sse functions", path: "/sse/functions", target: "/sse/assignments"},
		{name: "sse function key", path: "/sse/function/default%3Aweb-app%3Atenant-1", target: "/sse/assignment/default:web-app:tenant-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			assert.Equal(t, w.Code, http.StatusMovedPermanently)
			assert.Equal(t, w.Header().Get("Location"), tc.target)
		})
	}
}

func TestSSEDashboardContent(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/dashboard", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	// Should contain SSE event data with HTML fragments
	assert.Assert(t, len(body) > 0, "SSE response should not be empty")
}

func TestSSEAssignmentNotFound(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/assignment/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	// Should return 200 with empty body (no fragments to send)
	assert.Equal(t, w.Code, 200)
}

func TestSSEInstanceNotFound(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/instance/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, w.Code, 200)
}

func TestSSEDashboardFragments(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/dashboard", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Assert(t, strings.Contains(body, "datastar-patch-elements"),
		"should contain datastar event type")
	assert.Assert(t, strings.Contains(body, "health-indicators"),
		"should contain health-indicators selector")
	assert.Assert(t, strings.Contains(body, "supervisors-table"),
		"should contain supervisors-table selector")
}

func TestSSEAssignmentsFragments(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/assignments", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Assert(t, strings.Contains(body, "datastar-patch-elements"),
		"should contain datastar event type")
	assert.Assert(t, strings.Contains(body, "assignments-table"),
		"should contain assignments-table selector")
}

func TestSSEPatchEventsUseInnerMode(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	sseRoutes := []struct {
		name string
		path string
	}{
		{name: "dashboard", path: "/sse/dashboard"},
		{name: "assignments", path: "/sse/assignments"},
		{name: "assignment", path: "/sse/assignment/default%3Aweb-app%3Atenant-1"},
		{name: "controllers", path: "/sse/controllers"},
		{name: "controller", path: "/sse/controller/10.0.0.1"},
		{name: "routers", path: "/sse/routers"},
		{name: "instance", path: "/sse/instance/web-app-abc123"},
		{name: "tenants", path: "/sse/tenants"},
		{name: "tenant", path: "/sse/tenant/tenant-1"},
		{name: "deployments", path: "/sse/deployments"},
		{name: "deployment", path: "/sse/deployment/web-app"},
		{name: "events", path: "/sse/events"},
	}

	for _, tc := range sseRoutes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			assert.Equal(t, w.Code, 200)

			body := w.Body.String()
			blocks := strings.Split(body, "event: datastar-patch-elements")
			var patchCount int
			for _, block := range blocks {
				block = strings.TrimSpace(block)
				if block == "" {
					continue
				}
				patchCount++
				assert.Assert(t, strings.Contains(block, "mode inner"),
					"patch block missing 'mode inner' for %s:\n%s", tc.name, block)
			}
			if patchCount == 0 {
				t.Skipf("no patch-elements events for %s", tc.name)
			}
		})
	}
}

func TestSSETenantsFragments(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/tenants", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Assert(t, strings.Contains(body, "datastar-patch-elements"),
		"should contain datastar event type")
	assert.Assert(t, strings.Contains(body, "tenants-table"),
		"should contain tenants-table selector")
}

func TestSSETenantFragments(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/tenant/tenant-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Equal(t, w.Code, 200)
	assert.Assert(t, strings.Contains(body, "datastar-patch-elements"),
		"should contain datastar event type")
	assert.Assert(t, strings.Contains(body, "tenant-1"),
		"should contain tenant name")
}

func TestSSETenantNotFound(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/tenant/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	// Should return 200 with empty body (no fragments to send)
	assert.Equal(t, w.Code, 200)
}

func TestSSEDeploymentsFragments(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/deployments", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Equal(t, w.Code, 200)
	assert.Assert(t, strings.Contains(body, "datastar-patch-elements"),
		"should contain datastar event type")
	assert.Assert(t, strings.Contains(body, "deployments-table"),
		"should contain deployments-table selector")
}

func TestSSEEventsFragments(t *testing.T) {
	t.Parallel()
	srv := New(func(ctx context.Context) *skipper.ClusterState {
		return testState()
	})

	req := httptest.NewRequest(http.MethodGet, "/sse/events", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	assert.Assert(t, strings.Contains(body, "datastar-patch-elements"),
		"should contain datastar event type")
	assert.Assert(t, strings.Contains(body, "events-table"),
		"should contain events-table selector")
}
