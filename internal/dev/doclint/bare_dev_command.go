package doclint

import (
	"regexp"
	"strings"
)

// bareDevCommandRule flags shell-block lines whose first token is a
// known cobra subcommand name (`up`, `deploy`, `test`, ...) without
// the `dev` prefix. The rule scans only `bash`/`sh`/`shell`-tagged
// fenced blocks in markdown files inside docs/content/, CLAUDE.md,
// CONTRIBUTING.md, and .claude/.
type bareDevCommandRule struct{}

func (bareDevCommandRule) Name() string { return "bare-dev-command" }
func (bareDevCommandRule) Globs() []string {
	return []string{
		"docs/content/**/*.md",
		"CLAUDE.md",
		"CONTRIBUTING.md",
		".claude/**/*.md",
	}
}

func (bareDevCommandRule) Check(files []File) ([]Finding, error) {
	var findings []Finding
	for _, f := range files {
		findings = append(findings, scanBareDevCommand(f)...)
	}
	return findings, nil
}

func init() { register(bareDevCommandRule{}) }

// bareDevSubcommands lists the depth-1 cobra subcommand names, and
// bareDevSubSubcommands the depth-2 names. Both are populated by an
// init() in cmd/dev that introspects the live cobra tree, so a new
// subcommand is recognized automatically. Tests construct fixture
// maps via SetBareDevSubcommands.
var (
	bareDevSubcommands    = map[string]bool{}
	bareDevSubSubcommands = map[string]bool{}
)

// SetBareDevSubcommands injects the live cobra subcommand snapshot
// from the dev binary's main package. Called from cmd/dev/invoke.go's
// init() so the rule sees the same source of truth main() uses,
// without creating an import cycle (cmd/dev imports doclint, not the
// other way around). Tests construct their own snapshots inline.
func SetBareDevSubcommands(sub, subSub map[string]bool) {
	bareDevSubcommands = sub
	bareDevSubSubcommands = subSub
}

// bareDevDirenvPrefix matches `direnv exec . ` at the start of a line.
var bareDevDirenvPrefix = regexp.MustCompile(`^\s*direnv\s+exec\s+\.\s+`)

// scanBareDevCommand returns one finding per shell-block line whose
// first token is a known cobra subcommand and whose remainder makes it
// look like an invocation (vs. plain English).
func scanBareDevCommand(f File) []Finding {
	var findings []Finding
	forEachShellCodeLine(f.Content, func(lineNum int, line string) {
		stripped := line
		if loc := bareDevDirenvPrefix.FindStringIndex(line); loc != nil {
			stripped = line[loc[1]:]
		} else if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			// Continuation lines (leading whitespace, no direnv prefix)
			// are skipped.
			return
		}
		trimmed := strings.TrimLeft(stripped, " \t")
		if trimmed == "" {
			return
		}
		if strings.HasPrefix(trimmed, "#") {
			return
		}

		first, rest := splitFirstToken(trimmed)
		if first == "dev" {
			// Already prefixed. The line is correct; do not flag.
			return
		}
		if !bareDevSubcommands[first] {
			return
		}
		if !looksLikeInvocation(rest) {
			return
		}
		findings = append(findings, Finding{
			File:  f.Path,
			Line:  lineNum,
			Token: first,
			Rule:  "bare-dev-command",
			Hint:  "prefix with `dev ` (the dev shell auto-loads via direnv)",
		})
	})
	return findings
}

// splitFirstToken splits trimmed input on whitespace and returns the
// first token plus the rest of the line.
func splitFirstToken(s string) (string, string) {
	idx := strings.IndexAny(s, " \t")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

// looksLikeInvocation returns true when the post-subcommand remainder
// is shaped like a real shell invocation: a flag, a recognized
// sub-subcommand, a path argument (contains / or .), or a shell
// pipe/redirect.
func looksLikeInvocation(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		// Bare `deploy` on its own (e.g. listed in a flow) -- treat as
		// a real invocation; the user expected `dev deploy`.
		return true
	}
	if rest[0] == '-' {
		return true
	}
	if strings.ContainsAny(rest, "|>") {
		return true
	}
	first, _ := splitFirstToken(rest)
	if bareDevSubSubcommands[first] {
		return true
	}
	if strings.ContainsAny(first, "/.") {
		return true
	}
	return false
}
