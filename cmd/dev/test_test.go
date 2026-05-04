package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TestNewTestCmd_DispatchShape asserts the cobra tree wired by
// newTestCmd: a single leaf with RunE set, no children, and
// DisableFlagParsing so unknown flags forward to gotestsum.
func TestNewTestCmd_DispatchShape(t *testing.T) {
	t.Parallel()
	root := newTestCmd()
	assert.Assert(t, root.RunE != nil, "root must have RunE")
	assert.Assert(t, root.DisableFlagParsing, "root must disable flag parsing")
	assert.Equal(t, len(root.Commands()), 0, "test must have no subcommands")
}

// TestNewTestCmd_HelpFlagShowsCobraHelp asserts that `--help` and `-h`
// are intercepted and produce cobra's Short description rather than
// being forwarded to `go test`, which rejects `--help` as an unknown
// flag.
func TestNewTestCmd_HelpFlagShowsCobraHelp(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--help", "-h"} {
		root := newTestCmd()
		var buf bytes.Buffer
		root.SetOut(&buf)
		root.SetArgs([]string{flag})
		err := root.ExecuteContext(context.Background())
		assert.NilError(t, err, "%s should produce help, not run gotestsum", flag)
		out := buf.String()
		assert.Assert(t, strings.Contains(out, "Run Go tests"),
			"%s output should show cobra help:\n%s", flag, out)
	}
}

// TestRunGoTests_RejectsBareAll is the muscle-memory guard: any
// positional `all` token in the args reaches `go test all`, which
// matches every package in the import graph (including stdlib).
// runGoTests must refuse and surface a hint pointing at `dev test`.
func TestRunGoTests_RejectsBareAll(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"all"},
		{"all", "-v"},
		{"./pkg", "all"},
	}
	for _, args := range cases {
		err := runGoTests(context.Background(), args)
		assert.Assert(t, err != nil, "args %v: expected error", args)
		assert.ErrorContains(t, err, "dev test all")
	}
}

// TestRunGoTests_AllowsAllAsFlagValue ensures the guard scans only
// positional args, not flag values like `-run=all`.
func TestRunGoTests_AllowsAllAsFlagValue(t *testing.T) {
	t.Parallel()
	// Flag-shaped tokens that happen to contain `all` are not
	// positional and must not trigger the guard. We can verify the
	// classification without actually invoking gotestsum by checking
	// the guard logic in isolation.
	args := []string{"-run=all", "./internal/..."}
	assert.Assert(t, !hasBareAll(args), "flag value `all` must not trigger guard")
}

// TestHasPathArg_DetectsPositionalShapes covers the package-path
// detection rules:
//   - flag tokens like `-run=./pattern` MUST NOT count; otherwise the
//     default `./...` is dropped and `go test` runs against the
//     current directory only.
//   - any positional (non-flag) arg counts -- `./internal/...`, the
//     bare `internal/controller/...`, fully qualified import paths,
//     and gofmt-style `*_test.go` filenames all get respected.
func TestHasPathArg_DetectsPositionalShapes(t *testing.T) {
	t.Parallel()
	assert.Assert(t, !hasPathArg([]string{"-run=./pattern"}),
		"regex with `./` must not look like a package path")
	assert.Assert(t, !hasPathArg([]string{"-bench=foo./bar"}),
		"bench regex with `./` must not look like a package path")
	assert.Assert(t, !hasPathArg([]string{"-v", "-count=1"}),
		"flags only, no positional, must not count as a path")
	assert.Assert(t, hasPathArg([]string{"-run=Foo", "./internal/..."}),
		"./-prefixed path must be detected alongside flags")
	assert.Assert(t, hasPathArg([]string{"./..."}),
		"bare ./... must be detected")
	assert.Assert(t, hasPathArg([]string{"internal/controller/..."}),
		"bare module-relative `/...` path must be detected")
	assert.Assert(t, hasPathArg([]string{"github.com/gadget-inc/skipper/internal/controller"}),
		"fully qualified import path must be detected")
	assert.Assert(t, hasPathArg([]string{"foo_test.go"}),
		"gofmt-style filename must be detected")
}
