package doclint

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestHistoricalNarration_FlagsEachPhraseInMarkdown(t *testing.T) {
	t.Parallel()

	cases := []string{
		"This was previously a different design.",
		"Historically the controller did X.",
		"This config formerly applied to the router.",
		"This rule no longer applies after the migration.",
		"We used to call this method indirectly.",
		"The handler used to be a thin wrapper.",
		"This struct used to have three fields.",
		"Kept for backwards compatibility with old clients.",
		"Renamed from Foo to Bar in the v3 cleanup.",
		"Renamed to Bar from the older Foo name.",
		"Migrated from the previous JSON encoder.",
		"Extracted from the controller package.",
	}

	for _, line := range cases {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			got := scanHistoricalNarration(File{Path: "doc.md", Content: []byte(line)}, nil)
			assert.Assert(t, len(got) > 0, "expected a finding for: %q", line)
		})
	}
}

func TestHistoricalNarration_DoesNotFlagAllowedFalsePositives(t *testing.T) {
	t.Parallel()

	// These phrases were considered for the list but excluded because
	// they false-positive heavily on legitimate runtime-semantic prose.
	cases := []string{
		"replaceStaleInstances replaces them with fresh ones.",
		"Replaced by the next handler in the chain.",
		"the original request body is preserved.",
		"originally the controller's logic was separate.",
		"history alone does not justify a redesign.", // 'history alone'
		"still working through the queue.",           // 'still'
		"always returns the same result.",            // 'always'
	}

	for _, line := range cases {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			got := scanHistoricalNarration(File{Path: "doc.md", Content: []byte(line)}, nil)
			assert.Equal(t, len(got), 0, "expected no finding for: %q", line)
		})
	}
}

func TestHistoricalNarration_FencedBlocksSuppressMarkdownFindings(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"prose previously matters",
		"```",
		"previously inside a fence -- not flagged",
		"```",
	}, "\n")

	got := scanHistoricalNarration(File{Path: "doc.md", Content: []byte(src)}, nil)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Line, 1)
}

func TestHistoricalNarration_GoDocCommentsScannedStringLiteralsSkipped(t *testing.T) {
	t.Parallel()

	src := `package foo

// Foo previously did X.
func Foo() {
	x := "previously is in a string literal -- do not flag"
	_ = x
}
`
	got := scanHistoricalNarration(File{Path: "internal/foo/foo.go", Content: []byte(src)}, nil)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Line, 3)
}

func TestHistoricalNarration_AllowlistSuppression(t *testing.T) {
	t.Parallel()

	allow := []historicalNarrationAllowEntry{
		{File: "supervisor.go", Pattern: `previously had instances`},
	}

	src := `package foo

// supervisor previously had instances mapped here.
func S() {}
`
	got := scanHistoricalNarration(File{Path: "internal/controller/supervisor.go", Content: []byte(src)}, allow)
	assert.Equal(t, len(got), 0, "allowlist should suppress the controller-runtime use")

	// A different phrase in the same file is still flagged.
	src2 := `package foo

// the supervisor was migrated from the previous package.
func S() {}
`
	got = scanHistoricalNarration(File{Path: "internal/controller/supervisor.go", Content: []byte(src2)}, allow)
	assert.Equal(t, len(got), 1)
}

func TestHistoricalNarration_CaseInsensitive(t *testing.T) {
	t.Parallel()

	got := scanHistoricalNarration(File{Path: "doc.md", Content: []byte("PREVIOUSLY this used a different shape.")}, nil)
	assert.Equal(t, len(got), 1)
}
