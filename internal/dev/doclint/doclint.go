// Package doclint enforces a set of small, opinionated rules over the
// repository's documentation surface — markdown, Go doc comments, and a
// handful of dotfiles. Each rule is a self-contained file in this
// package; rules self-register at init time via [register], and Run
// dispatches every registered (or override-supplied) rule against a
// glob-filtered slice of repo-tracked files.
package doclint

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Finding is a single rule violation. File is repo-root-relative; Line
// is 1-based; Token is the offending substring; Rule is the violating
// rule's [Rule.Name]; Hint is a human-readable suggestion.
type Finding struct {
	File  string
	Line  int
	Token string
	Rule  string
	Hint  string
}

// File is a single source file enumerated by the runner. Path is
// repo-root-relative. Content is the file's bytes; rules MUST NOT
// re-read from disk.
type File struct {
	Path    string
	Content []byte
}

// Rule is the contract every doclint rule implements.
type Rule interface {
	// Name returns the rule's stable identifier, used in finding output.
	Name() string
	// Globs returns the file globs the rule cares about (e.g.
	// "**/*.md", "internal/**/*.go"). The runner filters files via
	// these globs before invoking [Rule.Check].
	Globs() []string
	// Check inspects the glob-filtered files and returns any findings.
	// Returning an error aborts the run; the runner propagates the
	// first error encountered.
	Check(files []File) ([]Finding, error)
}

// RunOptions configures a single [Run] invocation. Both fields are
// optional override hooks: a non-nil Rules replaces the package
// registry, and a non-nil Files skips the git-tracked enumeration.
type RunOptions struct {
	Rules []Rule
	Files []File
}

var registry []Rule

// register adds a rule to the package registry. Each rule file calls
// it from its init().
func register(r Rule) {
	registry = append(registry, r)
}

// Run executes every applicable rule against the repository's tracked
// text files. When opts.Rules is non-nil, the override slice is used in
// place of the registry; when opts.Files is non-nil, those files are
// scanned in place of the git-tracked enumeration. Findings are
// aggregated and sorted by (File, Line).
func Run(ctx context.Context, opts RunOptions) ([]Finding, error) {
	rules := opts.Rules
	if rules == nil {
		rules = registry
	}

	files := opts.Files
	if files == nil {
		enumerated, err := enumerateTrackedFiles(ctx)
		if err != nil {
			return nil, fmt.Errorf("enumerate tracked files: %w", err)
		}
		files = enumerated
	}

	var findings []Finding
	for _, rule := range rules {
		matched := filterByGlobs(files, rule.Globs())
		got, err := rule.Check(matched)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.Name(), err)
		}
		findings = append(findings, got...)
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

// filterByGlobs returns the subset of files whose Path matches at least
// one glob. Input order is preserved.
func filterByGlobs(files []File, globs []string) []File {
	if len(globs) == 0 {
		return nil
	}
	out := make([]File, 0, len(files))
	for _, f := range files {
		for _, g := range globs {
			if matchGlob(g, f.Path) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// matchGlob matches a path against a doublestar-aware glob. "**/"
// matches zero or more leading path segments, "**" alone matches any
// substring (including across separators), "*" matches a single path
// segment, and "?" matches one non-separator character.
func matchGlob(glob, p string) bool {
	return globRegex(glob).MatchString(p)
}

var globCache sync.Map // map[string]*regexp.Regexp

func globRegex(glob string) *regexp.Regexp {
	if cached, ok := globCache.Load(glob); ok {
		return cached.(*regexp.Regexp)
	}
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				if i+2 < len(glob) && glob[i+2] == '/' {
					sb.WriteString("(?:.*/)?")
					i += 2
				} else {
					sb.WriteString(".*")
					i++
				}
				continue
			}
			sb.WriteString("[^/]*")
		case '?':
			sb.WriteString("[^/]")
		case '.', '+', '(', ')', '{', '}', '|', '^', '$', '\\', '[', ']':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteString("$")
	re := regexp.MustCompile(sb.String())
	globCache.Store(glob, re)
	return re
}

// enumerateTrackedFiles invokes `git ls-files` from the repository
// root, loads each file's bytes into memory, and returns the list.
// Binary-shaped files are skipped via a content sniff.
func enumerateTrackedFiles(ctx context.Context) ([]File, error) {
	root, err := repoRoot(ctx)
	if err != nil {
		return nil, err
	}

	cmd := osexec.CommandContext(ctx, "git", "-C", root, "ls-files")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	var files []File
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		rel := strings.TrimSpace(scanner.Text())
		if rel == "" {
			continue
		}
		abs := filepath.Join(root, rel)
		content, readErr := os.ReadFile(abs)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				// Tracked but locally deleted; skip silently.
				continue
			}
			return nil, fmt.Errorf("read %s: %w", rel, readErr)
		}
		if isBinary(content) {
			continue
		}
		files = append(files, File{Path: filepath.ToSlash(rel), Content: content})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan git ls-files: %w", err)
	}
	return files, nil
}

// isBinary returns true for content that contains a NUL byte in the
// first 8 KiB. Good enough to keep .png, .pdf, etc. out of token-based
// rules.
func isBinary(content []byte) bool {
	limit := min(len(content), 8192)
	return bytes.IndexByte(content[:limit], 0) >= 0
}

// repoRoot returns the absolute path to the worktree root. WORKSPACE_DIR
// (set by the dev shell's direnv) wins; falls back to `git rev-parse
// --show-toplevel`.
func repoRoot(ctx context.Context) (string, error) {
	if d := os.Getenv("WORKSPACE_DIR"); d != "" {
		return d, nil
	}
	cmd := osexec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
