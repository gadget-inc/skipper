package doclint

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestFrontmatterScope_FlagsZeroMatchGlob(t *testing.T) {
	t.Parallel()

	doc := File{
		Path:    ".claude/rules/foo.md",
		Content: []byte("---\npaths:\n  - \"internal/**/*.go\"\n  - \"docs/**/*.astro\"\n---\n# foo\n"),
	}
	tracked := []File{
		{Path: "internal/skipper/types.go"},
	}

	got, err := scanFrontmatterScope(doc, tracked)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "docs/**/*.astro")
	assert.Equal(t, got[0].Line, 4)
}

func TestFrontmatterScope_NoFlagWhenAllGlobsMatch(t *testing.T) {
	t.Parallel()

	doc := File{
		Path:    "rule.md",
		Content: []byte("---\npaths:\n  - \"internal/**/*.go\"\n---\n"),
	}
	tracked := []File{{Path: "internal/skipper/types.go"}}

	got, err := scanFrontmatterScope(doc, tracked)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

func TestFrontmatterScope_DoubleStarRecursiveExpansion(t *testing.T) {
	t.Parallel()

	doc := File{
		Path:    "rule.md",
		Content: []byte("---\npaths:\n  - \"docs/**\"\n---\n"),
	}
	tracked := []File{{Path: "docs/content/guides/scaling.md"}}

	got, err := scanFrontmatterScope(doc, tracked)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

func TestFrontmatterScope_NoFrontmatter(t *testing.T) {
	t.Parallel()

	doc := File{
		Path:    "plain.md",
		Content: []byte("# title\n\nbody without frontmatter\n"),
	}
	got, err := scanFrontmatterScope(doc, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

func TestFrontmatterScope_FrontmatterWithoutPathsKey(t *testing.T) {
	t.Parallel()

	doc := File{
		Path:    "doc.md",
		Content: []byte("---\ntitle: hello\n---\n# body\n"),
	}
	got, err := scanFrontmatterScope(doc, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

func TestFrontmatterScope_NonListPathsValueIgnored(t *testing.T) {
	t.Parallel()

	// `paths: docs/whatever` is a scalar, not a list -- the rule
	// considers only the YAML list shape and reports nothing for the
	// scalar form.
	doc := File{
		Path:    "doc.md",
		Content: []byte("---\npaths: \"docs/whatever\"\n---\n"),
	}
	got, err := scanFrontmatterScope(doc, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

func TestFrontmatterScope_CommentedOutGlobNotParsed(t *testing.T) {
	t.Parallel()

	doc := File{
		Path: "doc.md",
		Content: []byte(strings.Join([]string{
			"---",
			"paths:",
			"  # - \"docs/**/*.astro\"",
			"  - \"internal/**/*.go\"",
			"---",
		}, "\n")),
	}
	tracked := []File{{Path: "internal/skipper/types.go"}}

	got, err := scanFrontmatterScope(doc, tracked)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0, "commented-out lines must not be treated as globs")
}
