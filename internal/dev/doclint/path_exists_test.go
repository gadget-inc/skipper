package doclint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestPathExists_FlagsBrokenPathButNotResolvingPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWrite(t, root, "real.go", "package real\n")

	doc := File{
		Path:    "doc.md",
		Content: []byte("- See `real.go`.\n- See `nope/missing.go`.\n"),
	}

	got, err := scanPathExists(context.Background(), root, doc, gitCheckIgnoreFn(map[string]bool{}))
	assert.NilError(t, err)
	assert.Equal(t, len(got), 1, "only the missing path should be flagged")
	assert.Equal(t, got[0].Token, "nope/missing.go")
	assert.Equal(t, got[0].Line, 2)
}

func TestPathExists_GitignoreSuppression(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	doc := File{Path: "doc.md", Content: []byte("- See `tmp/build/output.bin`.\n")}

	gitignored := map[string]bool{"tmp/build/output.bin": true}

	got, err := scanPathExists(context.Background(), root, doc, gitCheckIgnoreFn(gitignored))
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0, "gitignored path should not be flagged")
}

func TestPathExists_MarkdownLinkTargetShape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWrite(t, root, "guides/scaling.md", "# guide\n")

	doc := File{
		Path:    "doc.md",
		Content: []byte("Read [the guide](guides/scaling.md) and [missing](no/such.md)\n"),
	}

	got, err := scanPathExists(context.Background(), root, doc, gitCheckIgnoreFn(map[string]bool{}))
	assert.NilError(t, err)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "no/such.md")
}

func TestPathExists_SkipsUrlAnchorTemplate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	doc := File{
		Path: "doc.md",
		Content: []byte(
			"- [http link](https://example.com/x)\n" +
				"- [ftp link](ftp://example.com/x)\n" +
				"- [anchor](#section)\n" +
				"- [mailto](mailto:a@b)\n" +
				"- See `${VAR}/path` placeholder.\n",
		),
	}

	got, err := scanPathExists(context.Background(), root, doc, gitCheckIgnoreFn(map[string]bool{}))
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0, "URLs, anchors, and templates must be skipped")
}

func TestPathExists_SkipsBareFilenamesAndAbsolutePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	doc := File{
		Path: "doc.md",
		Content: []byte(
			"- Run `pnpm` (no slash, skipped).\n" +
				"- See `/absolute/no/such` (skipped, leading slash).\n" +
				"- See `@org/pkg` (skipped, leading @).\n",
		),
	}

	got, err := scanPathExists(context.Background(), root, doc, gitCheckIgnoreFn(map[string]bool{}))
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

func TestPathExists_DocRelativeResolution(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWrite(t, root, "docs/guides/sibling.md", "# sibling\n")

	doc := File{
		Path: "docs/guides/index.md",
		// "sibling.md" alone is bare (no slash), skipped.
		// "./sibling.md" has a slash and resolves doc-relative.
		Content: []byte("See [sib](./sibling.md).\n"),
	}

	got, err := scanPathExists(context.Background(), root, doc, gitCheckIgnoreFn(map[string]bool{}))
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0, "doc-relative ./sibling.md should resolve")
}

func TestPathExists_SkipsTokensWithoutExtensionInLastSegment(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	doc := File{
		Path: "doc.md",
		// All three look like paths but neither has a file extension in
		// the last segment, so the rule treats them as labels/imports
		// rather than file paths.
		Content: []byte(
			"- K8s label `skipper/deployment` -- skipped\n" +
				"- Go import `gotest.tools/v3/assert` -- skipped\n" +
				"- MIME type `text/event-stream` -- skipped\n",
		),
	}

	got, err := scanPathExists(context.Background(), root, doc, gitCheckIgnoreFn(map[string]bool{}))
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0, "non-path-shaped tokens must be skipped")
}

func TestPathExists_FencedCodeBlocksSkipped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	doc := File{
		Path:    "doc.md",
		Content: []byte("```\nthis is `path/in/fence.go` -- ignored\n```\n"),
	}

	got, err := scanPathExists(context.Background(), root, doc, gitCheckIgnoreFn(map[string]bool{}))
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0, "fenced-block path mentions are skipped")
}

func mustWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	assert.NilError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	assert.NilError(t, os.WriteFile(abs, []byte(content), 0o644))
}

// gitCheckIgnoreFn returns a stub that classifies the given paths as
// gitignored without shelling out to git -- used by tests that mock
// the gitignore lookup.
func gitCheckIgnoreFn(ignored map[string]bool) func(context.Context, []string) (map[string]bool, error) {
	return func(_ context.Context, paths []string) (map[string]bool, error) {
		out := make(map[string]bool, len(paths))
		for _, p := range paths {
			out[p] = ignored[p]
		}
		return out, nil
	}
}
