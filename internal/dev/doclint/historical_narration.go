package doclint

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
)

// historicalNarrationRule flags phrases that narrate the diff -- a
// rule artifact lives next to the code; the journey to the current
// state belongs in version control, not in a comment that rots when
// the next refactor lands.
//
// The phrase list deliberately omits "replaces", "replaced by", "the
// original", and "originally"; each of those false-positives heavily
// on legitimate runtime-semantic prose in the controller / router
// packages.
type historicalNarrationRule struct{}

func (historicalNarrationRule) Name() string { return "historical-narration" }
func (historicalNarrationRule) Globs() []string {
	return []string{
		"internal/**/*.go",
		"cmd/**/*.go",
		"pkg/**/*.go",
		"docs/content/**/*.md",
		"CLAUDE.md",
		"CONTRIBUTING.md",
		"README.md",
		".claude/**/*.md",
	}
}

// historicalNarrationAllowEntry suppresses a finding when File matches
// as a substring of the file path AND Pattern matches the line text.
type historicalNarrationAllowEntry struct {
	File    string
	Pattern string
}

var historicalNarrationAllowlist = []historicalNarrationAllowEntry{
	// internal/controller/supervisor.go uses "previously" / "no
	// longer" to describe runtime state, not code history.
	{File: "internal/controller/supervisor.go", Pattern: `previously had instances|no longer (have|exist)`},
	// supervisor_test.go test names embed "previously" describing test
	// scenarios, not code history.
	{File: "internal/controller/supervisor_test.go", Pattern: `previously`},
	// internal/controller/pod.go: "pods that no longer exist" describes
	// the runtime garbage-collection condition.
	{File: "internal/controller/pod.go", Pattern: `no longer exist`},
	// User-action references are not code-history narration.
	{File: "internal/dev/profileops/profileops.go", Pattern: `previously fetched profiles`},
	{File: "cmd/dev/profile.go", Pattern: `previously fetched profiles`},
	// docs/content/architecture/overview.md describes runtime
	// hash-ring ownership state -- not code history.
	{File: "docs/content/architecture/overview.md", Pattern: `no longer responsible`},
	// router_test.go comment describes Go 1.26 ReverseProxy runtime
	// behavior, not code-history narration.
	{File: "internal/router/router_test.go", Pattern: `no longer propagates`},
	// The doclint package's own test fixtures and rule body.
	{File: "internal/dev/doclint/", Pattern: ``},
}

var historicalNarrationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bpreviously\b`),
	regexp.MustCompile(`(?i)\bhistorically\b`),
	regexp.MustCompile(`(?i)\bformerly\b`),
	regexp.MustCompile(`(?i)\bno longer\b`),
	regexp.MustCompile(`(?i)\bwe used to\b`),
	regexp.MustCompile(`(?i)\bused to be\b`),
	regexp.MustCompile(`(?i)\bused to have\b`),
	regexp.MustCompile(`(?i)\bfor backwards compatibility\b`),
	regexp.MustCompile(`(?i)\brenamed from\b`),
	regexp.MustCompile(`(?i)\brenamed to\b`),
	regexp.MustCompile(`(?i)\bmigrated from\b`),
	regexp.MustCompile(`(?i)\bextracted from\b`),
}

func (historicalNarrationRule) Check(files []File) ([]Finding, error) {
	var findings []Finding
	for _, f := range files {
		findings = append(findings, scanHistoricalNarration(f, historicalNarrationAllowlist)...)
	}
	return findings, nil
}

func init() { register(historicalNarrationRule{}) }

func scanHistoricalNarration(f File, allow []historicalNarrationAllowEntry) []Finding {
	ext := strings.ToLower(filepath.Ext(f.Path))
	switch ext {
	case ".md":
		return scanHistoricalNarrationProse(f, allow)
	case ".go":
		findings, err := scanHistoricalNarrationGo(f, allow)
		if err != nil {
			// Bad Go syntax should not block the lint -- the compiler
			// owns that finding.
			return nil
		}
		return findings
	default:
		return nil
	}
}

func scanHistoricalNarrationProse(f File, allow []historicalNarrationAllowEntry) []Finding {
	var findings []Finding
	forEachProseLine(f.Content, func(lineNum int, line string) {
		findings = append(findings, matchHistoricalNarration(f.Path, lineNum, line, allow)...)
	})
	return findings
}

func scanHistoricalNarrationGo(f File, allow []historicalNarrationAllowEntry) ([]Finding, error) {
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
				findings = append(findings, matchHistoricalNarration(f.Path, startPos.Line+offset, line, allow)...)
			}
		}
	}
	return findings, nil
}

func matchHistoricalNarration(filePath string, lineNum int, line string, allow []historicalNarrationAllowEntry) []Finding {
	if isHistoricalNarrationAllowed(filePath, line, allow) {
		return nil
	}
	var findings []Finding
	for _, re := range historicalNarrationPatterns {
		if loc := re.FindStringIndex(line); loc != nil {
			findings = append(findings, Finding{
				File:  filePath,
				Line:  lineNum,
				Token: line[loc[0]:loc[1]],
				Rule:  "historical-narration",
				Hint:  "describe current state -- the diff is the source of truth for the journey",
			})
		}
	}
	return findings
}

func isHistoricalNarrationAllowed(filePath, line string, allow []historicalNarrationAllowEntry) bool {
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
