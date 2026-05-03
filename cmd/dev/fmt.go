package main

import (
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newFmtCmd runs the project's formatters: golangci-lint fmt for Go.
func newFmtCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fmt",
		Short: "Auto-fix formatting and lint issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			return exec.RunAll(cmd.Context(), [][]string{
				{"golangci-lint", "fmt"},
			})
		},
	}
}
