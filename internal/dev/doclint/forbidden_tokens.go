package doclint

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
)

// forbiddenToken describes a single banned word/phrase: its canonical
// display name (used in the [Finding.Token] field), the regex used to
// match it inside source content, and the human-readable hint shown
// alongside the finding.
type forbiddenToken struct {
	name string
	re   *regexp.Regexp
	hint string
}

// forbiddenTokensAllowEntry suppresses findings on lines where File
// matches as a substring of the file path AND Pattern matches the line
// text. Pattern is a Go regex; an empty Pattern matches every line.
type forbiddenTokensAllowEntry struct {
	File    string
	Pattern string
}

var forbiddenTokenList = []forbiddenToken{
	{"pnpm", regexp.MustCompile(`(?i)\bpnpm\b`), "drop the JavaScript toolchain reference"},
	{"oxc", regexp.MustCompile(`(?i)\boxc\b`), "drop the JS-tooling reference (oxc)"},
	{"oxlint", regexp.MustCompile(`(?i)\boxlint\b`), "drop the oxlint reference"},
	{"oxfmt", regexp.MustCompile(`(?i)\boxfmt\b`), "drop the oxfmt reference"},
	{"astro", regexp.MustCompile(`(?i)\bastro\b`), "describe the docs site without the Astro framework reference"},
	{"starlight", regexp.MustCompile(`(?i)\bstarlight\b`), "describe the docs site without the Starlight reference"},
	{"tailwind", regexp.MustCompile(`(?i)\btailwind\b`), "drop the Tailwind reference (docs use vanilla CSS)"},
	{"playwright", regexp.MustCompile(`(?i)\bplaywright\b`), "describe the e2e suite without the Playwright reference (chromedp now)"},
	{"vitest", regexp.MustCompile(`(?i)\bvitest\b`), "drop the vitest reference"},
	{"node_modules", regexp.MustCompile(`(?i)\bnode_modules\b`), "drop the node_modules reference"},
	{"tsconfig", regexp.MustCompile(`(?i)\btsconfig\b`), "drop the tsconfig reference"},
	{"pnpm-lock.yaml", regexp.MustCompile(`(?i)\bpnpm-lock\.yaml\b`), "drop the pnpm lockfile reference"},
	{"pnpm-workspace.yaml", regexp.MustCompile(`(?i)\bpnpm-workspace\.yaml\b`), "drop the pnpm-workspace reference"},
	{"@astrojs/", regexp.MustCompile(`@astrojs/`), "drop the Astro packages reference"},
	{".mdx", regexp.MustCompile(`(?i)\b\w+\.mdx\b`), "rename to plain .md (the docs site uses Goldmark, not MDX)"},
	{"MDX", regexp.MustCompile(`\bMDX\b`), "describe content as plain Markdown (the docs site is Goldmark-based)"},
}

var forbiddenTokensAllowlist = []forbiddenTokensAllowEntry{
	// internal/dev/watcher/watcher.go SkipDir case -- defensive safety
	// net for stray Node trees. Kept indefinitely.
	{File: "internal/dev/watcher/watcher.go", Pattern: `node_modules`},
	// The doclint package's own test fixtures.
	{File: "internal/dev/doclint/", Pattern: ``},
}

type forbiddenTokensRule struct{}

func (forbiddenTokensRule) Name() string    { return "forbidden-tokens" }
func (forbiddenTokensRule) Globs() []string { return []string{"**/*"} }

func (forbiddenTokensRule) Check(files []File) ([]Finding, error) {
	var findings []Finding
	for _, f := range files {
		got, err := scanForbiddenTokensWithAllow(f, forbiddenTokensAllowlist)
		if err != nil {
			return nil, err
		}
		findings = append(findings, got...)
	}
	return findings, nil
}

func init() { register(forbiddenTokensRule{}) }

// scanForbiddenTokens scans a single file with the package allowlist.
func scanForbiddenTokens(f File) ([]Finding, error) {
	return scanForbiddenTokensWithAllow(f, nil)
}

// scanForbiddenTokensWithAllow scans a single file with the supplied
// allowlist. Markdown skips fenced blocks; Go scans only doc/inline
// comments via go/ast; everything else is scanned line-by-line.
func scanForbiddenTokensWithAllow(f File, allow []forbiddenTokensAllowEntry) ([]Finding, error) {
	ext := strings.ToLower(filepath.Ext(f.Path))
	switch ext {
	case ".md":
		return scanLines(f, allow, true)
	case ".go":
		return scanGoComments(f, allow)
	default:
		return scanLines(f, allow, false)
	}
}

// scanLines walks every line in f.Content and emits a Finding for each
// forbidden-token regex match. When skipFences is true, fenced code
// blocks (``` ... ```) are skipped (markdown semantics); otherwise the
// entire content is scanned.
func scanLines(f File, allow []forbiddenTokensAllowEntry, skipFences bool) ([]Finding, error) {
	var findings []Finding
	visit := func(lineNum int, line string) {
		findings = append(findings, matchTokens(f.Path, lineNum, line, allow)...)
	}
	if skipFences {
		forEachProseLine(f.Content, visit)
	} else {
		forEachLine(f.Content, visit)
	}
	return findings, nil
}

// scanGoComments parses f as a Go source file and emits findings for
// every forbidden-token match inside any comment. String literals,
// identifiers, and other syntax are deliberately not scanned.
func scanGoComments(f File, allow []forbiddenTokensAllowEntry) ([]Finding, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, f.Path, f.Content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", f.Path, err)
	}
	var findings []Finding
	for _, group := range parsed.Comments {
		for _, c := range group.List {
			startPos := fset.Position(c.Pos())
			body := stripCommentMarkers(c.Text)
			for offset, line := range strings.Split(body, "\n") {
				findings = append(findings, matchTokens(f.Path, startPos.Line+offset, line, allow)...)
			}
		}
	}
	return findings, nil
}

// stripCommentMarkers removes the `//` or `/* ... */` syntax so the
// regex matchers see plain comment text. Multi-line block comments
// keep their newlines; per-line `//` markers are stripped.
func stripCommentMarkers(comment string) string {
	if rest, ok := strings.CutPrefix(comment, "//"); ok {
		return rest
	}
	if strings.HasPrefix(comment, "/*") && strings.HasSuffix(comment, "*/") {
		return comment[2 : len(comment)-2]
	}
	return comment
}

// matchTokens returns findings for every forbidden-token regex hit on
// line, suppressing those that match an allowlist entry.
func matchTokens(filePath string, lineNum int, line string, allow []forbiddenTokensAllowEntry) []Finding {
	var findings []Finding
	for _, tok := range forbiddenTokenList {
		if !tok.re.MatchString(line) {
			continue
		}
		if isAllowed(filePath, line, allow) {
			continue
		}
		findings = append(findings, Finding{
			File:  filePath,
			Line:  lineNum,
			Token: tok.name,
			Rule:  "forbidden-tokens",
			Hint:  tok.hint,
		})
	}
	return findings
}

func isAllowed(filePath, line string, allow []forbiddenTokensAllowEntry) bool {
	for _, entry := range allow {
		if !strings.Contains(filePath, entry.File) {
			continue
		}
		if entry.Pattern == "" {
			return true
		}
		re, err := regexp.Compile(entry.Pattern)
		if err != nil {
			continue
		}
		if re.MatchString(line) {
			return true
		}
	}
	return false
}
