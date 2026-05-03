package main

import (
	"context"
	"fmt"
	"io"

	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/doclint"
	"github.com/spf13/cobra"
)

// newLintDocsCmd registers the `dev lint-docs` subcommand: runs every
// registered doclint rule over the repo's tracked text files and
// prints any findings as `<file>:<line>: [<rule>] <token>` followed by
// an indented hint line. Exits non-zero when at least one finding is
// reported.
func newLintDocsCmd() *cobra.Command {
	return cmd.Build(cmd.Spec{
		Use:   "lint-docs",
		Short: "Lint repository documentation for stale toolchain words and other drift",
		RunE: func(c *cobra.Command, args []string) error {
			return runLintDocs(c.Context(), c.OutOrStdout(), doclint.RunOptions{})
		},
	})
}

func runLintDocs(ctx context.Context, out io.Writer, opts doclint.RunOptions) error {
	findings, err := doclint.Run(ctx, opts)
	if err != nil {
		return err
	}
	for _, f := range findings {
		fmt.Fprintf(out, "%s:%d: [%s] %s\n    %s\n", f.File, f.Line, f.Rule, f.Token, f.Hint)
	}
	if len(findings) > 0 {
		return fmt.Errorf("%d doclint finding(s)", len(findings))
	}
	return nil
}
