// Command dev is the Go entry point for skipper's developer-experience
// commands. The Nix-provided `dev` shell alias resolves to `go run
// cmd/dev`; subcommands live in sibling files (up.go, fmt.go, lint.go,
// ...) and either invoke native Go logic or shell out via
// internal/dev/exec to scripts that have not been ported yet.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	root := newRoot()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "dev",
		Short:         "skipper developer-experience commands",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(newUpCmd())
	root.AddCommand(newFmtCmd())
	root.AddCommand(newLintCmd())
	root.AddCommand(newCleanCmd())
	root.AddCommand(newGenerateCmd())
	root.AddCommand(newDocsCmd())
	root.AddCommand(newTestsCmd())
	root.AddCommand(newLogsCmd())
	root.AddCommand(newBuildCmd())
	root.AddCommand(newKubeLintCmd())
	root.AddCommand(newDeployCmd())
	root.AddCommand(newFixtureCmd())
	return root
}
