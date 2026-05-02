package main

import (
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newGenerateCmd regenerates the protobuf Go code via buf, mirroring
// scripts/generate.ts.
func newGenerateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Regenerate generated code (protobuf via buf)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return exec.Run(cmd.Context(), "buf", []string{"generate"})
		},
	}
}
