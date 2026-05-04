package doclint

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestForbiddenTokens_FlagsEachStaleToolchainWordInMarkdown(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"line one mentions pnpm install",                    // pnpm
		"line two mentions oxlint and oxfmt",                // oxlint, oxfmt
		"line three uses oxc.enable",                        // oxc
		"line four cites Astro and Starlight",               // astro, starlight
		"line five names tailwind",                          // tailwind
		"line six talks playwright",                         // playwright
		"line seven runs vitest",                            // vitest
		"line eight reads node_modules/typescript/lib",      // node_modules
		"line nine touches tsconfig.json",                   // tsconfig
		"line ten copies pnpm-lock.yaml",                    // pnpm-lock.yaml
		"line eleven copies pnpm-workspace.yaml",            // pnpm-workspace.yaml
		"line twelve imports @astrojs/starlight/components", // @astrojs/, starlight
		"line thirteen ends in foo.mdx",                     // .mdx
		"line fourteen names MDX directly",                  // MDX
	}, "\n")

	got, err := scanForbiddenTokens(File{Path: "doc.md", Content: []byte(src)})
	assert.NilError(t, err)

	hits := uniqueRules(got, "forbidden-tokens")
	want := []string{
		".mdx", "@astrojs/", "MDX", "astro", "node_modules", "oxc",
		"oxfmt", "oxlint", "playwright", "pnpm", "pnpm-lock.yaml",
		"pnpm-workspace.yaml", "starlight", "tailwind", "tsconfig", "vitest",
	}
	assert.DeepEqual(t, hits, want)
}

func TestForbiddenTokens_SkipsFencedCodeBlocksInMarkdown(t *testing.T) {
	t.Parallel()

	src := strings.Join([]string{
		"prose mentions pnpm",
		"```js",
		"const a = require('pnpm-lock.yaml');",
		"```",
		"more prose",
	}, "\n")

	got, err := scanForbiddenTokens(File{Path: "doc.md", Content: []byte(src)})
	assert.NilError(t, err)

	// Only the prose-line pnpm should match; the fenced-block
	// pnpm-lock.yaml should NOT.
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "pnpm")
	assert.Equal(t, got[0].Line, 1)
}

func TestForbiddenTokens_GoDocCommentsScannedStringLiteralsSkipped(t *testing.T) {
	t.Parallel()

	src := `package foo

// flagged: pnpm install reference in a doc comment.
func A() {
	x := "pnpm in a string literal -- should NOT be flagged"
	_ = x
}
`
	got, err := scanForbiddenTokens(File{Path: "foo.go", Content: []byte(src)})
	assert.NilError(t, err)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "pnpm")
	assert.Equal(t, got[0].Line, 3)
}

func TestForbiddenTokens_JSONFileScannedAllLines(t *testing.T) {
	t.Parallel()

	src := `{
  "oxc.enable": true,
  "typescript.tsdk": "node_modules/typescript/lib"
}`
	got, err := scanForbiddenTokens(File{Path: "settings.json", Content: []byte(src)})
	assert.NilError(t, err)

	tokens := tokensFromFindings(got)
	assert.Assert(t, contains(tokens, "oxc"), "oxc should be flagged in JSON")
	assert.Assert(t, contains(tokens, "node_modules"), "node_modules should be flagged in JSON")
}

func TestForbiddenTokens_AllowlistSuppression(t *testing.T) {
	t.Parallel()

	allow := []forbiddenTokensAllowEntry{
		{File: "watcher.go", Pattern: "node_modules"},
	}

	src := `package foo

// references node_modules as a Skipper directory skip case.
func skip() {}
`
	got, err := scanForbiddenTokensWithAllow(File{Path: "internal/dev/watcher/watcher.go", Content: []byte(src)}, allow)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0, "allowlist should suppress node_modules in watcher.go")

	// And the same line in a different file is still flagged.
	got, err = scanForbiddenTokensWithAllow(File{Path: "other.go", Content: []byte(src)}, allow)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "node_modules")
}

func TestForbiddenTokens_CaseInsensitiveAndCaseSensitiveTokens(t *testing.T) {
	t.Parallel()

	// Most tokens are case-insensitive.
	src := "PnPm and PLAYWRIGHT and TaIlWiNd"
	got, err := scanForbiddenTokens(File{Path: "doc.md", Content: []byte(src)})
	assert.NilError(t, err)

	tokens := tokensFromFindings(got)
	assert.Assert(t, contains(tokens, "pnpm"))
	assert.Assert(t, contains(tokens, "playwright"))
	assert.Assert(t, contains(tokens, "tailwind"))

	// MDX is case-sensitive: lowercase "mdx" alone (without the leading
	// dot of the file extension) MUST NOT match the standalone-MDX rule.
	got, err = scanForbiddenTokens(File{Path: "doc.md", Content: []byte("the term mdx in lowercase")})
	assert.NilError(t, err)
	tokens = tokensFromFindings(got)
	assert.Assert(t, !contains(tokens, "MDX"), "lowercase mdx should not match standalone MDX token")
}

func TestForbiddenTokens_BinaryAndUnknownExtensionsScannedAsPlainText(t *testing.T) {
	t.Parallel()

	// .gitignore-style dotfile -- scanned in full.
	src := "node_modules\nvendor\n"
	got, err := scanForbiddenTokens(File{Path: ".dockerignore", Content: []byte(src)})
	assert.NilError(t, err)
	tokens := tokensFromFindings(got)
	assert.Assert(t, contains(tokens, "node_modules"))
}

// uniqueRules returns the sorted unique Token values for findings whose
// Rule equals the given rule name.
func uniqueRules(findings []Finding, rule string) []string {
	seen := map[string]struct{}{}
	for _, f := range findings {
		if f.Rule == rule {
			seen[f.Token] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func tokensFromFindings(findings []Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Token
	}
	return out
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}
