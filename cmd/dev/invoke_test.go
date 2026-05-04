package main

import (
	"context"
	"sort"
	"testing"

	"gotest.tools/v3/assert"
)

// TestInvoke_UnknownPathReturnsError asserts dispatch surfaces cobra's
// "unknown command" error when the path doesn't resolve.
func TestInvoke_UnknownPathReturnsError(t *testing.T) {
	t.Parallel()
	err := invoke(context.Background(), "nonexistent")
	assert.Assert(t, err != nil, "expected unknown-command error")
	assert.ErrorContains(t, err, "unknown command")
}

// TestInvoke_LintDocsHelpSucceeds asserts a known top-level subcommand
// resolves and runs --help (which is a no-side-effect path) without
// error.
func TestInvoke_LintDocsHelpSucceeds(t *testing.T) {
	t.Parallel()
	err := invoke(context.Background(), "lint-docs", "--help")
	assert.NilError(t, err)
}

// TestInvoke_TestHelpSucceeds asserts the post-rename `test`
// subcommand resolves; this also exercises arg passthrough on a leaf
// that has DisableFlagParsing = true.
func TestInvoke_TestHelpSucceeds(t *testing.T) {
	t.Parallel()
	err := invoke(context.Background(), "test", "--help")
	assert.NilError(t, err)
}

// TestSubcommandNames_Depth1 pins the depth-1 set against the literal
// list of subcommands the cobra tree currently registers. This is the
// load-bearing assertion that proves the in-process snapshot is
// name-equivalent to the hand-maintained map at the moment of the cut.
func TestSubcommandNames_Depth1(t *testing.T) {
	t.Parallel()
	sub, _ := subcommandNames()

	want := []string{
		"build", "clean", "deploy", "docs", "fixture", "fmt", "generate",
		"kube-lint", "lint", "lint-docs", "logs", "profile", "test", "up",
	}
	got := keys(sub)
	sort.Strings(got)
	sort.Strings(want)
	assert.DeepEqual(t, got, want)
}

// TestSubcommandNames_Depth2 pins the depth-2 set, including the
// synthetic "dev" entry that supports `direnv exec . dev <...>` lines.
func TestSubcommandNames_Depth2(t *testing.T) {
	t.Parallel()
	_, subSub := subcommandNames()

	want := []string{
		"analyze", "build", "dev", "fetch", "load", "merge",
		"open", "request", "websocket",
	}
	got := keys(subSub)
	sort.Strings(got)
	sort.Strings(want)
	assert.DeepEqual(t, got, want)
}

// TestSubcommandNames_ExcludesBuiltins asserts cobra's auto-injected
// `help` and `completion` commands are filtered out.
func TestSubcommandNames_ExcludesBuiltins(t *testing.T) {
	t.Parallel()
	sub, subSub := subcommandNames()
	for _, builtin := range []string{"help", "completion"} {
		assert.Assert(t, !sub[builtin], "depth-1 must not contain %s", builtin)
		assert.Assert(t, !subSub[builtin], "depth-2 must not contain %s", builtin)
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
