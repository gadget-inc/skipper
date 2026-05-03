package main

import (
	"bytes"
	"context"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/gadget-inc/skipper/internal/dev/doclint"
)

type fixedRule struct {
	name     string
	findings []doclint.Finding
}

func (r fixedRule) Name() string    { return r.name }
func (r fixedRule) Globs() []string { return []string{"**/*.md"} }
func (r fixedRule) Check(_ []doclint.File) ([]doclint.Finding, error) {
	return r.findings, nil
}

func TestRunLintDocs_NoFindingsExitsZero(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := runLintDocs(context.Background(), &out, doclint.RunOptions{
		Rules: []doclint.Rule{},
		Files: []doclint.File{},
	})
	assert.NilError(t, err)
	assert.Equal(t, out.String(), "")
}

func TestRunLintDocs_FindingsExitNonZeroAndPrintExpectedFormat(t *testing.T) {
	t.Parallel()

	rule := fixedRule{
		name: "demo",
		findings: []doclint.Finding{
			{File: "a.md", Line: 4, Token: "pnpm", Rule: "demo", Hint: "use go"},
			{File: "b.md", Line: 1, Token: "astro", Rule: "demo", Hint: "use docssite"},
		},
	}

	var out bytes.Buffer
	err := runLintDocs(context.Background(), &out, doclint.RunOptions{
		Rules: []doclint.Rule{rule},
		Files: []doclint.File{
			{Path: "a.md", Content: []byte("body")},
			{Path: "b.md", Content: []byte("body")},
		},
	})
	assert.ErrorContains(t, err, "doclint")

	want := "" +
		"a.md:4: [demo] pnpm\n" +
		"    use go\n" +
		"b.md:1: [demo] astro\n" +
		"    use docssite\n"
	assert.Equal(t, out.String(), want)
}
