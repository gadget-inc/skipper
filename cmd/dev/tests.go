package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/devenv"
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newTestsCmd dispatches the project's test runners. The Go suite goes
// through gotestsum; e2e is a chromedp Go suite.
func newTestsCmd() *cobra.Command {
	goCmd := cmd.Build(cmd.Spec{
		Use:   "go [flags] [./path/...]",
		Short: "Run Go tests via gotestsum",
		RunE: func(c *cobra.Command, args []string) error {
			return runGoTests(c.Context(), args)
		},
	})
	goCmd.DisableFlagParsing = true

	e2eCmd := cmd.Build(cmd.Spec{
		Use:   "e2e [args...]",
		Short: "Run the chromedp e2e suite",
		RunE: func(c *cobra.Command, args []string) error {
			return exec.Run(c.Context(), "go", append([]string{"test", "./e2e/..."}, args...))
		},
	})
	e2eCmd.DisableFlagParsing = true

	allCmd := cmd.Build(cmd.Spec{
		Use:   "all",
		Short: "Run all Go tests (not e2e)",
		RunE: func(c *cobra.Command, args []string) error {
			return runGoTests(c.Context(), nil)
		},
	})

	return cmd.Build(cmd.Spec{
		Use:   "tests <go|e2e|all> [args...]",
		Short: "Run test suites",
		Sub:   []*cobra.Command{goCmd, e2eCmd, allCmd},
	})
}

// runGoTests builds the gotestsum invocation: piping through tee to
// tmp/logs/tests.log, defaulting to ./... when no path is supplied,
// and adding -count=1, -race, and a follow-up Allocations pass in CI.
func runGoTests(ctx context.Context, args []string) error {
	logsDir := filepath.Join(devenv.RepoRoot(), "tmp", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	goTestFlags := slices.Clone(args)
	if !hasPathArg(goTestFlags) {
		goTestFlags = append([]string{"./..."}, goTestFlags...)
	}
	if devenv.IsCI() {
		if !hasFlagPrefix(goTestFlags, "-count") {
			goTestFlags = append(goTestFlags, "-count=1")
		}
		if !hasFlagPrefix(goTestFlags, "-race") {
			goTestFlags = append(goTestFlags, "-race")
		}
	}

	gotestsumFlags := []string{"--format-hide-empty-pkg"}
	if !devenv.IsCI() {
		gotestsumFlags = append(gotestsumFlags, "--hide-summary=skipped")
	}

	if err := runWithTee(ctx, logsDir, "tests.log", false, gotestsumFlags, goTestFlags); err != nil {
		return err
	}
	if devenv.IsCI() {
		// Allocations pass: race detector adds extra allocations, so run
		// it separately without -race.
		allocFlags := []string{"./...", "-run=Allocations", "-count=1"}
		if err := runWithTee(ctx, logsDir, "tests.log", true, gotestsumFlags, allocFlags); err != nil {
			return err
		}
	}
	return nil
}

// runWithTee invokes `gotestsum <gotestsumFlags> -- <goTestFlags>` with
// stdout duplicated to the named log file. append controls whether the
// log file is overwritten or appended to.
func runWithTee(ctx context.Context, logsDir, name string, appendLog bool, gotestsumFlags, goTestFlags []string) error {
	flags := os.O_WRONLY | os.O_CREATE
	if appendLog {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	logFile, err := os.OpenFile(filepath.Join(logsDir, name), flags, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	args := append([]string{}, gotestsumFlags...)
	args = append(args, "--")
	args = append(args, goTestFlags...)
	return exec.Run(ctx, "gotestsum", args, exec.Stdout(io.MultiWriter(os.Stdout, logFile)))
}

func hasPathArg(args []string) bool {
	for _, a := range args {
		if strings.Contains(a, "./") {
			return true
		}
	}
	return false
}

func hasFlagPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
