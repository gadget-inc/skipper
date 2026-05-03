package main

import (
	"github.com/gadget-inc/skipper/internal/dev/profileops"
	"github.com/spf13/cobra"
)

// newProfileCmd builds the `dev profile <fetch|open|merge|analyze>`
// command tree. Each subcommand owns its own flag set and delegates
// to internal/dev/profileops.
func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile <fetch|open|merge|analyze>",
		Short: "Fetch, open, merge, and analyze pprof profiles",
	}
	cmd.AddCommand(newProfileFetchCmd())
	cmd.AddCommand(newProfileOpenCmd())
	cmd.AddCommand(newProfileMergeCmd())
	cmd.AddCommand(newProfileAnalyzeCmd())
	return cmd
}

func newProfileFetchCmd() *cobra.Command {
	opts := profileops.FetchOptions{}

	cmd := &cobra.Command{
		Use:   "fetch [pod] [flags]",
		Short: "Fetch a pprof profile from a running pod",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				opts.Pod = args[0]
			}
			return profileops.Fetch(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Type, "type", "t", "heap", "Profile type: heap or cpu")
	cmd.Flags().StringVarP(&opts.Component, "component", "c", "controller", "Component to profile (controller, router)")
	cmd.Flags().BoolVarP(&opts.Production, "production", "p", false, "Fetch from production")
	cmd.Flags().IntVarP(&opts.Seconds, "seconds", "s", 30, "CPU profile duration in seconds")
	cmd.Flags().BoolVarP(&opts.Web, "web", "w", false, "Open the web UI for the profile")
	cmd.Flags().BoolVarP(&opts.Diff, "diff", "d", false, "Compare the profile to previously fetched profiles")
	cmd.Flags().BoolVar(&opts.Spread, "spread", false, "Fetch one profile from every pod")

	return cmd
}

func newProfileOpenCmd() *cobra.Command {
	opts := profileops.OpenOptions{Web: true}

	cmd := &cobra.Command{
		Use:   "open <file> [flags]",
		Short: "Open a saved profile in go tool pprof",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Profile = args[0]
			return profileops.Open(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.Web, "web", "w", true, "Open the web UI for the profile")
	cmd.Flags().BoolVarP(&opts.Diff, "diff", "d", false, "Compare the profile to previous profiles")

	return cmd
}

func newProfileMergeCmd() *cobra.Command {
	opts := profileops.MergeOptions{Component: "all"}

	cmd := &cobra.Command{
		Use:   "merge [flags]",
		Short: "Merge CPU profiles into default.pgo files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return profileops.Merge(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Component, "component", "c", "all", "Component to merge (controller, router, all)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be merged without writing files")
	cmd.Flags().BoolVar(&opts.Clean, "clean", false, "Delete source profiles after successful merge")

	return cmd
}

func newProfileAnalyzeCmd() *cobra.Command {
	opts := profileops.AnalyzeOptions{Mode: "top"}

	cmd := &cobra.Command{
		Use:   "analyze [file] [flags]",
		Short: "Analyze a pprof profile and print text output",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				opts.Profile = args[0]
			}
			return profileops.Analyze(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Mode, "mode", "top", "Analysis mode: top, peek, source, diff")
	cmd.Flags().StringVarP(&opts.Function, "function", "f", "", "Target function regex (required for peek/source)")
	cmd.Flags().StringVarP(&opts.Component, "component", "c", "controller", "Component for --pgo (controller, router)")
	cmd.Flags().IntVarP(&opts.NodeCount, "nodecount", "n", 20, "Number of functions to show")
	cmd.Flags().BoolVar(&opts.Cumulative, "cum", false, "Sort by cumulative instead of flat")
	cmd.Flags().BoolVar(&opts.PGO, "pgo", false, "Analyze the committed default.pgo file")
	cmd.Flags().StringVar(&opts.DiffBase, "diff-base", "", "Base profile for diff mode")

	return cmd
}
