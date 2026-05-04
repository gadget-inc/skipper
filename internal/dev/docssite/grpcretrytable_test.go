package docssite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/gadget-inc/skipper/internal/controller"
)

func TestRenderGrpcRetryTable_HeaderShape(t *testing.T) {
	t.Parallel()

	out, err := renderGrpcRetryTable()
	assert.NilError(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Assert(t, len(lines) >= 3, "table must have header, separator, and at least one row")

	header := strings.ToLower(lines[0])
	for _, col := range []string{"method", "max attempts", "max backoff", "retry on"} {
		assert.Assert(t, strings.Contains(header, col),
			"header missing %q: %q", col, lines[0])
	}

	sep := strings.TrimSpace(lines[1])
	assert.Assert(t, strings.HasPrefix(sep, "|"))
	assert.Assert(t, strings.Contains(sep, "---"))
}

func TestRenderGrpcRetryTable_HasOneRowPerPolicy(t *testing.T) {
	t.Parallel()

	out, err := renderGrpcRetryTable()
	assert.NilError(t, err)

	for _, p := range controller.DefaultRetryPolicies {
		assert.Assert(t, strings.Contains(out, "| "+p.Method+" "),
			"table missing row for %s\n%s", p.Method, out)
	}
}

func TestRenderGrpcRetryTable_FormatsBackoffAsHumanReadable(t *testing.T) {
	t.Parallel()

	out, err := renderGrpcRetryTable()
	assert.NilError(t, err)

	for _, p := range controller.DefaultRetryPolicies {
		assert.Assert(t, strings.Contains(out, p.MaxBackoff.String()),
			"row for %s should contain backoff %q\n%s", p.Method, p.MaxBackoff.String(), out)
	}
}

func TestRenderGrpcRetryTable_RowOrderMatchesPolicySlice(t *testing.T) {
	t.Parallel()

	out, err := renderGrpcRetryTable()
	assert.NilError(t, err)

	prev := -1
	for _, p := range controller.DefaultRetryPolicies {
		idx := strings.Index(out, "| "+p.Method+" ")
		assert.Assert(t, idx > prev, "%s row out of order", p.Method)
		prev = idx
	}
}

func TestRenderGrpcRetryTable_JoinsRetryableStatusCodes(t *testing.T) {
	t.Parallel()

	t.Run("single code", func(t *testing.T) {
		t.Parallel()
		row := renderGrpcRetryRow(controller.RetryPolicy{
			Method: "Foo", MaxAttempts: 1,
			MaxBackoff: 10 * time.Millisecond, RetryableStatusCodes: []string{"UNAVAILABLE"},
		})
		assert.Assert(t, strings.Contains(row, "UNAVAILABLE"))
	})

	t.Run("multiple codes", func(t *testing.T) {
		t.Parallel()
		row := renderGrpcRetryRow(controller.RetryPolicy{
			Method: "Foo", MaxAttempts: 1,
			MaxBackoff: 10 * time.Millisecond, RetryableStatusCodes: []string{"UNAVAILABLE", "DEADLINE_EXCEEDED"},
		})
		assert.Assert(t, strings.Contains(row, "UNAVAILABLE, DEADLINE_EXCEEDED"))
	})
}

func TestBuild_GrpcRetryTableShortcodeRendersAllPolicies(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: API
---

## Client retry policies

{{< grpcRetryTable >}}
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "api.md"), []byte(page), 0o644))

	err := Build(srcDir, outDir, BuildOptions{})
	assert.NilError(t, err)

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "api", "index.html"))
	assert.NilError(t, err)
	out := string(htmlBytes)

	for _, p := range controller.DefaultRetryPolicies {
		assert.Assert(t, strings.Contains(out, p.Method),
			"rendered HTML missing %s: %s", p.Method, out)
	}
}
