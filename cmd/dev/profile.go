package main

import (
	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/profileops"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newProfileCmd builds the `dev profile <fetch|open|merge|analyze>`
// command tree. Each subcommand owns its own flag set and delegates
// to internal/dev/profileops.
func newProfileCmd() *cobra.Command {
	return cmd.Build(cmd.Spec{
		Use:   "profile <fetch|open|merge|analyze>",
		Short: "Fetch, open, merge, and analyze pprof profiles",
		Sub: []*cobra.Command{
			newProfileFetchCmd(),
			newProfileOpenCmd(),
			newProfileMergeCmd(),
			newProfileAnalyzeCmd(),
		},
	})
}

func newProfileFetchCmd() *cobra.Command {
	opts := profileops.FetchOptions{}

	return cmd.Build(cmd.Spec{
		Use:   "fetch [pod] [flags]",
		Short: "Fetch a pprof profile from a running pod",
		Args:  cobra.MaximumNArgs(1),
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVarP(&opts.Type, "type", "t", "heap", "Profile type: heap or cpu")
			fs.StringVarP(&opts.Component, "component", "c", "controller", "Component to profile (controller, router)")
			fs.BoolVarP(&opts.Production, "production", "p", false, "Fetch from production")
			fs.IntVarP(&opts.Seconds, "seconds", "s", 30, "CPU profile duration in seconds")
			fs.BoolVarP(&opts.Web, "web", "w", false, "Open the web UI for the profile")
			fs.BoolVarP(&opts.Diff, "diff", "d", false, "Compare the profile to previously fetched profiles")
			fs.BoolVar(&opts.Spread, "spread", false, "Fetch one profile from every pod")
		},
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 1 {
				opts.Pod = args[0]
			}
			return profileops.Fetch(c.Context(), opts)
		},
	})
}

func newProfileOpenCmd() *cobra.Command {
	opts := profileops.OpenOptions{Web: true}

	return cmd.Build(cmd.Spec{
		Use:   "open <file> [flags]",
		Short: "Open a saved profile in go tool pprof",
		Args:  cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet) {
			fs.BoolVarP(&opts.Web, "web", "w", true, "Open the web UI for the profile")
			fs.BoolVarP(&opts.Diff, "diff", "d", false, "Compare the profile to previous profiles")
		},
		RunE: func(c *cobra.Command, args []string) error {
			opts.Profile = args[0]
			return profileops.Open(c.Context(), opts)
		},
	})
}

func newProfileMergeCmd() *cobra.Command {
	opts := profileops.MergeOptions{Component: "all"}

	return cmd.Build(cmd.Spec{
		Use:   "merge [flags]",
		Short: "Merge CPU profiles into default.pgo files",
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVarP(&opts.Component, "component", "c", "all", "Component to merge (controller, router, all)")
			fs.BoolVar(&opts.DryRun, "dry-run", false, "Show what would be merged without writing files")
			fs.BoolVar(&opts.Clean, "clean", false, "Delete source profiles after successful merge")
		},
		RunE: func(c *cobra.Command, args []string) error {
			return profileops.Merge(c.Context(), opts)
		},
	})
}

func newProfileAnalyzeCmd() *cobra.Command {
	opts := profileops.AnalyzeOptions{Mode: "top"}

	return cmd.Build(cmd.Spec{
		Use:   "analyze [file] [flags]",
		Short: "Analyze a pprof profile and print text output",
		Args:  cobra.MaximumNArgs(1),
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&opts.Mode, "mode", "top", "Analysis mode: top, peek, source, diff")
			fs.StringVarP(&opts.Function, "function", "f", "", "Target function regex (required for peek/source)")
			fs.StringVarP(&opts.Component, "component", "c", "controller", "Component for --pgo (controller, router)")
			fs.IntVarP(&opts.NodeCount, "nodecount", "n", 20, "Number of functions to show")
			fs.BoolVar(&opts.Cumulative, "cum", false, "Sort by cumulative instead of flat")
			fs.BoolVar(&opts.PGO, "pgo", false, "Analyze the committed default.pgo file")
			fs.StringVar(&opts.DiffBase, "diff-base", "", "Base profile for diff mode")
		},
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 1 {
				opts.Profile = args[0]
			}
			return profileops.Analyze(c.Context(), opts)
		},
	})
}
