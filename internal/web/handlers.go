package web

import (
	"bytes"
	"net/http"
	"slices"
	"strings"

	"github.com/gadget-inc/skipper/internal/skipper"
)

type dashboardData struct {
	Title            string
	State            *skipper.ClusterState
	ReadyInstances   int
	PendingInstances int
	TotalInstances   int
	TotalTenants     int
	TotalDeployments int
	TopDeployments   []deploymentRow
	RecentEvents     []*skipper.Event
}

type assignmentsData struct {
	Title             string
	State             *skipper.ClusterState
	Supervisors       []*skipper.SupervisorState
	AssignmentSearch  string
	AssignmentSort    string
	AssignmentSortDir string
}

type assignmentData struct {
	Title          string
	Key            string
	State          *skipper.ClusterState
	Supervisor     *skipper.SupervisorState
	ReadyInstances int
	StaleInstances []*skipper.Instance
}

type controllerRow struct {
	IP             string
	IsSelf         bool
	Assignments    int
	ReadyInstances int
	TotalInstances int
}

type ringDistEntry struct {
	IP    string
	Count int
	Pct   int
}

type controllersData struct {
	Title            string
	State            *skipper.ClusterState
	ControllerRows   []controllerRow
	RingDistribution []ringDistEntry
	RingImbalanced   bool
}

type controllerData struct {
	Title          string
	IP             string
	Found          bool
	IsSelf         bool
	State          *skipper.ClusterState
	Supervisors    []*skipper.SupervisorState
	ReadyInstances int
	TotalInstances int
}

type routerRow struct {
	IP          string
	Assignments int
	InFlight    uint32
}

type routersData struct {
	Title      string
	RouterRows []routerRow
}

type routerEntry struct {
	Supervisor *skipper.SupervisorState
	Heartbeat  *skipper.HeartbeatState
}

type routerData struct {
	Title         string
	IP            string
	Found         bool
	Entries       []routerEntry
	TotalInFlight uint32
}

type instanceData struct {
	Title      string
	Name       string
	Found      bool
	Instance   *skipper.Instance
	Supervisor *skipper.SupervisorState
}

type eventsData struct {
	Title            string
	State            *skipper.ClusterState
	AssignmentFilter string
	SeverityFilter   string
	FilteredEvents   []*skipper.Event
}

type tenantRow struct {
	Tenant         string
	Assignments    int
	ReadyInstances int
	TotalInstances int
	Deployments    []string
}

type tenantsData struct {
	Title         string
	TenantRows    []tenantRow
	TenantSearch  string
	TenantSort    string
	TenantSortDir string
}

type tenantData struct {
	Title                string
	Tenant               string
	Found                bool
	Supervisors          []*skipper.SupervisorState
	ReadyInstances       int
	TotalInstances       int
	Deployments          []string
	DeploymentBreakdowns []tenantDeploymentBreakdown
}

type tenantDeploymentBreakdown struct {
	Deployment       string
	ReadyInstances   int
	PendingInstances int
	TotalInstances   int
}

type deploymentRow struct {
	Name             string
	Namespace        string
	Tenants          int
	ReadyInstances   int
	PendingInstances int
	TotalInstances   int
	ScaleMin         uint32
	ScaleMax         uint32
}

type deploymentsData struct {
	Title          string
	DeploymentRows []deploymentRow
}

type deploymentData struct {
	Title       string
	Name        string
	Found       bool
	Row         deploymentRow
	Supervisors []*skipper.SupervisorState
}

type configData struct {
	Title string
	State *skipper.ClusterState
}

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	t, ok := s.getPage(page)
	if !ok {
		http.Error(w, "unknown page: "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	state := s.state(r.Context())
	data := buildDashboardData(state)
	data.Title = "Dashboard"
	s.render(w, "dashboard", data)
}

// buildDashboardData populates the data structure rendered by the dashboard
// page and its SSE handler. Both call sites need identical instance counts,
// recent-events slicing, and top-deployments selection, so the construction
// lives here to keep them in lockstep.
func buildDashboardData(state *skipper.ClusterState) *dashboardData {
	ready, total := countInstances(state)
	events := state.GetEvents()
	if len(events) > 5 {
		events = events[:5]
	}

	tenantSet := make(map[string]struct{})
	deploySet := make(map[string]struct{})
	for _, sup := range state.GetSupervisors() {
		tenantSet[sup.GetAssignment().GetTenant()] = struct{}{}
		deploySet[sup.GetAssignment().GetDeployment()] = struct{}{}
	}

	topDeployments := slices.Clone(buildDeploymentRows(state))
	slices.SortFunc(topDeployments, func(a, b deploymentRow) int {
		if a.Tenants != b.Tenants {
			return b.Tenants - a.Tenants
		}
		return strings.Compare(a.Name, b.Name)
	})
	if len(topDeployments) > 5 {
		topDeployments = topDeployments[:5]
	}

	return &dashboardData{
		State:            state,
		ReadyInstances:   ready,
		PendingInstances: total - ready,
		TotalInstances:   total,
		TotalTenants:     len(tenantSet),
		TotalDeployments: len(deploySet),
		TopDeployments:   topDeployments,
		RecentEvents:     events,
	}
}

func (s *Server) handleAssignments(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	q := r.URL.Query()
	search := q.Get("search")
	sort := q.Get("sort")
	dir := q.Get("dir")
	if sort == "" {
		sort = "deployment"
	}
	if dir == "" {
		dir = "asc"
	}

	sups := filterSupervisors(state.GetSupervisors(), search)
	sups = sortSupervisors(sups, sort, dir)

	s.render(w, "assignments", &assignmentsData{
		Title:             "Assignments",
		State:             state,
		Supervisors:       sups,
		AssignmentSearch:  search,
		AssignmentSort:    sort,
		AssignmentSortDir: dir,
	})
}

func (s *Server) handleAssignment(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	state := s.state(r.Context())
	sup := findSupervisor(state, key)

	data := &assignmentData{
		Title:      "Assignment",
		Key:        key,
		State:      state,
		Supervisor: sup,
	}

	if sup != nil {
		data.ReadyInstances = countReady(sup.GetInstances())
		data.StaleInstances = staleInstances(sup)
	}

	s.render(w, "assignment", data)
}

func (s *Server) handleControllers(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	rows, dist, imbalanced := buildControllerData(state)
	s.render(w, "controllers", &controllersData{
		Title:            "Controllers",
		State:            state,
		ControllerRows:   rows,
		RingDistribution: dist,
		RingImbalanced:   imbalanced,
	})
}

func (s *Server) handleController(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	state := s.state(r.Context())

	found := slices.Contains(state.GetControllerIps(), ip)
	var sups []*skipper.SupervisorState
	var ready, total int
	if found {
		for _, sup := range state.GetSupervisors() {
			if sup.GetResponsibleControllerIp() == ip {
				sups = append(sups, sup)
				total += len(sup.GetInstances())
				ready += countReady(sup.GetInstances())
			}
		}
	}

	s.render(w, "controller", &controllerData{
		Title:          "Controller " + ip,
		IP:             ip,
		Found:          found,
		IsSelf:         ip == state.GetPodIp(),
		State:          state,
		Supervisors:    sups,
		ReadyInstances: ready,
		TotalInstances: total,
	})
}

func (s *Server) handleRouters(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	s.render(w, "routers", &routersData{
		Title:      "Routers",
		RouterRows: buildRouterRows(state),
	})
}

func (s *Server) handleRouter(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	state := s.state(r.Context())
	entries, totalInFlight := buildRouterEntries(state, ip)

	s.render(w, "router", &routerData{
		Title:         "Router " + ip,
		IP:            ip,
		Found:         len(entries) > 0,
		Entries:       entries,
		TotalInFlight: totalInFlight,
	})
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	state := s.state(r.Context())

	data := &instanceData{
		Title: "Instance " + name,
		Name:  name,
	}

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

	s.render(w, "instance", data)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	q := r.URL.Query()
	// New `?assignment=` wins over the legacy `?function=` when both
	// are present; either alone is honored. The cleanup plan drops the
	// legacy alias after the deprecation window.
	assignmentFilter := q.Get("assignment")
	if assignmentFilter == "" {
		assignmentFilter = q.Get("function")
	}
	sevFilter := q.Get("severity")

	s.render(w, "events", &eventsData{
		Title:            "Events",
		State:            state,
		AssignmentFilter: assignmentFilter,
		SeverityFilter:   sevFilter,
		FilteredEvents:   filterEvents(state.GetEvents(), assignmentFilter, sevFilter),
	})
}

func (s *Server) handleTenants(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	q := r.URL.Query()
	search := q.Get("search")
	sort := q.Get("sort")
	dir := q.Get("dir")
	if sort == "" {
		sort = "tenant"
	}
	if dir == "" {
		dir = "asc"
	}

	rows := buildTenantRows(state)
	rows = filterTenantRows(rows, search)
	rows = sortTenantRows(rows, sort, dir)

	s.render(w, "tenants", &tenantsData{
		Title:         "Tenants",
		TenantRows:    rows,
		TenantSearch:  search,
		TenantSort:    sort,
		TenantSortDir: dir,
	})
}

func (s *Server) handleTenant(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	state := s.state(r.Context())
	data := buildTenantData(state, tenant)
	data.Title = "Tenant " + tenant
	s.render(w, "tenant", data)
}

func (s *Server) handleDeployments(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	s.render(w, "deployments", &deploymentsData{
		Title:          "Deployments",
		DeploymentRows: buildDeploymentRows(state),
	})
}

func (s *Server) handleDeployment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	state := s.state(r.Context())

	var sups []*skipper.SupervisorState
	for _, sup := range state.GetSupervisors() {
		if sup.GetAssignment().GetDeployment() == name {
			sups = append(sups, sup)
		}
	}

	filtered := &skipper.ClusterState{}
	filtered.SetSupervisors(sups)
	var row deploymentRow
	if rows := buildDeploymentRows(filtered); len(rows) > 0 {
		row = rows[0]
	}

	s.render(w, "deployment", &deploymentData{
		Title:       "Deployment " + name,
		Name:        name,
		Found:       len(sups) > 0,
		Row:         row,
		Supervisors: sups,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	state := s.state(r.Context())
	s.render(w, "config", &configData{
		Title: "Config",
		State: state,
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// Helper functions

func countInstances(state *skipper.ClusterState) (ready, total int) {
	for _, sup := range state.GetSupervisors() {
		instances := sup.GetInstances()
		total += len(instances)
		ready += countReady(instances)
	}
	return
}

func countReady(instances []*skipper.Instance) int {
	count := 0
	for _, inst := range instances {
		if inst.HasReadyAt() {
			count++
		}
	}
	return count
}

func findSupervisor(state *skipper.ClusterState, key string) *skipper.SupervisorState {
	for _, sup := range state.GetSupervisors() {
		if assignmentKey(sup.GetAssignment()) == key {
			return sup
		}
	}
	return nil
}

func staleInstances(sup *skipper.SupervisorState) []*skipper.Instance {
	activeRS := sup.GetActiveReplicaSet()
	if activeRS == "" {
		return nil
	}
	var stale []*skipper.Instance
	for _, inst := range sup.GetInstances() {
		if inst.GetReplicaSet() != activeRS {
			stale = append(stale, inst)
		}
	}
	return stale
}

func buildControllerData(state *skipper.ClusterState) ([]controllerRow, []ringDistEntry, bool) {
	ips := state.GetControllerIps()
	rows := make([]controllerRow, 0, len(ips))

	counts := make(map[string]int, len(ips))
	for _, ip := range ips {
		counts[ip] = 0
	}

	for _, sup := range state.GetSupervisors() {
		ip := sup.GetResponsibleControllerIp()
		counts[ip]++
	}

	for _, ip := range ips {
		var ready, total int
		for _, sup := range state.GetSupervisors() {
			if sup.GetResponsibleControllerIp() == ip {
				total += len(sup.GetInstances())
				ready += countReady(sup.GetInstances())
			}
		}
		rows = append(rows, controllerRow{
			IP:             ip,
			IsSelf:         ip == state.GetPodIp(),
			Assignments:    counts[ip],
			ReadyInstances: ready,
			TotalInstances: total,
		})
	}

	// Ring distribution
	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	if maxCount == 0 {
		maxCount = 1
	}

	dist := make([]ringDistEntry, 0, len(ips))
	for _, ip := range ips {
		dist = append(dist, ringDistEntry{
			IP:    ip,
			Count: counts[ip],
			Pct:   counts[ip] * 100 / maxCount,
		})
	}

	// Check imbalance
	imbalanced := false
	if len(ips) > 0 {
		avg := float64(len(state.GetSupervisors())) / float64(len(ips))
		threshold := avg * 0.5
		for _, c := range counts {
			diff := float64(c) - avg
			if diff < 0 {
				diff = -diff
			}
			if avg > 0 && diff > threshold {
				imbalanced = true
				break
			}
		}
	}

	return rows, dist, imbalanced
}

func buildRouterRows(state *skipper.ClusterState) []routerRow {
	type routerStats struct {
		assignments int
		inFlight    uint32
	}
	m := make(map[string]*routerStats)

	for _, sup := range state.GetSupervisors() {
		for _, hb := range sup.GetRouterHeartbeats() {
			ip := hb.GetRouterIp()
			stats, ok := m[ip]
			if !ok {
				stats = &routerStats{}
				m[ip] = stats
			}
			stats.assignments++
			if hb.GetHeartbeat() != nil {
				stats.inFlight += hb.GetHeartbeat().GetInFlightRequests()
			}
		}
	}

	rows := make([]routerRow, 0, len(m))
	for ip, stats := range m {
		rows = append(rows, routerRow{
			IP:          ip,
			Assignments: stats.assignments,
			InFlight:    stats.inFlight,
		})
	}
	return rows
}

func buildRouterEntries(state *skipper.ClusterState, ip string) ([]routerEntry, uint32) {
	var entries []routerEntry
	var totalInFlight uint32

	for _, sup := range state.GetSupervisors() {
		for _, hb := range sup.GetRouterHeartbeats() {
			if hb.GetRouterIp() == ip {
				entries = append(entries, routerEntry{
					Supervisor: sup,
					Heartbeat:  hb,
				})
				if hb.GetHeartbeat() != nil {
					totalInFlight += hb.GetHeartbeat().GetInFlightRequests()
				}
			}
		}
	}

	return entries, totalInFlight
}

func buildTenantData(state *skipper.ClusterState, tenant string) *tenantData {
	var sups []*skipper.SupervisorState
	var ready, total int
	deploySet := make(map[string]struct{})
	breakdownMap := make(map[string]*tenantDeploymentBreakdown)

	for _, sup := range state.GetSupervisors() {
		if sup.GetAssignment().GetTenant() == tenant {
			sups = append(sups, sup)
			instances := sup.GetInstances()
			r := countReady(instances)
			t := len(instances)
			total += t
			ready += r
			deploy := sup.GetAssignment().GetDeployment()
			deploySet[deploy] = struct{}{}

			bd, ok := breakdownMap[deploy]
			if !ok {
				bd = &tenantDeploymentBreakdown{Deployment: deploy}
				breakdownMap[deploy] = bd
			}
			bd.TotalInstances += t
			bd.ReadyInstances += r
		}
	}

	deploys := make([]string, 0, len(deploySet))
	for d := range deploySet {
		deploys = append(deploys, d)
	}
	slices.Sort(deploys)

	breakdowns := make([]tenantDeploymentBreakdown, 0, len(breakdownMap))
	for _, bd := range breakdownMap {
		bd.PendingInstances = bd.TotalInstances - bd.ReadyInstances
		breakdowns = append(breakdowns, *bd)
	}
	slices.SortFunc(breakdowns, func(a, b tenantDeploymentBreakdown) int {
		return strings.Compare(a.Deployment, b.Deployment)
	})

	return &tenantData{
		Tenant:               tenant,
		Found:                len(sups) > 0,
		Supervisors:          sups,
		ReadyInstances:       ready,
		TotalInstances:       total,
		Deployments:          deploys,
		DeploymentBreakdowns: breakdowns,
	}
}

func buildTenantRows(state *skipper.ClusterState) []tenantRow {
	type tenantStats struct {
		assignments    int
		readyInstances int
		totalInstances int
		deployments    map[string]struct{}
	}
	m := make(map[string]*tenantStats)

	for _, sup := range state.GetSupervisors() {
		tenant := sup.GetAssignment().GetTenant()
		stats, ok := m[tenant]
		if !ok {
			stats = &tenantStats{deployments: make(map[string]struct{})}
			m[tenant] = stats
		}
		stats.assignments++
		stats.totalInstances += len(sup.GetInstances())
		stats.readyInstances += countReady(sup.GetInstances())
		stats.deployments[sup.GetAssignment().GetDeployment()] = struct{}{}
	}

	rows := make([]tenantRow, 0, len(m))
	for tenant, stats := range m {
		deploys := make([]string, 0, len(stats.deployments))
		for d := range stats.deployments {
			deploys = append(deploys, d)
		}
		slices.Sort(deploys)
		rows = append(rows, tenantRow{
			Tenant:         tenant,
			Assignments:    stats.assignments,
			ReadyInstances: stats.readyInstances,
			TotalInstances: stats.totalInstances,
			Deployments:    deploys,
		})
	}
	slices.SortFunc(rows, func(a, b tenantRow) int {
		return strings.Compare(a.Tenant, b.Tenant)
	})
	return rows
}

func buildDeploymentRows(state *skipper.ClusterState) []deploymentRow {
	type deploymentStats struct {
		namespace      string
		tenants        map[string]struct{}
		readyInstances int
		totalInstances int
		scaleMin       uint32
		scaleMax       uint32
	}
	m := make(map[string]*deploymentStats)

	for _, sup := range state.GetSupervisors() {
		fn := sup.GetAssignment()
		name := fn.GetDeployment()
		stats, ok := m[name]
		if !ok {
			stats = &deploymentStats{
				namespace: fn.GetNamespace(),
				tenants:   make(map[string]struct{}),
			}
			m[name] = stats
		}
		stats.tenants[fn.GetTenant()] = struct{}{}
		stats.totalInstances += len(sup.GetInstances())
		stats.readyInstances += countReady(sup.GetInstances())
		if scale := fn.GetScale(); scale != nil {
			stats.scaleMin += scale.GetMinInstances()
			stats.scaleMax += scale.GetMaxInstances()
		}
	}

	rows := make([]deploymentRow, 0, len(m))
	for name, stats := range m {
		rows = append(rows, deploymentRow{
			Name:             name,
			Namespace:        stats.namespace,
			Tenants:          len(stats.tenants),
			ReadyInstances:   stats.readyInstances,
			PendingInstances: stats.totalInstances - stats.readyInstances,
			TotalInstances:   stats.totalInstances,
			ScaleMin:         stats.scaleMin,
			ScaleMax:         stats.scaleMax,
		})
	}
	slices.SortFunc(rows, func(a, b deploymentRow) int {
		return strings.Compare(a.Name, b.Name)
	})
	return rows
}

func filterEvents(events []*skipper.Event, fnFilter, sevFilter string) []*skipper.Event {
	if fnFilter == "" && (sevFilter == "" || sevFilter == "all") {
		return events
	}

	var filtered []*skipper.Event
	for _, e := range events {
		if fnFilter != "" {
			if e.GetAssignment() == nil {
				continue
			}
			if !strings.Contains(strings.ToLower(assignmentKey(e.GetAssignment())), strings.ToLower(fnFilter)) {
				continue
			}
		}

		if sevFilter != "" && sevFilter != "all" {
			switch sevFilter {
			case "1":
				if e.GetSeverity() != skipper.EventSeverity_EVENT_SEVERITY_INFO {
					continue
				}
			case "2":
				if e.GetSeverity() != skipper.EventSeverity_EVENT_SEVERITY_WARN {
					continue
				}
			}
		}

		filtered = append(filtered, e)
	}
	return filtered
}

func filterSupervisors(sups []*skipper.SupervisorState, search string) []*skipper.SupervisorState {
	if search == "" {
		return sups
	}
	lower := strings.ToLower(search)
	var filtered []*skipper.SupervisorState
	for _, sup := range sups {
		fn := sup.GetAssignment()
		if strings.Contains(strings.ToLower(fn.GetDeployment()), lower) ||
			strings.Contains(strings.ToLower(fn.GetNamespace()), lower) ||
			strings.Contains(strings.ToLower(fn.GetTenant()), lower) {
			filtered = append(filtered, sup)
		}
	}
	return filtered
}

func sortSupervisors(sups []*skipper.SupervisorState, col, dir string) []*skipper.SupervisorState {
	sorted := slices.Clone(sups)
	slices.SortFunc(sorted, func(a, b *skipper.SupervisorState) int {
		var cmp int
		switch col {
		case "namespace":
			cmp = strings.Compare(a.GetAssignment().GetNamespace(), b.GetAssignment().GetNamespace())
		case "tenant":
			cmp = strings.Compare(a.GetAssignment().GetTenant(), b.GetAssignment().GetTenant())
		case "instances":
			cmp = len(a.GetInstances()) - len(b.GetInstances())
		default: // "deployment"
			cmp = strings.Compare(a.GetAssignment().GetDeployment(), b.GetAssignment().GetDeployment())
		}
		if dir == "desc" {
			return -cmp
		}
		return cmp
	})
	return sorted
}

func filterTenantRows(rows []tenantRow, search string) []tenantRow {
	if search == "" {
		return rows
	}
	lower := strings.ToLower(search)
	var filtered []tenantRow
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Tenant), lower) {
			filtered = append(filtered, row)
			continue
		}
		for _, d := range row.Deployments {
			if strings.Contains(strings.ToLower(d), lower) {
				filtered = append(filtered, row)
				break
			}
		}
	}
	return filtered
}

func sortTenantRows(rows []tenantRow, col, dir string) []tenantRow {
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b tenantRow) int {
		var cmp int
		switch col {
		case "assignments", "functions":
			cmp = a.Assignments - b.Assignments
		case "instances":
			cmp = a.ReadyInstances - b.ReadyInstances
		case "deployments":
			cmp = len(a.Deployments) - len(b.Deployments)
		default: // "tenant"
			cmp = strings.Compare(a.Tenant, b.Tenant)
		}
		if dir == "desc" {
			return -cmp
		}
		return cmp
	})
	return sorted
}
