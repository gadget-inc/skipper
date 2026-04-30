package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gadget-inc/skipper/internal/skipper"
	"gotest.tools/v3/assert"
)

func TestDevModeUsesTemplatesFromDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tplDir := filepath.Join(dir, "templates")
	assert.NilError(t, os.MkdirAll(tplDir, 0o755))

	layout := `{{define "layout"}}<!doctype html><html><body>{{template "content" .}}</body></html>{{end}}`
	page := `{{define "content"}}<h1>disk-dashboard</h1>{{end}}`
	assert.NilError(t, os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(tplDir, "dashboard.html"), []byte(page), 0o644))

	srv := New(
		func(ctx context.Context) *skipper.ClusterState { return testState() },
		WithTemplateDir(dir),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, w.Code, 200)
	assert.Assert(t, strings.Contains(w.Body.String(), "disk-dashboard"))
}

func TestDevModeLiveReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tplDir := filepath.Join(dir, "templates")
	assert.NilError(t, os.MkdirAll(tplDir, 0o755))

	layout := `{{define "layout"}}<!doctype html><html><body>{{template "content" .}}</body></html>{{end}}`
	page := `{{define "content"}}<h1>version-1</h1>{{end}}`
	assert.NilError(t, os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(tplDir, "dashboard.html"), []byte(page), 0o644))

	srv := New(
		func(ctx context.Context) *skipper.ClusterState { return testState() },
		WithTemplateDir(dir),
	)

	// First render
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Assert(t, strings.Contains(w.Body.String(), "version-1"))

	// Modify template on disk
	page2 := `{{define "content"}}<h1>version-2</h1>{{end}}`
	assert.NilError(t, os.WriteFile(filepath.Join(tplDir, "dashboard.html"), []byte(page2), 0o644))

	// Second render picks up the change
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Assert(t, strings.Contains(w.Body.String(), "version-2"))
}
