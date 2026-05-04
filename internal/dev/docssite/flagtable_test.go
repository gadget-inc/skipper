package docssite

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestRenderFlagTable_UnknownSection(t *testing.T) {
	t.Parallel()
	_, err := renderFlagTable("nope")
	assert.Assert(t, errors.Is(err, errUnknownFlagSection))
}

// TestRenderFlagTable_ControllerHasExpectedFlags asserts the controller
// section renders a row for every `flag:"…"` tag declared on
// controller.Config, including the dev-only flags `web-template-dir`
// and `single-controller-mode`.
func TestRenderFlagTable_ControllerHasExpectedFlags(t *testing.T) {
	t.Parallel()
	out, err := renderFlagTable("controller")
	assert.NilError(t, err)

	expected := []string{
		"host", "port", "shutdown-timeout", "namespace", "pod-ip",
		"kubeconfig-qps", "kubeconfig-burst", "paseto-private-key",
		"heartbeat-timeout", "scale-interval", "hpa-tolerance",
		"hpa-initial-readiness-delay", "hpa-downscale-stabilization",
		"hash-ring-wait-time", "function-namespaces",
		"function-assign-path", "function-assign-timeout",
		"max-concurrent-stale-replacements", "skip-forbidden-namespaces",
		"web-port", "web-template-dir", "single-controller-mode",
	}
	for _, flag := range expected {
		assert.Assert(t, strings.Contains(out, "`--"+flag+"`"),
			"controller table missing --%s\n%s", flag, out)
	}
}

func TestRenderFlagTable_RouterHasExpectedFlags(t *testing.T) {
	t.Parallel()
	out, err := renderFlagTable("router")
	assert.NilError(t, err)

	expected := []string{
		"host", "port", "shutdown-timeout", "pod-ip",
		"heartbeat-interval", "max-round-trip-attempts",
		"round-trip-retry-min-timeout", "round-trip-retry-max-timeout",
		"controller-service-host", "controller-port",
		"controller-headless-service-host",
	}
	for _, flag := range expected {
		assert.Assert(t, strings.Contains(out, "`--"+flag+"`"),
			"router table missing --%s\n%s", flag, out)
	}
}

// TestRenderFlagTable_SharedConcatenatesInBindingOrder asserts the
// shared section emits log flags first, then pprof, then telemetry —
// matching the slice order of the binding list.
func TestRenderFlagTable_SharedConcatenatesInBindingOrder(t *testing.T) {
	t.Parallel()
	out, err := renderFlagTable("shared")
	assert.NilError(t, err)

	logIdx := strings.Index(out, "`--log-level`")
	pprofIdx := strings.Index(out, "`--pprof`")
	telemIdx := strings.Index(out, "`--telemetry`")

	assert.Assert(t, logIdx >= 0 && pprofIdx >= 0 && telemIdx >= 0,
		"shared table missing one of --log-level/--pprof/--telemetry\n%s", out)
	assert.Assert(t, logIdx < pprofIdx,
		"log flags must appear before pprof flags\n%s", out)
	assert.Assert(t, pprofIdx < telemIdx,
		"pprof flags must appear before telemetry flags\n%s", out)
}

// TestRenderFlagTable_DeclarationOrder asserts fields appear in
// reflect.Type.Field(i) order within a struct.
func TestRenderFlagTable_DeclarationOrder(t *testing.T) {
	t.Parallel()
	out, err := renderFlagTable("router")
	assert.NilError(t, err)
	hostIdx := strings.Index(out, "`--host`")
	portIdx := strings.Index(out, "`--port`")
	shutdownIdx := strings.Index(out, "`--shutdown-timeout`")
	assert.Assert(t, hostIdx < portIdx)
	assert.Assert(t, portIdx < shutdownIdx)
}

// TestRenderFlagTable_RequiredAndSensitiveDefaults asserts the
// (required) marker, the , sensitive append, and their combination
// render correctly for paseto-private-key.
func TestRenderFlagTable_RequiredAndSensitiveDefaults(t *testing.T) {
	t.Parallel()
	out, err := renderFlagTable("controller")
	assert.NilError(t, err)

	pasetoRow := rowFor(t, out, "paseto-private-key")
	assert.Assert(t, strings.Contains(pasetoRow, "(required)"),
		"expected (required) on paseto row: %q", pasetoRow)
	assert.Assert(t, strings.Contains(pasetoRow, ", sensitive"),
		"expected `, sensitive` on paseto row: %q", pasetoRow)

	namespaceRow := rowFor(t, out, "namespace")
	assert.Assert(t, strings.Contains(namespaceRow, "(required)"),
		"expected (required) on namespace row: %q", namespaceRow)
	assert.Assert(t, !strings.Contains(namespaceRow, "sensitive"),
		"namespace row should not be sensitive: %q", namespaceRow)
}

// TestRenderFlagTable_EmptyDefaultRendersBlank asserts a non-required
// field with no `default` tag emits an empty default cell.
func TestRenderFlagTable_EmptyDefaultRendersBlank(t *testing.T) {
	t.Parallel()
	out, err := renderFlagTable("router")
	assert.NilError(t, err)

	row := rowFor(t, out, "controller-headless-service-host")
	cells := splitRow(row)
	assert.Equal(t, len(cells), 4, "row %q should have 4 cells", row)
	assert.Equal(t, cells[2], "", "default cell should be empty")
}

// TestRenderFlagTable_TypeRewrites asserts time.Duration → duration,
// []string → string list, LogLevel → string, PasetoPrivateKey →
// string, and that primitives pass through unchanged.
func TestRenderFlagTable_TypeRewrites(t *testing.T) {
	t.Parallel()
	out, err := renderFlagTable("controller")
	assert.NilError(t, err)
	cells := func(flag string) []string { return splitRow(rowFor(t, out, flag)) }

	assert.Equal(t, cells("port")[1], "int")
	assert.Equal(t, cells("host")[1], "string")
	assert.Equal(t, cells("shutdown-timeout")[1], "duration")
	assert.Equal(t, cells("function-namespaces")[1], "string list")
	assert.Equal(t, cells("paseto-private-key")[1], "string")
	assert.Equal(t, cells("kubeconfig-qps")[1], "float32")
	assert.Equal(t, cells("hpa-tolerance")[1], "float64")
	assert.Equal(t, cells("skip-forbidden-namespaces")[1], "bool")

	sharedOut, err := renderFlagTable("shared")
	assert.NilError(t, err)
	cellsShared := func(flag string) []string { return splitRow(rowFor(t, sharedOut, flag)) }
	assert.Equal(t, cellsShared("log-level")[1], "string")
}

// TestRenderFlagTable_TableShape asserts the output is a markdown table
// with a header row and a separator row before the data rows.
func TestRenderFlagTable_TableShape(t *testing.T) {
	t.Parallel()
	out, err := renderFlagTable("router")
	assert.NilError(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Assert(t, len(lines) >= 3, "table must have header, separator, and at least one row")

	header := strings.ToLower(lines[0])
	for _, col := range []string{"flag", "type", "default", "description"} {
		assert.Assert(t, strings.Contains(header, col),
			"header missing %q: %q", col, lines[0])
	}

	sep := strings.TrimSpace(lines[1])
	assert.Assert(t, strings.HasPrefix(sep, "|"))
	assert.Assert(t, strings.Contains(sep, "---"))
}

// TestRenderFlagRows_MalformedRequiredReturnsWrappedError uses an
// internal helper so we can feed a test-only struct with a bogus
// `required:"yes"` tag without polluting the real bindings.
func TestRenderFlagRows_MalformedRequiredReturnsWrappedError(t *testing.T) {
	t.Parallel()
	type bad struct {
		Field string `flag:"x" required:"yes"`
	}
	_, err := renderFlagRows([]any{bad{}})
	assert.ErrorContains(t, err, "required")
}

func TestRenderFlagRows_MalformedSensitiveReturnsWrappedError(t *testing.T) {
	t.Parallel()
	type bad struct {
		Field string `flag:"x" sensitive:"yes"`
	}
	_, err := renderFlagRows([]any{bad{}})
	assert.ErrorContains(t, err, "sensitive")
}

// TestRenderFlagRows_FieldsWithoutFlagTagAreSkipped asserts a struct
// field without a `flag:` tag emits no row.
func TestRenderFlagRows_FieldsWithoutFlagTagAreSkipped(t *testing.T) {
	t.Parallel()
	type s struct {
		Tagged   string `flag:"keep"`
		Untagged string
		Empty    string `flag:""`
	}
	out, err := renderFlagRows([]any{s{}})
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(out, "`--keep`"))
	assert.Assert(t, !strings.Contains(out, "Untagged"))
	assert.Assert(t, !strings.Contains(out, "Empty"))
}

// TestRenderFlagTable_ErrorsWrapWithSection asserts errors include the
// requested section name in the wrap.
func TestRenderFlagTable_ErrorsWrapWithSection(t *testing.T) {
	t.Parallel()
	_, err := renderFlagTable("typo")
	assert.ErrorContains(t, err, "typo")
}

// TestRenderFlagRows_DefaultEscapesPipe ensures a default value
// containing a literal `|` does not break the markdown table.
func TestRenderFlagRows_DefaultEscapesPipe(t *testing.T) {
	t.Parallel()
	type s struct {
		Field string `flag:"piped" description:"a | description with a pipe" default:"a|b"`
	}
	out, err := renderFlagRows([]any{s{}})
	assert.NilError(t, err)
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		// Each row must have exactly 4 columns plus the leading and
		// trailing pipes — so 5 unescaped pipes total.
		unescaped := countUnescapedPipes(line)
		assert.Equal(t, unescaped, 5, "row %q has %d unescaped pipes", line, unescaped)
	}
}

// rowFor returns the table line whose first cell is `--<flag>`. Fails
// the test if no such row is found.
func rowFor(t *testing.T, table, flag string) string {
	t.Helper()
	needle := "`--" + flag + "`"
	for line := range strings.SplitSeq(table, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no row for --%s in:\n%s", flag, table)
	return ""
}

// splitRow splits a markdown table row into its cell contents.
func splitRow(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	parts := splitOnUnescapedPipe(row)
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(strings.ReplaceAll(p, `\|`, "|"))
	}
	return out
}

func splitOnUnescapedPipe(s string) []string {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '|' {
			cur.WriteByte('\\')
			cur.WriteByte('|')
			i++
			continue
		}
		if s[i] == '|' {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	parts = append(parts, cur.String())
	return parts
}

func countUnescapedPipes(line string) int {
	n := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			i++
			continue
		}
		if line[i] == '|' {
			n++
		}
	}
	return n
}

// _ ensures the time import isn't lost when removing tests.
var _ = time.Duration(0)

// TestBuild_FlagtableShortcodeRendersAllStructFlags is the load-bearing
// regression test: it renders a fixture markdown body containing each
// of the three flagtable shortcodes, then asserts every `flag:"…"` tag
// declared on any bound config struct appears in the rendered HTML.
// A struct field that loses its tag silently breaks the docs; this
// test catches it.
func TestBuild_FlagtableShortcodeRendersAllStructFlags(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: Configuration Reference
---

## Controller configuration

{{< flagtable controller >}}

## Router configuration

{{< flagtable router >}}

## Shared configuration

{{< flagtable shared >}}
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "configuration.md"), []byte(page), 0o644))

	err := Build(srcDir, outDir, BuildOptions{})
	assert.NilError(t, err)

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "configuration", "index.html"))
	assert.NilError(t, err)
	out := string(htmlBytes)

	for _, b := range flagTableBindings {
		for _, s := range b.Structs {
			ty := reflect.TypeOf(s)
			for f := range ty.Fields() {
				flag := f.Tag.Get("flag")
				if flag == "" {
					continue
				}
				assert.Assert(t, strings.Contains(out, "--"+flag),
					"section %q missing --%s in rendered HTML", b.Section, flag)
			}
		}
	}
}

// TestBuild_FlagtableShortcodeUnknownSectionFails asserts an unknown
// section slug surfaces as a build-time error rather than a silent
// no-op.
func TestBuild_FlagtableShortcodeUnknownSectionFails(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: Bad
---

{{< flagtable typo >}}
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "bad.md"), []byte(page), 0o644))

	err := Build(srcDir, outDir, BuildOptions{})
	assert.ErrorContains(t, err, "typo")
}
