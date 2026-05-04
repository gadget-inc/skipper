package doclint

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// flagReferenceSyncRule keeps the configuration reference doc in lockstep
// with the controller and router config structs. Every `flag:"..."` tag
// in those structs MUST appear as a `--flag-name` row in the matching
// section of docs/content/reference/configuration.md (modulo a small
// allowlist for dev-only flags); every `--flag-name` row in those
// sections MUST exist in the corresponding struct.
type flagReferenceSyncRule struct{}

// flagSyncPair binds one config-source-of-truth Go file to the markdown
// section header that documents it.
type flagSyncPair struct {
	Section  string
	ConfigGo File
}

// flagSyncAllowEntry suppresses a struct-side flag that is intentionally
// undocumented (typically dev-only). Pairs are matched on (Section,
// Flag) exactly. The allowlist suppresses missing-from-docs findings
// only -- extra-in-docs flags are always reported.
type flagSyncAllowEntry struct {
	Section string
	Flag    string
}

const flagReferenceDocPath = "docs/content/reference/configuration.md"

// flagSyncSourcePairs lists the config-struct ↔ doc-section bindings
// scanned at run time. Adding a new operator-facing config struct means
// adding a new entry here AND a new section to the reference doc.
var flagSyncSourcePairs = []struct {
	Section string
	Path    string
}{
	{Section: "Controller configuration", Path: "internal/controller/config.go"},
	{Section: "Router configuration", Path: "internal/router/config.go"},
}

var flagReferenceSyncAllowlist = []flagSyncAllowEntry{
	// internal/controller/config.go web-template-dir is the dev-mode
	// template-reload knob; not a production tunable.
	{Section: "Controller configuration", Flag: "web-template-dir"},
	// internal/controller/config.go single-controller-mode bypasses
	// hash-ring discovery for local dev.
	{Section: "Controller configuration", Flag: "single-controller-mode"},
}

func (flagReferenceSyncRule) Name() string { return "flag-reference-sync" }
func (flagReferenceSyncRule) Globs() []string {
	return []string{flagReferenceDocPath}
}

func (flagReferenceSyncRule) Check(files []File) ([]Finding, error) {
	if len(files) == 0 {
		return nil, nil
	}
	var doc File
	for _, f := range files {
		if f.Path == flagReferenceDocPath {
			doc = f
			break
		}
	}
	if doc.Path == "" {
		return nil, nil
	}

	pairs, err := loadFlagSyncPairs(flagSyncSourcePairs)
	if err != nil {
		return nil, err
	}
	return scanFlagReferenceSync(pairs, doc, flagReferenceSyncAllowlist)
}

func init() { register(flagReferenceSyncRule{}) }

// loadFlagSyncPairs reads each binding's Go file from disk so the runner
// doesn't have to hand the rule a wider file slice. The doclint runner
// already filters [Rule.Check] inputs by [Rule.Globs] and config.go is
// not under the rule's globs.
func loadFlagSyncPairs(bindings []struct {
	Section string
	Path    string
},
) ([]flagSyncPair, error) {
	root, err := repoRoot(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]flagSyncPair, 0, len(bindings))
	for _, b := range bindings {
		abs := filepath.Join(root, b.Path)
		content, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", b.Path, err)
		}
		out = append(out, flagSyncPair{
			Section:  b.Section,
			ConfigGo: File{Path: b.Path, Content: content},
		})
	}
	return out, nil
}

var (
	flagSyncTagRe       = regexp.MustCompile("`(?:[^`]*\\s)?flag:\"([a-z][a-z0-9-]*)\"")
	flagSyncDocRowRe    = regexp.MustCompile("`--([a-z][a-z0-9-]*)`")
	flagSyncSectionRe   = regexp.MustCompile(`^##\s+(.+?)\s*$`)
	flagSyncTableRowRe  = regexp.MustCompile(`^\s*\|`)
	flagSyncTableSepRe  = regexp.MustCompile(`^\s*\|\s*-+`)
	flagSyncTableHeadRe = regexp.MustCompile(`(?i)^\s*\|\s*flag\s*\|`)
)

// scanFlagReferenceSync compares each pair's Go config-struct flag tags
// against the matching section's table in doc and returns findings for
// drift in either direction. Findings on missing-from-docs entries
// point at the section header line; findings on extra-in-docs entries
// point at the offending table row.
func scanFlagReferenceSync(pairs []flagSyncPair, doc File, allow []flagSyncAllowEntry) ([]Finding, error) {
	sections := extractFlagSections(doc.Content)

	var findings []Finding
	for _, pair := range pairs {
		structFlags := extractStructFlags(pair.ConfigGo.Content)
		section, ok := sections[pair.Section]
		if !ok {
			findings = append(findings, Finding{
				File:  doc.Path,
				Line:  1,
				Token: pair.Section,
				Rule:  "flag-reference-sync",
				Hint:  fmt.Sprintf("add a `## %s` section documenting %s", pair.Section, pair.ConfigGo.Path),
			})
			continue
		}

		structSet := make(map[string]struct{}, len(structFlags))
		for _, name := range structFlags {
			structSet[name] = struct{}{}
		}
		docSet := make(map[string]int, len(section.flags))
		for _, fr := range section.flags {
			docSet[fr.name] = fr.line
		}

		missing := make([]string, 0)
		for _, name := range structFlags {
			if _, has := docSet[name]; has {
				continue
			}
			if isFlagSyncAllowed(pair.Section, name, allow) {
				continue
			}
			missing = append(missing, name)
		}
		sort.Strings(missing)
		for _, name := range missing {
			findings = append(findings, Finding{
				File:  doc.Path,
				Line:  section.headerLine,
				Token: "--" + name,
				Rule:  "flag-reference-sync",
				Hint:  fmt.Sprintf("add a row for `--%s` (declared in %s) or extend the allowlist if the flag is dev-only", name, pair.ConfigGo.Path),
			})
		}

		extra := make([]string, 0)
		for name := range docSet {
			if _, has := structSet[name]; !has {
				extra = append(extra, name)
			}
		}
		sort.Strings(extra)
		for _, name := range extra {
			findings = append(findings, Finding{
				File:  doc.Path,
				Line:  docSet[name],
				Token: "--" + name,
				Rule:  "flag-reference-sync",
				Hint:  fmt.Sprintf("remove the row -- `--%s` is not declared in %s", name, pair.ConfigGo.Path),
			})
		}
	}
	return findings, nil
}

// extractStructFlags returns the flag-tag names declared in a Go
// configuration source file, in source order. Duplicate tags collapse
// to a single entry (later occurrences are ignored).
func extractStructFlags(content []byte) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, m := range flagSyncTagRe.FindAllStringSubmatch(string(content), -1) {
		name := m[1]
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

type flagDocSection struct {
	headerLine int
	flags      []flagDocRow
}

type flagDocRow struct {
	name string
	line int
}

// extractFlagSections walks doc and returns one entry per `##`-level
// section, populated with the `--flag-name` tokens found in the first
// markdown table after the heading. Rows before the table separator are
// treated as the table header and excluded.
func extractFlagSections(content []byte) map[string]flagDocSection {
	out := make(map[string]flagDocSection)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		currentName string
		currentSec  flagDocSection
		inTable     bool
		seenSep     bool
	)

	flush := func() {
		if currentName != "" {
			out[currentName] = currentSec
		}
		currentName = ""
		currentSec = flagDocSection{}
		inTable = false
		seenSep = false
	}

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if m := flagSyncSectionRe.FindStringSubmatch(line); m != nil {
			flush()
			currentName = strings.TrimSpace(m[1])
			currentSec = flagDocSection{headerLine: lineNum}
			continue
		}
		if currentName == "" {
			continue
		}
		if !flagSyncTableRowRe.MatchString(line) {
			if inTable {
				inTable = false
				seenSep = false
			}
			continue
		}
		// First table row after the heading is the column header; the
		// row that matches the separator pattern flips us into body.
		if !inTable {
			if flagSyncTableHeadRe.MatchString(line) || strings.Contains(strings.ToLower(line), "| flag") {
				inTable = true
				seenSep = false
				continue
			}
			continue
		}
		if !seenSep {
			if flagSyncTableSepRe.MatchString(line) {
				seenSep = true
			}
			continue
		}
		for _, m := range flagSyncDocRowRe.FindAllStringSubmatch(line, -1) {
			currentSec.flags = append(currentSec.flags, flagDocRow{name: m[1], line: lineNum})
			break // only the first column's flag name per row
		}
	}
	flush()
	return out
}

func isFlagSyncAllowed(section, flag string, allow []flagSyncAllowEntry) bool {
	for _, e := range allow {
		if e.Section == section && e.Flag == flag {
			return true
		}
	}
	return false
}
