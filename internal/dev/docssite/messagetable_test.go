package docssite

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/reflect/protoreflect"
	"gotest.tools/v3/assert"
)

func TestRenderMessageTable_HeaderShape(t *testing.T) {
	t.Parallel()

	out, err := renderMessageTable("Assignment")
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

	out, err := renderMessageTable("Assignment")
	assert.NilError(t, err)

	for _, name := range []string{"namespace", "deployment", "tenant", "metadata", "scale", "oneshot"} {
		assert.Assert(t, strings.Contains(out, "`"+name+"`"),
			"Assignment table missing field %q\n%s", name, out)
	}
}

func TestRenderMessageTable_RowsOrderedByFieldNumber(t *testing.T) {
	t.Parallel()

	out, err := renderMessageTable("Assignment")
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

	t.Run("Assignment.scale renders as Scale (skipper. stripped)", func(t *testing.T) {
		t.Parallel()
		out, err := renderMessageTable("Assignment")
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

// TestRenderMessageTable_RepeatedAndEnumFieldsKeepSemanticType
// pins the type-column rendering for proto features the original
// hand-typed docs surfaced: `repeated` lists carry the qualifier,
// and enum fields render their declared enum name instead of the
// generic `"enum"` kind string.
func TestRenderMessageTable_RepeatedAndEnumFieldsKeepSemanticType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		message  string
		field    string
		wantCell string
	}{
		// repeated string
		{"GetInstanceRequest", "exclude_instance_names", "repeated string"},
		// repeated message (with the local skipper. prefix stripped)
		{"HeartbeatRequest", "heartbeats", "repeated Heartbeat"},
		// repeated string in a different message
		{"HeartbeatRequest", "forwarded_for", "repeated string"},
		// repeated message in ScaleResponse
		{"ScaleResponse", "instances", "repeated Instance"},
		// enum field rendered with declared enum name
		{"ScaleRequest", "reason", "ScaleReason"},
	}

	for _, tc := range cases {
		t.Run(tc.message+"."+tc.field, func(t *testing.T) {
			t.Parallel()
			out, err := renderMessageTable(tc.message)
			assert.NilError(t, err)
			row := messageRowFor(t, out, tc.field)
			assert.Assert(t, strings.Contains(row, " "+tc.wantCell+" "),
				"%s.%s row should render type %q in its own cell: %q",
				tc.message, tc.field, tc.wantCell, row)
		})
	}
}

// (a) unknown message name to the shortcode.
func TestRenderMessageTable_UnknownMessageFails(t *testing.T) {
	t.Parallel()

	_, err := renderMessageTable("Event")
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMessageUnknown))
	assert.ErrorContains(t, err, "Event")
	// The error should also list every registered message.
	for _, msg := range messageTableRegistry {
		assert.Assert(t, strings.Contains(err.Error(), msg),
			"error message should list registered message %q: %v", msg, err)
	}
}

// TestRenderMessageTable_RequestResponseRegistry asserts every
// request/response message added in the round-2 registry expansion
// renders end-to-end: it's whitelisted, has a wired descriptor, and
// its fields all have description-map entries.
func TestRenderMessageTable_RequestResponseRegistry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		fields []string
	}{
		{"GetInstanceRequest", []string{"assignment", "exclude_instance_names"}},
		{"GetInstanceResponse", []string{"instance"}},
		{"HeartbeatRequest", []string{"router_ip", "heartbeats", "forwarded_for"}},
		{"ScaleRequest", []string{"assignment", "desired_instances", "reason"}},
		{"ScaleResponse", []string{"instances"}},
		{"ReleaseInstanceRequest", []string{"instance"}},
		{"GetClusterStateResponse", []string{"cluster_state"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := renderMessageTable(tc.name)
			assert.NilError(t, err)
			for _, f := range tc.fields {
				assert.Assert(t, strings.Contains(out, "`"+f+"`"),
					"%s table missing field %q\n%s", tc.name, f, out)
			}
		})
	}
}

// (b) proto field with no description.
func TestRenderMessageTable_MissingDescriptionFails(t *testing.T) {
	t.Parallel()

	gappy := map[string]string{} // empty descriptions
	_, err := renderMessageRows("Assignment", gappy, messageTableRegistry)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMessageDescriptionDrift))
	assert.ErrorContains(t, err, "Assignment.namespace")
}

// (c) description key for nonexistent field.
func TestRenderMessageTable_ExtraDescriptionFails(t *testing.T) {
	t.Parallel()

	// Start from the live description map so adding fields to Assignment
	// doesn't trip the missing-description check before the "extra key"
	// check this case actually exercises.
	descriptions := make(map[string]string, len(messageTableDescriptions)+1)
	maps.Copy(descriptions, messageTableDescriptions)
	descriptions["Assignment.bogus"] = "extraneous"

	_, err := renderMessageRows("Assignment", descriptions, messageTableRegistry)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMessageDescriptionDrift))
	assert.ErrorContains(t, err, "Assignment.bogus")
}

// (d) description-map key for unregistered message.
func TestRenderMessageTable_UnregisteredKeyFails(t *testing.T) {
	t.Parallel()

	descriptions := map[string]string{
		"Assignment.namespace":  "x",
		"Assignment.deployment": "x",
		"Assignment.tenant":     "x",
		"Assignment.metadata":   "x",
		"Assignment.scale":      "x",
		"Assignment.oneshot":    "x",
		"Event.type":            "wrong message",
	}
	_, err := renderMessageRows("Assignment", descriptions, messageTableRegistry)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMessageDescriptionDrift))
	assert.ErrorContains(t, err, "Event")
	for _, msg := range []string{"Assignment", "Scale", "Instance", "Heartbeat"} {
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

// TestMessageTableRegistry_CoversControllerServiceMessages asserts
// every ControllerService method's Request and Response message
// (with at least one field) is registered. A future RPC method
// that introduces a non-empty request/response shape without
// updating the registry fails this test.
func TestMessageTableRegistry_CoversControllerServiceMessages(t *testing.T) {
	t.Parallel()

	svc := skipper.File_controller_proto.Services().ByName("ControllerService")
	assert.Assert(t, svc != nil, "ControllerService not found on file descriptor")

	methods := svc.Methods()
	for i := range methods.Len() {
		m := methods.Get(i)
		for _, io := range []protoreflect.MessageDescriptor{m.Input(), m.Output()} {
			if io.Fields().Len() == 0 {
				continue
			}
			name := string(io.Name())
			assert.Assert(t, slices.Contains(messageTableRegistry, name),
				"messageTableRegistry missing %s (referenced by %s)",
				name, m.Name())
		}
	}
}

func TestBuild_MessageTableShortcodeRendersAllFields(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: API
---

## Assignment

{{< messageTable Assignment >}}

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
