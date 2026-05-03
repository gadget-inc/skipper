package main

import (
	"github.com/spf13/cobra"
)

// newLintCmd is the read-only counterpart to fmt: it runs the project's
// suite of checkers (kube-lint, golangci-lint, oxfmt --check, oxlint).
func newLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Check formatting, linters, and rendered manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAll(cmd.Context(), [][]string{
				{"go", "run", "./cmd/dev", "kube-lint"},
				{"golangci-lint", "run"},
				{"pnpm", "exec", "oxfmt", "--check", "."},
				{"pnpm", "exec", "oxlint", "--type-aware", "--type-check", "--max-warnings=0", "."},
			})
		},
	}
}
