package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gadget-inc/skipper/internal/skipper"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// StateProvider returns the current cluster state.
type StateProvider func(ctx context.Context) *skipper.ClusterState

// Option configures the web server.
type Option func(*Server)

// WithTemplateDir enables dev mode: templates are re-parsed from the given
// directory on every request instead of using the embedded copies.
func WithTemplateDir(dir string) Option {
	return func(s *Server) { s.templateDir = dir }
}

// WithStaticDir enables dev mode for static assets: files are served from the
// given directory instead of using the embedded copies.
func WithStaticDir(dir string) Option {
	return func(s *Server) { s.staticDir = dir }
}

// Server serves the web UI.
type Server struct {
	state       StateProvider
	mux         *http.ServeMux
	pages       map[string]*template.Template
	templateDir string // when non-empty, re-parse from disk on each render
	staticDir   string // when non-empty, serve static files from disk
}

func newFuncMap() template.FuncMap {
	return template.FuncMap{
		"timeAgo":            timeAgo,
		"formatTimestamp":    formatTimestamp,
		"durationBetween":    durationBetween,
		"functionKey":        functionKey,
		"functionPath":       functionPath,
		"scaleReasonLabel":   scaleReasonLabel,
		"eventTypeLabel":     eventTypeLabel,
		"eventSeverityBadge": eventSeverityBadge,
		"statusBadge":        statusBadge,
		"instanceStateBadge": instanceStateBadge,
		"metricsString":      metricsString,
		"healthIssues":       computeHealthIssues,
		"activeNav":          activeNav,
		"pathEscape":         url.PathEscape,
		"isReady": func(inst *skipper.Instance) bool {
			return inst.HasReadyAt()
		},
		"sub":     func(a, b int) int { return a - b },
		"pct":     pct,
		"signals": signalsAttr,
	}
}

// parsePage creates a template set from layout.html + the given page template.
func parsePage(name string) *template.Template {
	return parsePageFS(templateFS, name)
}

func parsePageFS(fsys fs.FS, name string) *template.Template {
	return template.Must(
		template.New("").Funcs(newFuncMap()).ParseFS(fsys, "templates/layout.html", "templates/"+name+".html"),
	)
}

// New creates a new web server.
func New(state StateProvider, opts ...Option) *Server {
	s := &Server{
		state: state,
		mux:   http.NewServeMux(),
		pages: map[string]*template.Template{
			"dashboard":   parsePage("dashboard"),
			"functions":   parsePage("functions"),
			"function":    parsePage("function"),
			"controllers": parsePage("controllers"),
			"controller":  parsePage("controller"),
			"routers":     parsePage("routers"),
			"router":      parsePage("router"),
			"instance":    parsePage("instance"),
			"events":      parsePage("events"),
			"tenants":     parsePage("tenants"),
			"tenant":      parsePage("tenant"),
			"deployments": parsePage("deployments"),
			"deployment":  parsePage("deployment"),
			"config":      parsePage("config"),
		},
	}
	for _, o := range opts {
		o(s)
	}

	// Full-page handlers
	s.mux.HandleFunc("GET /", s.handleDashboard)
	s.mux.HandleFunc("GET /functions", s.handleFunctions)
	s.mux.HandleFunc("GET /functions/{key}", s.handleFunction)
	s.mux.HandleFunc("GET /controllers", s.handleControllers)
	s.mux.HandleFunc("GET /controllers/{ip}", s.handleController)
	s.mux.HandleFunc("GET /routers", s.handleRouters)
	s.mux.HandleFunc("GET /routers/{ip}", s.handleRouter)
	s.mux.HandleFunc("GET /instances/{name}", s.handleInstance)
	s.mux.HandleFunc("GET /tenants", s.handleTenants)
	s.mux.HandleFunc("GET /tenants/{tenant}", s.handleTenant)
	s.mux.HandleFunc("GET /deployments", s.handleDeployments)
	s.mux.HandleFunc("GET /deployments/{name}", s.handleDeployment)
	s.mux.HandleFunc("GET /events", s.handleEvents)
	s.mux.HandleFunc("GET /config", s.handleConfig)

	// SSE fragment handlers (Datastar)
	s.mux.HandleFunc("GET /sse/dashboard", s.sseDashboard)
	s.mux.HandleFunc("GET /sse/functions", s.sseFunctions)
	s.mux.HandleFunc("GET /sse/function/{key}", s.sseFunction)
	s.mux.HandleFunc("GET /sse/controllers", s.sseControllers)
	s.mux.HandleFunc("GET /sse/controller/{ip}", s.sseController)
	s.mux.HandleFunc("GET /sse/routers", s.sseRouters)
	s.mux.HandleFunc("GET /sse/router/{ip}", s.sseRouter)
	s.mux.HandleFunc("GET /sse/instance/{name}", s.sseInstance)
	s.mux.HandleFunc("GET /sse/tenants", s.sseTenants)
	s.mux.HandleFunc("GET /sse/tenant/{tenant}", s.sseTenant)
	s.mux.HandleFunc("GET /sse/deployments", s.sseDeployments)
	s.mux.HandleFunc("GET /sse/deployment/{name}", s.sseDeployment)
	s.mux.HandleFunc("GET /sse/events", s.sseEvents)

	// Static assets
	s.mux.Handle("GET /static/", s.staticHandler())

	// Health
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)

	return s
}

// getPage returns the template for a page. In dev mode it re-parses from disk
// on every call so edits are reflected without a restart.
func (s *Server) getPage(name string) (*template.Template, bool) {
	if s.templateDir != "" {
		t, err := template.New("").Funcs(newFuncMap()).ParseFS(
			os.DirFS(s.templateDir), "templates/layout.html", "templates/"+name+".html",
		)
		if err != nil {
			return nil, false
		}
		return t, true
	}
	t, ok := s.pages[name]
	return t, ok
}

// staticHandler returns the handler for /static/ assets. In dev mode it serves
// from the staticDir on disk; otherwise it uses the embedded filesystem.
func (s *Server) staticHandler() http.Handler {
	if s.staticDir != "" {
		return http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(s.staticDir, "static"))))
	}
	return http.FileServerFS(staticFS)
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.mux
}
