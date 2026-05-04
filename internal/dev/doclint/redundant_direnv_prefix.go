package doclint

import (
	"regexp"
	"strings"
)

// redundantDirenvPrefixRule flags shell-block lines that prefix a
// command with `direnv exec .`. The dev shell auto-loads via direnv
// on `cd`, so contributor docs should show the bare command form;
// the canonical CLAUDE.md preamble that explains the prefix-from-
// outside-the-shell qualification is allowlisted.
type redundantDirenvPrefixRule struct{}

func (redundantDirenvPrefixRule) Name() string { return "redundant-direnv-prefix" }
func (redundantDirenvPrefixRule) Globs() []string {
	return []string{
		"docs/content/**/*.md",
		"CLAUDE.md",
		"CONTRIBUTING.md",
		".claude/**/*.md",
	}
}

// redundantDirenvPrefixAllowEntry suppresses a finding when File
// matches as a substring of the doc path AND Pattern matches the
// flagged line.
type redundantDirenvPrefixAllowEntry struct {
	File    string
	Pattern string
}

var redundantDirenvPrefixAllowlist = []redundantDirenvPrefixAllowEntry{
	{File: "CLAUDE.md", Pattern: `single source of truth for that qualification`},
}

func (redundantDirenvPrefixRule) Check(files []File) ([]Finding, error) {
	var findings []Finding
	for _, f := range files {
		findings = append(findings, scanRedundantDirenvPrefix(f, redundantDirenvPrefixAllowlist)...)
	}
	return findings, nil
}

func init() { register(redundantDirenvPrefixRule{}) }

// redundantDirenvPattern matches `direnv exec .` followed by a token
// boundary (whitespace or end-of-line). `direnv allow` and `direnv
// exec .foo` (no whitespace) won't match.
var redundantDirenvPattern = regexp.MustCompile(`\bdirenv\s+exec\s+\.(?:\s|$)`)

func scanRedundantDirenvPrefix(f File, allow []redundantDirenvPrefixAllowEntry) []Finding {
	var findings []Finding
	forEachShellCodeLine(f.Content, func(lineNum int, line string) {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			return
		}
		if !redundantDirenvPattern.MatchString(line) {
			return
		}
		if isRedundantDirenvAllowed(f.Path, line, allow) {
			return
		}
		findings = append(findings, Finding{
			File:  f.Path,
			Line:  lineNum,
			Token: "direnv exec .",
			Rule:  "redundant-direnv-prefix",
			Hint:  "drop the prefix -- the dev shell auto-loads on `cd`",
		})
	})
	return findings
}

func isRedundantDirenvAllowed(filePath, line string, allow []redundantDirenvPrefixAllowEntry) bool {
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
