package main

import (
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newDocsCmd is a passthrough shim around `pnpm --filter docs <cmd>`
// for `dev`, `build`, and `preview`. Phase 11 swaps the body for direct
// internal/dev/docssite.Serve / .Build calls; until then cobra disables
// flag parsing so the existing arg surface reaches Astro unchanged.
func newDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "docs [dev|build|preview] [flags...]",
		Short:              "Run the docs dev server, build, or preview",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sub := "dev"
			rest := args
			if len(args) > 0 {
				sub, rest = args[0], args[1:]
			}
			return exec.Run(cmd.Context(), "pnpm",
				append([]string{"--filter", "docs", sub}, rest...),
			)
		},
	}
}
