// Package docssite is the hand-rolled static-site generator that backs
// `dev docs` and `dev docs build`. The Build entry point walks a
// markdown source tree and writes static HTML to an output directory;
// Serve hosts the rendered tree over HTTP, watches the source tree
// with fsnotify, and broadcasts a WebSocket reload signal to connected
// browsers when a file changes.
//
// The public surface is intentionally small: two functions plus the
// matching options structs. Internal pipeline stages (frontmatter
// parsing, shortcode expansion, base-path link rewriting, page-layout
// templating, sidebar rendering) live in unexported helpers in this
// package.
package docssite

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SidebarGroup is one heading group in the sidebar -- a labeled
// section containing one or more page links.
type SidebarGroup struct {
	Label string
	Items []SidebarItem
}

// SidebarItem is a single page link in the sidebar.
type SidebarItem struct {
	Label string
	// Link is the absolute path under BasePath. Use leading "/"; the
	// base path is prepended at render time.
	Link string
}

// Sidebar describes the documentation site's left-hand navigation.
// The first entry is the optional "Introduction" link rendered above
// the grouped items.
type Sidebar struct {
	// Intro, when set, is rendered above the grouped sections; the
	// link points at the site root.
	Intro *SidebarItem
	// Groups are the labelled sections.
	Groups []SidebarGroup
}

// BuildOptions configures a Build run.
type BuildOptions struct {
	// BasePath, when set (e.g. "/skipper"), is prepended to every
	// generated URL and to absolute internal markdown links matching
	// /<group>/<page>/.
	BasePath string
	// Sidebar describes the left-hand navigation. When zero the page
	// renders without a sidebar.
	Sidebar Sidebar
	// SiteTitle appears in the page <title>.
	SiteTitle string
}

// ServeOptions configures the dev server. ServeOptions embeds
// BuildOptions so callers configure the renderer once.
type ServeOptions struct {
	BuildOptions
}

// DefaultBuildOutputDir is where Build writes when callers do not
// override the destination. .github/workflows/docs.yaml references
// docs/dist as the publish directory, so the deploy pipeline picks
// up the rendered tree without configuration changes.
const DefaultBuildOutputDir = "docs/dist"

//go:embed templates/page.html
var pageTemplateSource string

//go:embed widgets/scaling_calculator.html
var scalingCalculatorPartial string

// pageTemplate is the parsed page layout. Exported lazily by the
// renderer.
var pageTemplate = template.Must(template.New("page").Parse(pageTemplateSource))

// Build walks srcDir for .md files, renders each to HTML, and writes
// the result under outDir preserving the source-tree layout. Any
// non-markdown file in the source tree is copied verbatim to the same
// relative path in the output, so static assets (CSS, images, the
// favicon) ship alongside the rendered pages. An empty outDir falls
// back to DefaultBuildOutputDir resolved relative to the current
// working directory.
func Build(srcDir, outDir string, opts BuildOptions) error {
	if srcDir == "" {
		return errors.New("docssite.Build: srcDir is required")
	}
	if outDir == "" {
		outDir = DefaultBuildOutputDir
	}
	if _, err := os.Stat(srcDir); err != nil {
		return fmt.Errorf("source directory: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	r := newRenderer(opts)

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return copyStatic(path, filepath.Join(outDir, rel))
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := r.RenderPage(rel, raw, false)
		if err != nil {
			return fmt.Errorf("render %s: %w", rel, err)
		}
		dst := filepath.Join(outDir, htmlOutputPath(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, out, 0o644); err != nil {
			return err
		}
		return nil
	})
}

// copyStatic copies src to dst byte-for-byte, creating dst's parent
// directory tree as needed.
func copyStatic(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// Serve is the dev-mode entry point. It blocks until ctx is cancelled.
// Browsers receive a websocket reload signal whenever a .md file in
// srcDir changes.
func Serve(ctx context.Context, srcDir, addr string, opts ServeOptions) error {
	if srcDir == "" {
		return errors.New("docssite.Serve: srcDir is required")
	}
	if addr == "" {
		addr = ":4321"
	}
	srv := newDevServer(srcDir, opts)
	return srv.run(ctx, addr)
}

// htmlOutputPath maps a source-relative .md path to its output path.
// `index.md` becomes `index.html`; `<page>.md` becomes
// `<page>/index.html` so URLs can use trailing-slash form without
// extension.
func htmlOutputPath(rel string) string {
	dir := filepath.Dir(rel)
	base := filepath.Base(rel)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "index" {
		return filepath.Join(dir, "index.html")
	}
	return filepath.Join(dir, stem, "index.html")
}
