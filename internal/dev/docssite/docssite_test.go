package docssite_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
	"gotest.tools/v3/poll"

	"github.com/coder/websocket"
	"github.com/gadget-inc/skipper/internal/dev/docssite"
)

// fixtureSidebar is the four-group structure the criterion expects to
// match the current Starlight layout.
var fixtureSidebar = docssite.Sidebar{
	Intro: &docssite.SidebarItem{Label: "Introduction", Link: "/"},
	Groups: []docssite.SidebarGroup{
		{
			Label: "Guides",
			Items: []docssite.SidebarItem{
				{Label: "Routing", Link: "/guides/routing/"},
				{Label: "Scaling", Link: "/guides/scaling/"},
			},
		},
		{
			Label: "Architecture",
			Items: []docssite.SidebarItem{
				{Label: "Overview", Link: "/architecture/overview/"},
			},
		},
		{
			Label: "Reference",
			Items: []docssite.SidebarItem{
				{Label: "API", Link: "/reference/api/"},
			},
		},
		{
			Label: "Contributing",
			Items: []docssite.SidebarItem{
				{Label: "Workflow", Link: "/contributing/workflow/"},
			},
		},
	},
}

// TestBuild_RendersGoldenFixtures runs Build over a checked-in source
// tree and golden-compares each rendered HTML page. Updates with
// `go test ./internal/dev/docssite/... -update`.
func TestBuild_RendersGoldenFixtures(t *testing.T) {
	srcDir := filepath.Join("testdata", "src")
	outDir := t.TempDir()

	err := docssite.Build(srcDir, outDir, docssite.BuildOptions{
		BasePath:  "/skipper",
		SiteTitle: "Skipper",
		Sidebar:   fixtureSidebar,
	})
	assert.NilError(t, err)

	cases := []struct {
		name        string
		path        string
		goldenFile  string
		mustContain []string
	}{
		{
			name:       "frontmatter and inline link",
			path:       "index.html",
			goldenFile: "index.html.golden",
			mustContain: []string{
				"<title>Welcome - Skipper</title>",
				`href="/skipper/guides/routing/"`,
				// External link is unchanged
				`href="https://github.com"`,
			},
		},
		{
			name:       "code highlighting",
			path:       filepath.Join("snippet", "index.html"),
			goldenFile: "snippet.html.golden",
			mustContain: []string{
				// chroma styled output
				`style=`,
				// must contain our keyword
				"main",
			},
		},
		{
			name:       "scaling calculator widget inclusion",
			path:       filepath.Join("scaling", "index.html"),
			goldenFile: "scaling.html.golden",
			mustContain: []string{
				`scaling-calculator`,
				`calc-instances`,
			},
		},
		{
			name:       "admonitions: default, tip, caution",
			path:       filepath.Join("admonitions", "index.html"),
			goldenFile: "admonitions.html.golden",
			mustContain: []string{
				"aside-note",
				"aside-tip",
				"aside-caution",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(outDir, tc.path))
			assert.NilError(t, err)
			out := string(data)
			for _, sub := range tc.mustContain {
				assert.Assert(t, strings.Contains(out, sub),
					"output missing %q\n--- output ---\n%s", sub, out)
			}
			golden.Assert(t, out, tc.goldenFile)
		})
	}
}

// TestBuild_NoBasePath confirms an empty BasePath leaves absolute
// internal links untouched.
func TestBuild_NoBasePath(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: Bare
---

[Routing](/guides/routing/)
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "index.md"), []byte(page), 0o644))

	err := docssite.Build(srcDir, outDir, docssite.BuildOptions{
		SiteTitle: "Skipper",
	})
	assert.NilError(t, err)

	out, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(out), `href="/guides/routing/"`),
		"empty BasePath should leave absolute internal links untouched: %s", out)
	assert.Assert(t, !strings.Contains(string(out), `href="/skipper/guides/routing/"`))
}

// TestBuild_BasePathRewrite confirms BasePath = "/skipper" rewrites
// matching absolute internal links.
func TestBuild_BasePathRewrite(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: Linked
---

Internal: [Routing](/guides/routing/).
External: [GitHub](https://github.com).
Already-prefixed: [Scaling](/skipper/guides/scaling/).
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "index.md"), []byte(page), 0o644))

	err := docssite.Build(srcDir, outDir, docssite.BuildOptions{
		BasePath:  "/skipper",
		SiteTitle: "Skipper",
	})
	assert.NilError(t, err)

	out, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	assert.NilError(t, err)
	got := string(out)
	assert.Assert(t, strings.Contains(got, `href="/skipper/guides/routing/"`), got)
	assert.Assert(t, strings.Contains(got, `href="https://github.com"`), got)
	// Already-prefixed link must not double-prefix.
	assert.Assert(t, !strings.Contains(got, `/skipper/skipper/`), got)
	assert.Assert(t, strings.Contains(got, `href="/skipper/guides/scaling/"`), got)
}

// TestBuild_OutputLayout confirms the Build pass writes index.md to
// `<out>/index.html` and other pages to `<out>/<stem>/index.html`.
func TestBuild_OutputLayout(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()

	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "index.md"),
		[]byte("---\ntitle: Home\n---\n\nHi."), 0o644))
	assert.NilError(t, os.MkdirAll(filepath.Join(srcDir, "guides"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "guides", "routing.md"),
		[]byte("---\ntitle: Routing\n---\n\nBody."), 0o644))

	err := docssite.Build(srcDir, outDir, docssite.BuildOptions{})
	assert.NilError(t, err)

	for _, p := range []string{
		"index.html",
		filepath.Join("guides", "routing", "index.html"),
	} {
		_, err := os.Stat(filepath.Join(outDir, p))
		assert.NilError(t, err, "expected %s to exist", p)
	}
}

// TestBuild_CopiesStaticAssets confirms non-markdown files in srcDir
// reach outDir at the same relative path.
func TestBuild_CopiesStaticAssets(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()

	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "index.md"),
		[]byte("---\ntitle: Home\n---\n\nHi."), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "style.css"),
		[]byte("body { color: red; }"), 0o644))
	assert.NilError(t, os.MkdirAll(filepath.Join(srcDir, "assets"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "assets", "logo.svg"),
		[]byte("<svg/>"), 0o644))

	err := docssite.Build(srcDir, outDir, docssite.BuildOptions{})
	assert.NilError(t, err)

	css, err := os.ReadFile(filepath.Join(outDir, "style.css"))
	assert.NilError(t, err)
	assert.Equal(t, string(css), "body { color: red; }")

	svg, err := os.ReadFile(filepath.Join(outDir, "assets", "logo.svg"))
	assert.NilError(t, err)
	assert.Equal(t, string(svg), "<svg/>")
}

// TestServe_StaticAsset confirms the dev server serves a non-markdown
// file from srcDir at its relative path, honouring BasePath.
func TestServe_StaticAsset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live-server integration test in short mode")
	}

	srcDir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "index.md"),
		[]byte("---\ntitle: Home\n---\n\nHi."), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "style.css"),
		[]byte("body { color: red; }"), 0o644))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_ = docssite.Serve(ctx, srcDir, addr, docssite.ServeOptions{
			BuildOptions: docssite.BuildOptions{BasePath: "/skipper"},
		})
	}()

	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		c, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err != nil {
			return poll.Continue("waiting for server on %s", addr)
		}
		c.Close()
		return poll.Success()
	}, poll.WithTimeout(3*time.Second), poll.WithDelay(50*time.Millisecond))

	resp, err := http.Get("http://" + addr + "/skipper/style.css")
	assert.NilError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, resp.StatusCode, 200)
	body, err := io.ReadAll(resp.Body)
	assert.NilError(t, err)
	assert.Equal(t, string(body), "body { color: red; }")
}

// TestServe_HotReload starts the dev server, connects to /__reload,
// modifies a markdown file, and asserts a reload event arrives.
func TestServe_HotReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live-server integration test in short mode")
	}

	srcDir := t.TempDir()
	mdPath := filepath.Join(srcDir, "index.md")
	assert.NilError(t, os.WriteFile(mdPath, []byte("---\ntitle: Reload\n---\n\nv1"), 0o644))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() {
		done <- docssite.Serve(ctx, srcDir, addr, docssite.ServeOptions{})
	}()

	// Wait for the server to start accepting connections.
	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		c, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err != nil {
			return poll.Continue("waiting for server on %s", addr)
		}
		c.Close()
		return poll.Success()
	}, poll.WithTimeout(3*time.Second), poll.WithDelay(50*time.Millisecond))

	dialCtx, dialCancel := context.WithTimeout(ctx, 2*time.Second)
	defer dialCancel()
	conn, _, err := websocket.Dial(dialCtx, "ws://"+addr+"/__reload", nil)
	assert.NilError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Modify the file; expect a reload message within a short window.
	time.Sleep(50 * time.Millisecond)
	assert.NilError(t, os.WriteFile(mdPath, []byte("---\ntitle: Reload\n---\n\nv2"), 0o644))

	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	_, msg, err := conn.Read(readCtx)
	assert.NilError(t, err)
	assert.Equal(t, string(msg), "reload")
}

// TestServe_PageRender confirms the dev server returns rendered HTML
// for an existing markdown page and 404s otherwise.
func TestServe_PageRender(t *testing.T) {
	srcDir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "index.md"),
		[]byte("---\ntitle: Home\n---\n\nHi."), 0o644))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_ = docssite.Serve(ctx, srcDir, addr, docssite.ServeOptions{})
	}()

	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		c, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err != nil {
			return poll.Continue("waiting for server")
		}
		c.Close()
		return poll.Success()
	}, poll.WithTimeout(3*time.Second), poll.WithDelay(50*time.Millisecond))

	resp, err := http.Get("http://" + addr + "/")
	assert.NilError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, resp.StatusCode, http.StatusOK)

	resp404, err := http.Get("http://" + addr + "/missing/")
	assert.NilError(t, err)
	defer resp404.Body.Close()
	assert.Equal(t, resp404.StatusCode, http.StatusNotFound)
}

// TestRenderSidebar_GoldenFourGroup checks the sidebar HTML for the
// four-group fixture matches the golden file.
func TestRenderSidebar_GoldenFourGroup(t *testing.T) {
	// Render the same fixture sidebar the build test uses by routing
	// it through Build over a tiny markdown source. This avoids
	// exposing a separate sidebar render API to consumers.
	srcDir := t.TempDir()
	outDir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "index.md"),
		[]byte("---\ntitle: Sidebar Probe\n---\n\nHi."), 0o644))
	err := docssite.Build(srcDir, outDir, docssite.BuildOptions{
		BasePath: "/skipper",
		Sidebar:  fixtureSidebar,
	})
	assert.NilError(t, err)
	html, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	assert.NilError(t, err)
	// Extract the <nav class="sidebar"...> block so the golden focuses
	// on the sidebar markup, not the surrounding chrome.
	out := string(html)
	start := strings.Index(out, `<nav class="sidebar"`)
	end := strings.Index(out, `</nav>`)
	if start == -1 || end == -1 {
		t.Fatalf("could not locate <nav class=sidebar>: %s", out)
	}
	sidebar := out[start : end+len(`</nav>`)]
	golden.Assert(t, sidebar, "sidebar.html.golden")
}

// silence unused import linter warnings in the unlikely case some
// helpers above stop being used during refactors.
var _ = httptest.NewRecorder
