package doclint

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
)

// frontmatterScopeRule flags YAML frontmatter `paths:` globs in
// .claude/rules/*.md and .claude/skills/**/*.md that match zero tracked
// files. Globs are matched against the runner-provided file list (so
// gitignore semantics are inherited via `git ls-files`).
type frontmatterScopeRule struct{}

func (frontmatterScopeRule) Name() string { return "frontmatter-scope-exists" }
func (frontmatterScopeRule) Globs() []string {
	return []string{
		".claude/rules/*.md",
		".claude/skills/**/*.md",
	}
}

func (frontmatterScopeRule) Check(files []File) ([]Finding, error) {
	// `files` is the glob-filtered subset (rule docs only). The
	// candidate set for glob matching is the union of all files the
	// runner would pass to any rule -- but at this stage we only see
	// our subset. The runner doesn't pass the full file slice, so we
	// fall back to enumerating tracked files separately.
	tracked, err := enumerateTrackedFiles(context.Background())
	if err != nil {
		return nil, fmt.Errorf("enumerate for frontmatter-scope: %w", err)
	}
	var findings []Finding
	for _, f := range files {
		got, err := scanFrontmatterScope(f, tracked)
		if err != nil {
			return nil, err
		}
		findings = append(findings, got...)
	}
	return findings, nil
}

func init() { register(frontmatterScopeRule{}) }

// scanFrontmatterScope inspects f's YAML frontmatter for a `paths:`
// list and returns one finding per glob that matches zero tracked
// files.
func scanFrontmatterScope(f File, tracked []File) ([]Finding, error) {
	globs := extractFrontmatterPaths(f.Content)
	if len(globs) == 0 {
		return nil, nil
	}
	var findings []Finding
	for _, g := range globs {
		if anyMatch(tracked, g.glob) {
			continue
		}
		findings = append(findings, Finding{
			File:  f.Path,
			Line:  g.line,
			Token: g.glob,
			Rule:  "frontmatter-scope-exists",
			Hint:  "remove or update the glob -- it matches no tracked files",
		})
	}
	return findings, nil
}

type frontmatterGlob struct {
	glob string
	line int // 1-based source line of the glob entry
}

// extractFrontmatterPaths returns the YAML `paths:` list entries from
// the leading frontmatter block, with each entry's source line number
// recorded for reporting. Frontmatter must open on line 1 with `---`
// and close with another `---` line; entries are list items
// (`  - "..."`) following a `paths:` key. Comments and other YAML keys
// are skipped.
func extractFrontmatterPaths(content []byte) []frontmatterGlob {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return nil
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return nil
	}

	var globs []frontmatterGlob
	inPaths := false
	lineNum := 1
	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "---" {
			break
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t") {
			if !inPaths {
				continue
			}
			if !strings.HasPrefix(trimmed, "-") {
				inPaths = false
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			value = strings.Trim(value, `"'`)
			if value == "" {
				continue
			}
			globs = append(globs, frontmatterGlob{glob: value, line: lineNum})
			continue
		}
		// Top-level YAML key. Set inPaths only if it's exactly `paths:`.
		if trimmed == "paths:" {
			inPaths = true
			continue
		}
		inPaths = false
	}
	return globs
}

// anyMatch returns true if any tracked file's path matches the glob.
func anyMatch(files []File, glob string) bool {
	for _, f := range files {
		if matchGlob(glob, f.Path) {
			return true
		}
	}
	return false
}
