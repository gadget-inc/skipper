package main

import (
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newLintCmd is the read-only counterpart to fmt: it runs kube-lint
// and golangci-lint over the project sources.
func newLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Check formatting, linters, and rendered manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return exec.RunAll(cmd.Context(), [][]string{
				{"go", "run", "./cmd/dev", "kube-lint"},
				{"go", "run", "./cmd/dev", "lint-docs"},
				{"golangci-lint", "run"},
			})
		},
	}
}
