package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gadget-inc/skipper/internal/dev/devenv"
	"github.com/gadget-inc/skipper/internal/dev/docssite"
	"github.com/spf13/cobra"
)

// docsBasePath matches the production deploy URL prefix
// (`https://gadget-inc.github.io/skipper/...`) so absolute internal
// links resolve to the same paths in dev and on GitHub Pages.
const docsBasePath = "/skipper"

// docsSiteTitle appears as the page <title> suffix.
const docsSiteTitle = "Skipper"

// docsContentDir is the markdown source tree, relative to the
// repository root.
const docsContentDir = "docs/content"

// docsSidebar is the four-group navigation rendered alongside every
// docs page (Guides, Architecture, Reference, Contributing).
var docsSidebar = docssite.Sidebar{
	Intro: &docssite.SidebarItem{Label: "Introduction", Link: "/"},
	Groups: []docssite.SidebarGroup{
		{
			Label: "Guides",
			Items: []docssite.SidebarItem{
				{Label: "Deploying Functions", Link: "/guides/deploying-functions/"},
				{Label: "Routing and Proxying", Link: "/guides/routing/"},
				{Label: "Scaling", Link: "/guides/scaling/"},
				{Label: "Heartbeats and Lifecycle", Link: "/guides/heartbeats-and-lifecycle/"},
				{Label: "Observability", Link: "/guides/observability/"},
			},
		},
		{
			Label: "Architecture",
			Items: []docssite.SidebarItem{
				{Label: "Overview", Link: "/architecture/overview/"},
				{Label: "Hashing", Link: "/architecture/hashing/"},
				{Label: "Controller Internals", Link: "/architecture/controller-internals/"},
				{Label: "Router Internals", Link: "/architecture/router-internals/"},
				{Label: "Infrastructure", Link: "/architecture/infrastructure/"},
			},
		},
		{
			Label: "Reference",
			Items: []docssite.SidebarItem{
				{Label: "Configuration", Link: "/reference/configuration/"},
				{Label: "API", Link: "/reference/api/"},
			},
		},
		{
			Label: "Contributing",
			Items: []docssite.SidebarItem{
				{Label: "Getting Started", Link: "/contributing/getting-started/"},
				{Label: "Building and Deploying", Link: "/contributing/building-and-deploying/"},
				{Label: "Testing and Linting", Link: "/contributing/testing/"},
				{Label: "Web UI", Link: "/contributing/web-ui/"},
				{Label: "Watching Logs", Link: "/contributing/watching-logs/"},
				{Label: "Profiling and PGO", Link: "/contributing/profiling/"},
				{Label: "Contribution Workflow", Link: "/contributing/workflow/"},
			},
		},
	},
}

// newDocsCmd dispatches `dev docs` straight into the SSG: bare or
// `dev docs dev` runs the live-reload server; `dev docs build`
// renders the static site to `docs/dist`.
func newDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs [dev|build]",
		Short: "Run the docs dev server or build the static site",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDocsServe(cmd.Context(), ":4321")
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "dev",
			Short: "Run the docs dev server (default)",
			RunE: func(cmd *cobra.Command, args []string) error {
				return runDocsServe(cmd.Context(), ":4321")
			},
		},
		&cobra.Command{
			Use:   "build",
			Short: "Build the static docs site",
			RunE: func(cmd *cobra.Command, args []string) error {
				return runDocsBuild()
			},
		},
	)
	return cmd
}

// docsBuildOptions returns the BuildOptions wired with the project's
// sidebar, base path, and site title.
func docsBuildOptions() docssite.BuildOptions {
	return docssite.BuildOptions{
		BasePath:  docsBasePath,
		Sidebar:   docsSidebar,
		SiteTitle: docsSiteTitle,
	}
}

// runDocsServe boots the dev server bound to addr.
func runDocsServe(ctx context.Context, addr string) error {
	src := filepath.Join(devenv.RepoRoot(), docsContentDir)
	opts := docssite.ServeOptions{BuildOptions: docsBuildOptions()}
	fmt.Printf("docs dev server listening on http://127.0.0.1%s%s/\n", addr, docsBasePath)
	return docssite.Serve(ctx, src, addr, opts)
}

// runDocsBuild renders the static site to the package default output
// directory, the path .github/workflows/docs.yaml uploads.
func runDocsBuild() error {
	src := filepath.Join(devenv.RepoRoot(), docsContentDir)
	out := filepath.Join(devenv.RepoRoot(), docssite.DefaultBuildOutputDir)
	if err := docssite.Build(src, out, docsBuildOptions()); err != nil {
		return fmt.Errorf("build docs: %w", err)
	}
	fmt.Printf("docs built → %s\n", out)
	return nil
}
