package docssite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestRenderMessageTable_HeaderShape(t *testing.T) {
	t.Parallel()

	out, err := renderMessageTable("Function")
	assert.NilError(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Assert(t, len(lines) >= 3)

	header := strings.ToLower(lines[0])
	for _, col := range []string{"field", "type", "number", "description"} {
		assert.Assert(t, strings.Contains(header, col),
			"header missing %q: %q", col, lines[0])
	}
}

func TestRenderMessageTable_FunctionFields(t *testing.T) {
	t.Parallel()

	out, err := renderMessageTable("Function")
	assert.NilError(t, err)

	for _, name := range []string{"namespace", "deployment", "tenant", "metadata", "scale", "oneshot"} {
		assert.Assert(t, strings.Contains(out, "`"+name+"`"),
			"Function table missing field %q\n%s", name, out)
	}
}

func TestRenderMessageTable_RowsOrderedByFieldNumber(t *testing.T) {
	t.Parallel()

	out, err := renderMessageTable("Function")
	assert.NilError(t, err)

	prev := -1
	for _, name := range []string{"namespace", "deployment", "tenant", "metadata", "scale", "oneshot"} {
		idx := strings.Index(out, "`"+name+"`")
		assert.Assert(t, idx > prev, "%s out of order", name)
		prev = idx
	}
}

func TestRenderMessageTable_MessageTypedFieldsRenderFullName(t *testing.T) {
	t.Parallel()

	t.Run("Function.scale renders as Scale (skipper. stripped)", func(t *testing.T) {
		t.Parallel()
		out, err := renderMessageTable("Function")
		assert.NilError(t, err)
		row := messageRowFor(t, out, "scale")
		assert.Assert(t, strings.Contains(row, " Scale "),
			"scale row should render bare type 'Scale': %q", row)
		assert.Assert(t, !strings.Contains(row, "skipper.Scale"),
			"scale row should not include skipper. prefix: %q", row)
	})

	t.Run("Instance.assigned_at renders as google.protobuf.Timestamp", func(t *testing.T) {
		t.Parallel()
		out, err := renderMessageTable("Instance")
		assert.NilError(t, err)
		row := messageRowFor(t, out, "assigned_at")
		assert.Assert(t, strings.Contains(row, "google.protobuf.Timestamp"),
			"assigned_at row should include the full Timestamp name: %q", row)
	})
}

// (a) unknown message name to the shortcode.
func TestRenderMessageTable_UnknownMessageFails(t *testing.T) {
	t.Parallel()

	_, err := renderMessageTable("Event")
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMessageUnknown))
	assert.ErrorContains(t, err, "Event")
	// The error should also list the registered messages.
	for _, msg := range []string{"Function", "Scale", "Instance", "Heartbeat"} {
		assert.Assert(t, strings.Contains(err.Error(), msg),
			"error message should list registered message %q: %v", msg, err)
	}
}

// (b) proto field with no description.
func TestRenderMessageTable_MissingDescriptionFails(t *testing.T) {
	t.Parallel()

	gappy := map[string]string{} // empty descriptions
	_, err := renderMessageRows("Function", gappy, messageTableRegistry)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMessageDescriptionDrift))
	assert.ErrorContains(t, err, "Function.namespace")
}

// (c) description key for nonexistent field.
func TestRenderMessageTable_ExtraDescriptionFails(t *testing.T) {
	t.Parallel()

	descriptions := map[string]string{
		"Function.namespace":  "x",
		"Function.deployment": "x",
		"Function.tenant":     "x",
		"Function.metadata":   "x",
		"Function.scale":      "x",
		"Function.oneshot":    "x",
		"Function.bogus":      "extraneous",
	}
	_, err := renderMessageRows("Function", descriptions, messageTableRegistry)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMessageDescriptionDrift))
	assert.ErrorContains(t, err, "Function.bogus")
}

// (d) description-map key for unregistered message.
func TestRenderMessageTable_UnregisteredKeyFails(t *testing.T) {
	t.Parallel()

	descriptions := map[string]string{
		"Function.namespace":  "x",
		"Function.deployment": "x",
		"Function.tenant":     "x",
		"Function.metadata":   "x",
		"Function.scale":      "x",
		"Function.oneshot":    "x",
		"Event.type":          "wrong message",
	}
	_, err := renderMessageRows("Function", descriptions, messageTableRegistry)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMessageDescriptionDrift))
	assert.ErrorContains(t, err, "Event")
	for _, msg := range []string{"Function", "Scale", "Instance", "Heartbeat"} {
		assert.Assert(t, strings.Contains(err.Error(), msg),
			"error should list registered messages: %v", err)
	}
}

// messageRowFor returns the table line whose first cell is the
// backticked proto field name. Mirrors rowFor from
// flagtable_test.go but matches the message-table cell shape
// (no leading `--`).
func messageRowFor(t *testing.T, table, field string) string {
	t.Helper()
	needle := "`" + field + "`"
	for line := range strings.SplitSeq(table, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no row for %s in:\n%s", field, table)
	return ""
}

func TestBuild_MessageTableShortcodeRendersAllFields(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: API
---

## Function

{{< messageTable Function >}}

## Scale

{{< messageTable Scale >}}

## Instance

{{< messageTable Instance >}}

## Heartbeat

{{< messageTable Heartbeat >}}
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "api.md"), []byte(page), 0o644))

	err := Build(srcDir, outDir, BuildOptions{})
	assert.NilError(t, err)

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "api", "index.html"))
	assert.NilError(t, err)
	out := string(htmlBytes)

	for _, field := range []string{"namespace", "deployment", "tenant", "min_instances", "max_instances", "addr", "ready_at", "in_flight_requests"} {
		assert.Assert(t, strings.Contains(out, field),
			"rendered HTML missing %q", field)
	}
}
