package doclint

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// pathExistsRule flags backticked path-shaped tokens and markdown link
// targets in `.md` files that don't exist on disk and aren't gitignored.
// To stay practical the rule only considers tokens whose last segment
// has a file extension or which end with a trailing slash -- keeping
// out Kubernetes labels, Go import paths, and MIME types that share
// the same `<a>/<b>` shape.
type pathExistsRule struct{}

// pathExistsAllowEntry suppresses path-exists findings whose File
// matches as a substring of the doc path AND Pattern matches the
// flagged token text. An empty Pattern matches any token in the file.
type pathExistsAllowEntry struct {
	File    string
	Pattern string
}

var pathExistsAllowlist = []pathExistsAllowEntry{}

func (pathExistsRule) Name() string    { return "path-exists" }
func (pathExistsRule) Globs() []string { return []string{"**/*.md"} }

func (pathExistsRule) Check(files []File) ([]Finding, error) {
	if len(files) == 0 {
		return nil, nil
	}
	root, err := repoRoot(context.Background())
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for _, f := range files {
		got, err := scanPathExists(context.Background(), root, f, gitCheckIgnore)
		if err != nil {
			return nil, err
		}
		findings = append(findings, got...)
	}
	return findings, nil
}

func init() { register(pathExistsRule{}) }

// gitIgnoreLookup is the contract scanPathExists uses to test paths
// for gitignore status. The package-level [gitCheckIgnore] satisfies it
// directly; tests inject a stub.
type gitIgnoreLookup func(context.Context, []string) (map[string]bool, error)

var (
	pathExistsBacktickRe = regexp.MustCompile("`([^`\\s]+)`")
	pathExistsLinkRe     = regexp.MustCompile(`\]\(([^)\s]+?)\)`)
)

// scanPathExists examines f's prose lines, extracts path-shaped tokens
// (backticked or markdown-link targets), and returns findings for any
// that don't resolve on disk and aren't gitignored.
func scanPathExists(ctx context.Context, repoRoot string, f File, lookup gitIgnoreLookup) ([]Finding, error) {
	type candidate struct {
		token   string
		lineNum int
	}

	var candidates []candidate
	forEachProseLine(f.Content, func(lineNum int, line string) {
		for _, m := range pathExistsBacktickRe.FindAllStringSubmatch(line, -1) {
			candidates = append(candidates, candidate{m[1], lineNum})
		}
		for _, m := range pathExistsLinkRe.FindAllStringSubmatch(line, -1) {
			candidates = append(candidates, candidate{m[1], lineNum})
		}
	})

	docDir := filepath.Dir(f.Path)
	if docDir == "." {
		docDir = ""
	}

	var unresolved []candidate
	var unresolvedPaths []string
	for _, c := range candidates {
		if isPathExistsSkippable(c.token) {
			continue
		}
		resolved, rel := pathExistsResolve(repoRoot, docDir, c.token)
		if resolved {
			continue
		}
		unresolved = append(unresolved, c)
		unresolvedPaths = append(unresolvedPaths, rel)
	}

	if len(unresolved) == 0 {
		return nil, nil
	}

	ignored, err := lookup(ctx, unresolvedPaths)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for i, c := range unresolved {
		if ignored[unresolvedPaths[i]] {
			continue
		}
		if isPathExistsAllowed(f.Path, c.token, pathExistsAllowlist) {
			continue
		}
		findings = append(findings, Finding{
			File:  f.Path,
			Line:  c.lineNum,
			Token: c.token,
			Rule:  "path-exists",
			Hint:  "fix or remove the broken path",
		})
	}
	return findings, nil
}

func isPathExistsAllowed(filePath, token string, allow []pathExistsAllowEntry) bool {
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
		if re.MatchString(token) {
			return true
		}
	}
	return false
}

// isPathExistsSkippable returns true for tokens that aren't worth
// resolving: bare filenames (no slash), absolute paths, package-style
// @-prefixed names, in-page anchors, URLs with a scheme, template
// placeholders, and tokens whose final segment lacks a file extension
// (a heuristic that keeps Kubernetes labels, Go import paths, and
// MIME types out of the rule's mouth).
func isPathExistsSkippable(token string) bool {
	if token == "" {
		return true
	}
	if !strings.Contains(token, "/") {
		return true
	}
	if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "@") || strings.HasPrefix(token, "#") {
		return true
	}
	if strings.ContainsAny(token, "${}*?[]<>") {
		return true
	}
	if i := strings.Index(token, "://"); i > 0 {
		return true
	}
	if strings.HasPrefix(token, "mailto:") {
		return true
	}
	if strings.HasSuffix(token, "/") {
		// Trailing-slash tokens need at least one interior slash; a
		// single-segment foo/ is more often a namespace prefix than a
		// directory path.
		if strings.Count(token, "/") < 2 {
			return true
		}
		return false
	}
	// Final segment must contain a `.` (file extension) for the token
	// to be considered path-shaped.
	last := token
	if i := strings.LastIndex(token, "/"); i >= 0 {
		last = token[i+1:]
	}
	return !strings.Contains(last, ".")
}

// pathExistsResolve returns (true, "") if token resolves either at the
// repo root or doc-directory-relative. The empty rel is irrelevant in
// that case. On miss, returns (false, repoRelPath) -- the
// repo-relative path used for the gitignore lookup.
func pathExistsResolve(repoRoot, docDir, token string) (bool, string) {
	clean := strings.TrimPrefix(token, "./")
	rootRel := clean
	rootAbs := filepath.Join(repoRoot, clean)
	if _, err := os.Stat(rootAbs); err == nil {
		return true, rootRel
	}

	if docDir != "" {
		docRel := filepath.ToSlash(filepath.Join(docDir, clean))
		docAbs := filepath.Join(repoRoot, docRel)
		if _, err := os.Stat(docAbs); err == nil {
			return true, docRel
		}
		return false, docRel
	}
	return false, rootRel
}
