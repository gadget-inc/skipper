package main

import (
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newUpCmd is a passthrough shim around scripts/dev.ts. Phase 8 swaps
// the body for a native Go orchestrator (errgroup-based process group,
// fsnotify watcher, in-process PASETO setup); cobra deliberately does
// no flag parsing here so the existing `--only=` and any other flags
// continue to reach scripts/dev.ts unchanged during the transition.
func newUpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "up [flags...]",
		Short:              "Run controller, router, and the docs site locally",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return exec.Run(cmd.Context(), "scripts/dev.ts", args)
		},
	}
	return cmd
}
