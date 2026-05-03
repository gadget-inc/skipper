package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newTestsCmd dispatches the project's test runners. The Go suite goes
// through gotestsum; docs uses pnpm filter; e2e is a chromedp Go suite.
func newTestsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tests <go|docs|e2e|all> [args...]",
		Short: "Run test suites",
	}

	goCmd := &cobra.Command{
		Use:                "go [flags] [./path/...]",
		Short:              "Run Go tests via gotestsum",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGoTests(cmd.Context(), args)
		},
	}
	docsCmd := &cobra.Command{
		Use:                "docs [args...]",
		Short:              "Run docs tests",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return exec.Run(cmd.Context(), "pnpm", append([]string{"--filter", "docs", "test"}, args...))
		},
	}
	e2eCmd := &cobra.Command{
		Use:                "e2e [args...]",
		Short:              "Run the chromedp e2e suite",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return exec.Run(cmd.Context(), "go", append([]string{"test", "./e2e/..."}, args...))
		},
	}
	allCmd := &cobra.Command{
		Use:   "all",
		Short: "Run go + docs (not e2e)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := runGoTests(ctx, nil); err != nil {
				return err
			}
			return exec.Run(ctx, "pnpm", []string{"--filter", "docs", "test"})
		},
	}

	cmd.AddCommand(goCmd, docsCmd, e2eCmd, allCmd)
	return cmd
}

// runGoTests builds the gotestsum invocation that scripts/tests.ts ran:
// piping through tee to tmp/logs/tests.log, defaulting to ./... when no
// path is supplied, and adding -count=1, -race, and a follow-up
// Allocations pass in CI.
func runGoTests(ctx context.Context, args []string) error {
	logsDir := filepath.Join(repoRoot(), "tmp", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	goTestFlags := slices.Clone(args)
	if !hasPathArg(goTestFlags) {
		goTestFlags = append([]string{"./..."}, goTestFlags...)
	}
	if isCI() {
		if !hasFlagPrefix(goTestFlags, "-count") {
			goTestFlags = append(goTestFlags, "-count=1")
		}
		if !hasFlagPrefix(goTestFlags, "-race") {
			goTestFlags = append(goTestFlags, "-race")
		}
	}

	gotestsumFlags := []string{"--format-hide-empty-pkg"}
	if !isCI() {
		gotestsumFlags = append(gotestsumFlags, "--hide-summary=skipped")
	}

	if err := runWithTee(ctx, logsDir, "tests.log", false, gotestsumFlags, goTestFlags); err != nil {
		return err
	}
	if isCI() {
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

func isCI() bool {
	v := os.Getenv("CI")
	return v == "1" || v == "true"
}

func repoRoot() string {
	if d := os.Getenv("WORKSPACE_DIR"); d != "" {
		return d
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
