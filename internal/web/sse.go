package web

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/starfederation/datastar-go/datastar"
)

func (s *Server) renderFragment(page, tmpl string, data any) (string, error) {
	t, ok := s.getPage(page)
	if !ok {
		return "", fmt.Errorf("unknown page: %s", page)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, tmpl, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// patchFragment renders a named template and patches the element whose ID
// matches the template name. It uses inner mode so the target element (and
// its ID) is preserved across SSE updates.
func (s *Server) patchFragment(sse *datastar.ServerSentEventGenerator, page, id string, data any) {
	html, err := s.renderFragment(page, id, data)
	if err != nil {
		return
	}
	_ = sse.PatchElements(html,
		datastar.WithSelectorID(id),
		datastar.WithMode(datastar.ElementPatchModeInner),
	)
}

func (s *Server) sseDashboard(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	data := buildDashboardData(state)

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "dashboard", "health-indicators", state)
	s.patchFragment(sse, "dashboard", "dashboard-stats", data)
	s.patchFragment(sse, "dashboard", "top-deployments", data)
	s.patchFragment(sse, "dashboard", "supervisors-table", data)
	s.patchFragment(sse, "dashboard", "recent-activity", data)
}

func (s *Server) sseAssignments(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())

	var sig struct {
		AssignmentSearch  string `json:"assignmentSearch"`
		AssignmentSort    string `json:"assignmentSort"`
		AssignmentSortDir string `json:"assignmentSortDir"`
	}
	_ = datastar.ReadSignals(r, &sig)
	if sig.AssignmentSort == "" {
		sig.AssignmentSort = "deployment"
	}
	if sig.AssignmentSortDir == "" {
		sig.AssignmentSortDir = "asc"
	}

	sups := filterSupervisors(state.GetSupervisors(), sig.AssignmentSearch)
	sups = sortSupervisors(sups, sig.AssignmentSort, sig.AssignmentSortDir)

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "assignments", "assignments-table", &assignmentsData{
		State:       state,
		Supervisors: sups,
	})
}

func (s *Server) sseAssignment(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	state := s.state(r.Context())
	sup := findSupervisor(state, key)
	if sup == nil {
		return
	}

	data := &assignmentData{
		Key:            key,
		State:          state,
		Supervisor:     sup,
		ReadyInstances: countReady(sup.GetInstances()),
		StaleInstances: staleInstances(sup),
	}

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "assignment", "instances-table", data)
}

func (s *Server) sseControllers(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	rows, dist, imbalanced := buildControllerData(state)
	data := &controllersData{
		State:            state,
		ControllerRows:   rows,
		RingDistribution: dist,
		RingImbalanced:   imbalanced,
	}

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "controllers", "controllers-table", data)
	s.patchFragment(sse, "controllers", "ring-distribution", data)
}

func (s *Server) sseController(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	state := s.state(r.Context())

	var sups []*skipper.SupervisorState
	var ready, total int
	for _, sup := range state.GetSupervisors() {
		if sup.GetResponsibleControllerIp() == ip {
			sups = append(sups, sup)
			total += len(sup.GetInstances())
			ready += countReady(sup.GetInstances())
		}
	}

	data := &controllerData{
		IP:             ip,
		Found:          true,
		IsSelf:         ip == state.GetPodIp(),
		State:          state,
		Supervisors:    sups,
		ReadyInstances: ready,
		TotalInstances: total,
	}

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "controller", "controller-detail", data)
}

func (s *Server) sseRouters(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	rows := buildRouterRows(state)

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "routers", "routers-table", rows)
}

func (s *Server) sseRouter(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	state := s.state(r.Context())
	entries, totalInFlight := buildRouterEntries(state, ip)

	data := &routerData{
		IP:            ip,
		Found:         len(entries) > 0,
		Entries:       entries,
		TotalInFlight: totalInFlight,
	}

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "router", "router-detail", data)
}

func (s *Server) sseInstance(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	state := s.state(r.Context())

	data := &instanceData{Name: name}
	for _, sup := range state.GetSupervisors() {
		for _, inst := range sup.GetInstances() {
			if inst.GetName() == name {
				data.Found = true
				data.Instance = inst
				data.Supervisor = sup
				break
			}
		}
		if data.Found {
			break
		}
	}
	if !data.Found {
		return
	}

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "instance", "instance-detail", data)
}

func (s *Server) sseTenants(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())

	var sig struct {
		TenantSearch  string `json:"tenantSearch"`
		TenantSort    string `json:"tenantSort"`
		TenantSortDir string `json:"tenantSortDir"`
	}
	_ = datastar.ReadSignals(r, &sig)
	if sig.TenantSort == "" {
		sig.TenantSort = "tenant"
	}
	if sig.TenantSortDir == "" {
		sig.TenantSortDir = "asc"
	}

	rows := buildTenantRows(state)
	rows = filterTenantRows(rows, sig.TenantSearch)
	rows = sortTenantRows(rows, sig.TenantSort, sig.TenantSortDir)

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "tenants", "tenants-table", rows)
}

func (s *Server) sseTenant(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	state := s.state(r.Context())
	data := buildTenantData(state, tenant)
	if !data.Found {
		return
	}

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "tenant", "tenant-detail", data)
}

func (s *Server) sseDeployments(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	rows := buildDeploymentRows(state)

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "deployments", "deployments-table", rows)
}

func (s *Server) sseDeployment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	state := s.state(r.Context())

	var sups []*skipper.SupervisorState
	for _, sup := range state.GetSupervisors() {
		if sup.GetAssignment().GetDeployment() == name {
			sups = append(sups, sup)
		}
	}
	if len(sups) == 0 {
		return
	}

	filtered := &skipper.ClusterState{}
	filtered.SetSupervisors(sups)
	var row deploymentRow
	if rows := buildDeploymentRows(filtered); len(rows) > 0 {
		row = rows[0]
	}

	data := &deploymentData{
		Name:        name,
		Found:       true,
		Row:         row,
		Supervisors: sups,
	}

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "deployment", "deployment-detail", data)
}

func (s *Server) sseEvents(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())

	var assignmentFilter, sevFilter string
	type signals struct {
		EventAssignment string `json:"eventAssignment"`
		EventSeverity   string `json:"eventSeverity"`
	}
	var sig signals
	if err := datastar.ReadSignals(r, &sig); err == nil {
		assignmentFilter = sig.EventAssignment
		sevFilter = sig.EventSeverity
	}

	filtered := filterEvents(state.GetEvents(), assignmentFilter, sevFilter)

	sse := datastar.NewSSE(w, r)
	s.patchFragment(sse, "events", "events-table", filtered)
}
