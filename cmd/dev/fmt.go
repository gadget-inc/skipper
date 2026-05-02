package main

import (
	"context"

	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newFmtCmd runs the formatters that scripts/fmt.ts ran: golangci-lint
// fmt for Go, then oxfmt and oxlint --fix for the JS surface that still
// exists during the migration.
func newFmtCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fmt",
		Short: "Auto-fix formatting and lint issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAll(cmd.Context(), [][]string{
				{"golangci-lint", "fmt"},
				{"pnpm", "exec", "oxfmt", "--write", "."},
				{"pnpm", "exec", "oxlint", "--type-aware", "--type-check", "--fix", "."},
			})
		},
	}
}

// runAll runs each step in order. Like scripts/_utils.ts $nothrow, a
// non-zero exit from one step does not skip the rest -- developers want
// to see findings from every formatter / linter, not just the first.
// The first non-nil error is returned to set the process exit code.
func runAll(ctx context.Context, steps [][]string) error {
	var firstErr error
	for _, step := range steps {
		if err := exec.Run(ctx, step[0], step[1:]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
