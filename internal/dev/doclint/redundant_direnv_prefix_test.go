package doclint

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestRedundantDirenvPrefix_FlagsLineInShellBlock(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"```bash",
		"direnv exec . dev up",
		"```",
	}, "\n")

	got := scanRedundantDirenvPrefix(File{Path: "docs/content/contributing/foo.md", Content: []byte(src)}, nil)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Line, 2)
}

func TestRedundantDirenvPrefix_AllowlistSuppressesCanonicalParagraph(t *testing.T) {
	t.Parallel()

	allow := []redundantDirenvPrefixAllowEntry{
		{File: "CLAUDE.md", Pattern: `single source of truth for that qualification`},
	}

	src := strings.Join([]string{
		"prose paragraph mentioning `direnv exec .` -- this paragraph is the single source of truth for that qualification",
	}, "\n")

	got := scanRedundantDirenvPrefix(File{Path: "CLAUDE.md", Content: []byte(src)}, allow)
	// The line is in prose (not a shell fence) AND it's in the
	// allowlist. Either alone would suppress; both together is fine.
	assert.Equal(t, len(got), 0)
}

func TestRedundantDirenvPrefix_NonShellFenceNotScanned(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"```go",
		"// direnv exec . dev up",
		"fmt.Println(\"x\")",
		"```",
	}, "\n")

	got := scanRedundantDirenvPrefix(File{Path: "CLAUDE.md", Content: []byte(src)}, nil)
	assert.Equal(t, len(got), 0)
}

func TestRedundantDirenvPrefix_ShellCommentNotFlagged(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"```bash",
		"# direnv exec . dev up",
		"```",
	}, "\n")

	got := scanRedundantDirenvPrefix(File{Path: "CLAUDE.md", Content: []byte(src)}, nil)
	assert.Equal(t, len(got), 0)
}

func TestRedundantDirenvPrefix_DirenvWithoutTrailingDotNotFlagged(t *testing.T) {
	t.Parallel()

	// `direnv allow` is a real direnv subcommand (not the
	// `direnv exec . ...` alias for running through the shell).
	src := strings.Join([]string{
		"```bash",
		"direnv allow",
		"```",
	}, "\n")

	got := scanRedundantDirenvPrefix(File{Path: "CLAUDE.md", Content: []byte(src)}, nil)
	assert.Equal(t, len(got), 0)
}
