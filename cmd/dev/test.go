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

// goTestValueFlags lists the `go test` and built-in `testing` flags
// whose value is the next argv element when written in the space form
// (`-run TestFoo`). The `-flag=value` form is self-contained and
// handled by the equals check in flagConsumesNext directly. Boolean
// flags (`-v`, `-race`, `-cover`, `-short`, `-failfast`, ...) and the
// argv-terminator `-args` deliberately do NOT appear -- the next
// token after them is positional, not a flag value.
var goTestValueFlags = map[string]bool{
	"-asmflags":             true,
	"-bench":                true,
	"-benchtime":            true,
	"-blockprofile":         true,
	"-blockprofilerate":     true,
	"-count":                true,
	"-coverpkg":             true,
	"-coverprofile":         true,
	"-cpu":                  true,
	"-cpuprofile":           true,
	"-fuzz":                 true,
	"-fuzzminimizetime":     true,
	"-fuzztime":             true,
	"-gccgoflags":           true,
	"-gcflags":              true,
	"-ldflags":              true,
	"-list":                 true,
	"-memprofile":           true,
	"-memprofilerate":       true,
	"-mutexprofile":         true,
	"-mutexprofilefraction": true,
	"-outputdir":            true,
	"-parallel":             true,
	"-run":                  true,
	"-shuffle":              true,
	"-skip":                 true,
	"-tags":                 true,
	"-testlogfile":          true,
	"-timeout":              true,
	"-trace":                true,
}

// flagConsumesNext reports whether arg is a `go test` flag whose
// value is supplied as the next argv element (the space form). The
// `--flag` and `-flag` forms map to the same flag for Go's flag
// package, so both are accepted. The `-flag=value` form is
// self-contained -- it is NOT a value-consuming token under this
// rule.
func flagConsumesNext(arg string) bool {
	if !strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
		return false
	}
	return goTestValueFlags["-"+strings.TrimLeft(arg, "-")]
}

// hasPathArg reports whether the user supplied any positional (non-
// flag) target -- a relative package path (`./internal/...`), a bare
// module-relative path (`internal/controller/...`), a fully-qualified
// import path, or a gofmt-style `*_test.go` filename. When no
// positional is present, the runner prepends `./...`. The bare token
// `all` is rejected upstream by `hasBareAll`, so any other positional
// is treated as a deliberate scope. Space-separated flag values
// (`-run TestFoo`) are skipped via goTestValueFlags so the value
// token is not misread as a path.
func hasPathArg(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if flagConsumesNext(a) && i+1 < len(args) {
				i++
			}
			continue
		}
		return true
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
// otherwise reach `go test ./... all` (Go's meta-package). Space-
// separated flag values (`-run all`) are skipped so the value is not
// mistaken for a positional.
func hasBareAll(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if flagConsumesNext(a) && i+1 < len(args) {
				i++
			}
			continue
		}
		if a == "all" {
			return true
		}
	}
	return false
}
