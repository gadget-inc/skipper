package doclint_test

import (
	"context"
	"errors"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/gadget-inc/skipper/internal/dev/doclint"
)

// stubRule is an in-memory Rule for runner tests; declared globs decide
// which files Check is invoked against, and findings is returned as-is.
type stubRule struct {
	name     string
	globs    []string
	findings []doclint.Finding
	err      error
	seen     [][]string // per-call file paths, set inside Check
}

func (r *stubRule) Name() string    { return r.name }
func (r *stubRule) Globs() []string { return r.globs }
func (r *stubRule) Check(files []doclint.File) ([]doclint.Finding, error) {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	r.seen = append(r.seen, paths)
	return r.findings, r.err
}

func TestRun_EmptyRulesReturnsNoFindings(t *testing.T) {
	t.Parallel()

	got, err := doclint.Run(context.Background(), doclint.RunOptions{
		Rules: []doclint.Rule{},
	})

	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

func TestRun_AggregatesFindingsSortedByFileThenLine(t *testing.T) {
	t.Parallel()

	rule := &stubRule{
		name:  "stub",
		globs: []string{"**/*.md"},
		findings: []doclint.Finding{
			{File: "b.md", Line: 1, Token: "x", Rule: "stub", Hint: "h"},
			{File: "a.md", Line: 5, Token: "y", Rule: "stub", Hint: "h"},
			{File: "a.md", Line: 2, Token: "z", Rule: "stub", Hint: "h"},
		},
	}

	got, err := doclint.Run(context.Background(), doclint.RunOptions{
		Rules: []doclint.Rule{rule},
	})

	assert.NilError(t, err)
	want := []doclint.Finding{
		{File: "a.md", Line: 2, Token: "z", Rule: "stub", Hint: "h"},
		{File: "a.md", Line: 5, Token: "y", Rule: "stub", Hint: "h"},
		{File: "b.md", Line: 1, Token: "x", Rule: "stub", Hint: "h"},
	}
	assert.DeepEqual(t, got, want)
}

func TestRun_PropagatesRuleError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	rule := &stubRule{name: "stub", globs: []string{"**/*.md"}, err: boom}

	_, err := doclint.Run(context.Background(), doclint.RunOptions{
		Rules: []doclint.Rule{rule},
	})

	assert.ErrorIs(t, err, boom)
}

func TestRun_NilRulesUsesPackageRegistry(t *testing.T) {
	t.Parallel()

	// Files=[] with Rules=nil means: enumerate the registered rules
	// against an empty file set. The shipped always-empty rule should
	// produce zero findings, but this confirms the registry is consulted
	// (vs. the spec's Rules-override path).
	got, err := doclint.Run(context.Background(), doclint.RunOptions{
		Files: []doclint.File{},
	})
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}

func TestRun_FiltersFilesByRuleGlobs(t *testing.T) {
	t.Parallel()

	mdRule := &stubRule{name: "md", globs: []string{"**/*.md"}}
	goRule := &stubRule{name: "go", globs: []string{"**/*.go"}}

	files := []doclint.File{
		{Path: "README.md", Content: []byte("# readme")},
		{Path: "main.go", Content: []byte("package main")},
		{Path: "docs/intro.md", Content: []byte("# intro")},
	}

	_, err := doclint.Run(context.Background(), doclint.RunOptions{
		Rules: []doclint.Rule{mdRule, goRule},
		Files: files,
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, mdRule.seen, [][]string{{"README.md", "docs/intro.md"}})
	assert.DeepEqual(t, goRule.seen, [][]string{{"main.go"}})
}
