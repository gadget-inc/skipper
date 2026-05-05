package docssite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/gadget-inc/skipper/internal/skipper"
)

func TestRenderScaleReasonTable_HeaderShape(t *testing.T) {
	t.Parallel()

	out, err := renderScaleReasonTable()
	assert.NilError(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Assert(t, len(lines) >= 3, "table must have header, separator, and at least one row")

	header := strings.ToLower(lines[0])
	for _, col := range []string{"value", "number", "description"} {
		assert.Assert(t, strings.Contains(header, col),
			"header missing %q: %q", col, lines[0])
	}

	sep := strings.TrimSpace(lines[1])
	assert.Assert(t, strings.HasPrefix(sep, "|"))
	assert.Assert(t, strings.Contains(sep, "---"))
}

func TestRenderScaleReasonTable_HasOneRowPerEnumValue(t *testing.T) {
	t.Parallel()

	out, err := renderScaleReasonTable()
	assert.NilError(t, err)

	for _, name := range skipper.ScaleReason_name {
		assert.Assert(t, strings.Contains(out, "`"+name+"`"),
			"table missing row for %s\n%s", name, out)
	}
}

func TestRenderScaleReasonTable_RowsOrderedByEnumNumber(t *testing.T) {
	t.Parallel()

	out, err := renderScaleReasonTable()
	assert.NilError(t, err)

	prev := -1
	for n := int32(0); n < int32(len(skipper.ScaleReason_name)); n++ {
		name := skipper.ScaleReason_name[n]
		idx := strings.Index(out, "`"+name+"`")
		assert.Assert(t, idx > prev, "%s row out of order at idx=%d prev=%d", name, idx, prev)
		prev = idx
	}
}

func TestRenderScaleReasonTable_MissingDescriptionFails(t *testing.T) {
	t.Parallel()

	gappy := map[int32]string{0: "first only"}
	_, err := renderScaleReasonRows(skipper.ScaleReason_name, gappy)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errScaleReasonDescriptionDrift))
	assert.ErrorContains(t, err, "missing description")
}

func TestRenderScaleReasonTable_ExtraDescriptionFails(t *testing.T) {
	t.Parallel()

	descriptions := map[int32]string{}
	for n := range skipper.ScaleReason_name {
		descriptions[n] = "ok"
	}
	descriptions[999] = "nonexistent"

	_, err := renderScaleReasonRows(skipper.ScaleReason_name, descriptions)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errScaleReasonDescriptionDrift))
	assert.ErrorContains(t, err, "999")
}

func TestBuild_ScaleReasonTableShortcodeRendersAllValues(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: API
---

## ScaleReason

{{< scaleReasonTable >}}
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "api.md"), []byte(page), 0o644))

	err := Build(srcDir, outDir, BuildOptions{})
	assert.NilError(t, err)

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "api", "index.html"))
	assert.NilError(t, err)
	out := string(htmlBytes)

	for _, name := range skipper.ScaleReason_name {
		assert.Assert(t, strings.Contains(out, name),
			"rendered HTML missing %s", name)
	}
}
