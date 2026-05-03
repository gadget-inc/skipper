package doclint

import (
	"regexp"
	"strings"
)

// bareDevCommandRule flags shell-block lines whose first token is a
// known cobra subcommand name (`up`, `deploy`, `tests`, ...) without
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

// bareDevSubcommands lists the cobra subcommands registered on the
// root `dev` command. Updates here MUST stay in sync with
// cmd/dev/main.go's AddCommand list.
var bareDevSubcommands = map[string]bool{
	"up":        true,
	"deploy":    true,
	"tests":     true,
	"profile":   true,
	"logs":      true,
	"build":     true,
	"clean":     true,
	"fixture":   true,
	"kube-lint": true,
	"fmt":       true,
	"lint":      true,
	"lint-docs": true,
	"generate":  true,
	"docs":      true,
}

// bareDevInvocationFollowups lists the second tokens that make a line
// look like a real `dev <subcommand> <followup>` invocation: a flag
// (-/--), a known sub-subcommand identifier, a path-shaped argument,
// or a shell pipe/redirect.
var bareDevSubSubcommands = map[string]bool{
	"go":        true, // tests go
	"e2e":       true, // tests e2e
	"all":       true, // tests all
	"request":   true, // fixture request
	"websocket": true, // fixture websocket
	"load":      true, // fixture load
	"fetch":     true, // profile fetch
	"merge":     true, // profile merge
	"analyze":   true, // profile analyze
	"open":      true, // profile open
	"build":     true, // docs build / build
	"dev":       true, // direnv exec . dev <...>
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
