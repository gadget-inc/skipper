package web

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gadget-inc/skipper/internal/skipper"
	"gotest.tools/v3/assert"
)

func TestStaticCSS_Embedded(t *testing.T) {
	t.Parallel()
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/static/css/output.css", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, w.Code, 200)
	assert.Assert(t, strings.Contains(w.Header().Get("Content-Type"), "text/css"))
	assert.Assert(t, w.Body.Len() > 0, "body should be non-empty")
}

func TestStaticCSS_NotFound(t *testing.T) {
	t.Parallel()
	srv := testServer()

	req := httptest.NewRequest(http.MethodGet, "/static/nonexistent.css", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, w.Code, 404)
}

func TestLayoutCSS_HrefMatchesEmbeddedFile(t *testing.T) {
	t.Parallel()

	// Read the raw layout template from the embedded filesystem.
	raw, err := fs.ReadFile(templateFS, "templates/layout.html")
	assert.NilError(t, err)

	// Extract the CSS href from the <link> tag.
	re := regexp.MustCompile(`<link[^>]+href="([^"]+\.css)"`)
	matches := re.FindSubmatch(raw)
	assert.Assert(t, matches != nil, "layout.html should contain a CSS <link> tag")

	href := string(matches[1]) // e.g. "/static/css/output.css"

	// Strip the leading "/static/" prefix to get the path relative to the
	// staticFS embed root, then re-add the "static/" directory prefix that
	// the embed.FS uses internally.
	relPath := strings.TrimPrefix(href, "/static/")
	fsPath := "static/" + relPath

	_, err = fs.Stat(staticFS, fsPath)
	assert.NilError(t, err, "CSS file referenced in layout.html must exist in embedded staticFS: %s", fsPath)
}

func TestStaticCSS_DevMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	staticDir := filepath.Join(dir, "static")
	assert.NilError(t, os.MkdirAll(staticDir, 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(staticDir, "output.css"), []byte("body { color: red; }"), 0o644))

	srv := New(
		func(ctx context.Context) *skipper.ClusterState { return testState() },
		WithStaticDir(dir),
	)

	req := httptest.NewRequest(http.MethodGet, "/static/output.css", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, w.Code, 200)
	assert.Assert(t, strings.Contains(w.Body.String(), "body { color: red; }"))
}
