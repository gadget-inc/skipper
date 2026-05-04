package main

import (
	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newLintCmd is the read-only counterpart to fmt: it runs kube-lint,
// lint-docs, and golangci-lint over the project sources. Each step
// runs even if a prior one failed so developers see findings from
// every linter in a single pass; the first non-nil error sets the
// process exit code.
func newLintCmd() *cobra.Command {
	return cmd.Build(cmd.Spec{
		Use:   "lint",
		Short: "Check formatting, linters, and rendered manifests",
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			var firstErr error
			track := func(err error) {
				if err != nil && firstErr == nil {
					firstErr = err
				}
			}
			track(invoke(ctx, "kube-lint"))
			track(invoke(ctx, "lint-docs"))
			track(exec.Run(ctx, "golangci-lint", []string{"run"}))
			return firstErr
		},
	})
}
