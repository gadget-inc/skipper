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

// newTestCmd runs the Go suite via gotestsum, forwarding all args
// (including the chromedp suite under internal/web). DisableFlagParsing
// passes unknown flags (`-short`, `-run`, `-bench`, ...) verbatim to
// gotestsum; the RunE intercepts `--help`/`-h` so they show cobra's
// Short description rather than reaching `go test`, which rejects
// `--help` as an unknown flag.
func newTestCmd() *cobra.Command {
	root := cmd.Build(cmd.Spec{
		Use:   "test [flags] [./path/...]",
		Short: "Run Go tests via gotestsum",
		RunE: func(c *cobra.Command, args []string) error {
			if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
				return c.Help()
			}
			return runGoTests(c.Context(), args)
		},
	})
	root.DisableFlagParsing = true
	return root
}

// runGoTests builds the gotestsum invocation: piping through tee to
// tmp/logs/tests.log, defaulting to ./... when no path is supplied,
// and adding -count=1, -race, and a follow-up Allocations pass in CI.
func runGoTests(ctx context.Context, args []string) error {
	if hasBareAll(args) {
		return fmt.Errorf("dev test all is not a subcommand; run `dev test` (no args) to run every Go package")
	}

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

	gotestsumArgs := append([]string{}, gotestsumFlags...)
	gotestsumArgs = append(gotestsumArgs, "--")
	gotestsumArgs = append(gotestsumArgs, goTestFlags...)
	return exec.Run(ctx, "gotestsum", gotestsumArgs, exec.Stdout(io.MultiWriter(os.Stdout, logFile)))
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

// hasBareAll reports whether any positional (non-flag) arg is the
// literal `all`. Catches the muscle-memory shapes `dev test all`,
// `dev test all -v`, and `dev test ./pkg all` -- any of which would
// otherwise reach `go test ./... all` (Go's meta-package).
func hasBareAll(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if a == "all" {
			return true
		}
	}
	return false
}
