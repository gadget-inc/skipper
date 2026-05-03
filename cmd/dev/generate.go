package main

import (
	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newGenerateCmd regenerates the protobuf Go code via buf.
func newGenerateCmd() *cobra.Command {
	return cmd.Build(cmd.Spec{
		Use:   "generate",
		Short: "Regenerate generated code (protobuf via buf)",
		RunE: func(c *cobra.Command, args []string) error {
			return exec.Run(c.Context(), "buf", []string{"generate"})
		},
	})
}
