// Command dev is the Go entry point for skipper's developer-experience
// commands. Subcommands live in sibling files (up.go, fmt.go, ...) and
// either invoke native Go logic or shell out via internal/dev/exec.
package main

import (
	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/spf13/cobra"
)

func main() { cmd.Main(newRoot()) }

func newRoot() *cobra.Command {
	return cmd.Build(cmd.Spec{
		Use:   "dev",
		Short: "skipper developer-experience commands",
		Sub: []*cobra.Command{
			newUpCmd(), newFmtCmd(), newLintCmd(), newCleanCmd(),
			newGenerateCmd(), newDocsCmd(), newTestsCmd(), newLogsCmd(),
			newBuildCmd(), newKubeLintCmd(), newLintDocsCmd(),
			newDeployCmd(), newFixtureCmd(), newProfileCmd(),
		},
	})
}
