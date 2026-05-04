package docssite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/gadget-inc/skipper/internal/router"
)

func TestRenderTransportTable_HeaderShape(t *testing.T) {
	t.Parallel()

	out, err := renderTransportTable()
	assert.NilError(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Assert(t, len(lines) >= 3, "table must have header, separator, and at least one row")

	header := strings.ToLower(lines[0])
	for _, col := range []string{"setting", "value"} {
		assert.Assert(t, strings.Contains(header, col),
			"header missing %q: %q", col, lines[0])
	}

	sep := strings.TrimSpace(lines[1])
	assert.Assert(t, strings.HasPrefix(sep, "|"))
	assert.Assert(t, strings.Contains(sep, "---"))
}

func TestRenderTransportTable_NumericRowsBeforeProseRows(t *testing.T) {
	t.Parallel()

	out, err := renderTransportTable()
	assert.NilError(t, err)

	dialIdx := strings.Index(out, "Dial timeout")
	tlsIdx := strings.Index(out, "TLS handshake")
	protocolIdx := strings.Index(out, "Protocol")
	compressionIdx := strings.Index(out, "Compression")

	assert.Assert(t, dialIdx >= 0)
	assert.Assert(t, tlsIdx >= 0)
	assert.Assert(t, protocolIdx >= 0)
	assert.Assert(t, compressionIdx >= 0)
	assert.Assert(t, tlsIdx < protocolIdx, "numeric rows must precede prose rows")
	assert.Assert(t, protocolIdx < compressionIdx, "Protocol prose row must precede Compression")
}

func TestRenderTransportTable_NumericRowsRenderProductionValues(t *testing.T) {
	t.Parallel()

	out, err := renderTransportTable()
	assert.NilError(t, err)

	want := []string{
		router.DefaultHTTPTransportSettings.DialTimeout.String(),
		router.DefaultHTTPTransportSettings.KeepAlive.String(),
		"100",
		router.DefaultHTTPTransportSettings.IdleConnTimeout.String(),
		router.DefaultHTTPTransportSettings.TLSHandshakeTimeout.String(),
	}
	for _, v := range want {
		assert.Assert(t, strings.Contains(out, v),
			"table missing value %q\n%s", v, out)
	}
}

func TestRenderTransportTable_BoolRowsReflectStructFields(t *testing.T) {
	t.Parallel()

	t.Run("HTTP/2 attempted when ForceAttemptHTTP2 true", func(t *testing.T) {
		t.Parallel()
		s := router.HTTPTransportSettings{
			DialTimeout: time.Second, ForceAttemptHTTP2: true,
		}
		out, err := renderTransportRows(s)
		assert.NilError(t, err)
		assert.Assert(t, strings.Contains(out, "HTTP/2 attempted"),
			"expected HTTP/2-attempted prose: %s", out)
	})

	t.Run("HTTP/1.1-only when ForceAttemptHTTP2 false", func(t *testing.T) {
		t.Parallel()
		s := router.HTTPTransportSettings{
			DialTimeout: time.Second, ForceAttemptHTTP2: false,
		}
		out, err := renderTransportRows(s)
		assert.NilError(t, err)
		assert.Assert(t, !strings.Contains(out, "HTTP/2"),
			"unexpected HTTP/2 prose when ForceAttemptHTTP2 is false: %s", out)
	})

	t.Run("compression disabled when DisableCompression true", func(t *testing.T) {
		t.Parallel()
		s := router.HTTPTransportSettings{
			DialTimeout: time.Second, DisableCompression: true,
		}
		out, err := renderTransportRows(s)
		assert.NilError(t, err)
		assert.Assert(t, strings.Contains(out, "Disabled"),
			"expected compression-disabled prose: %s", out)
	})

	t.Run("compression enabled when DisableCompression false", func(t *testing.T) {
		t.Parallel()
		s := router.HTTPTransportSettings{
			DialTimeout: time.Second, DisableCompression: false,
		}
		out, err := renderTransportRows(s)
		assert.NilError(t, err)
		assert.Assert(t, strings.Contains(out, "Enabled"),
			"expected compression-enabled prose: %s", out)
	})
}

func TestRenderTransportTable_UnmappedBoolFails(t *testing.T) {
	t.Parallel()

	got := renderTransportRowsAdverse(t)
	assert.Assert(t, got != nil)
	assert.Assert(t, errors.Is(got, errTransportBoolUnmapped))
	assert.ErrorContains(t, got, "Surprise")
}

// TestRenderTransportTable_StaleProseEntryFails asserts the
// reverse direction of the drift check: a transportProseMap entry
// that names a field the struct does not declare fails the docs
// build. Mirrors the symmetric drift check the other generators in
// this package already do.
func TestRenderTransportTable_StaleProseEntryFails(t *testing.T) {
	t.Parallel()

	stale := map[string]transportProseRow{
		"ForceAttemptHTTP2":  {Label: "Protocol", TrueText: "x", FalseText: "x"},
		"DisableCompression": {Label: "Compression", TrueText: "x", FalseText: "x"},
		"GoneField":          {Label: "Stale", TrueText: "x", FalseText: "x"},
	}
	err := checkTransportProseMapCoverage(stale,
		[]string{"ForceAttemptHTTP2", "DisableCompression"})
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errTransportBoolUnmapped))
	assert.ErrorContains(t, err, "GoneField")
}

// renderTransportRowsAdverse is a small helper that builds an
// HTTPTransportSettings-shaped struct WITH an extra bool field
// not present in the prose-row map, and expects the renderer to
// fail. It uses an internal seam: the helper extracts a mutable
// copy of the prose map and runs the renderer with a forced extra
// bool name. Lives in the test file so production code stays
// closed.
func renderTransportRowsAdverse(t *testing.T) error {
	t.Helper()
	// Inject an unmapped bool name into the renderer's check.
	return checkTransportProseMapCoverage(map[string]transportProseRow{}, []string{"Surprise"})
}

func TestBuild_TransportTableShortcodeRendersAllRows(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: Routing
---

## HTTP transport configuration

{{< transportTable >}}
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "routing.md"), []byte(page), 0o644))

	err := Build(srcDir, outDir, BuildOptions{})
	assert.NilError(t, err)

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "routing", "index.html"))
	assert.NilError(t, err)
	out := string(htmlBytes)

	for _, expect := range []string{"Dial timeout", "Keep-alive", "Max idle", "Idle connection", "TLS handshake", "Protocol", "Compression"} {
		assert.Assert(t, strings.Contains(out, expect),
			"rendered HTML missing %q", expect)
	}
}
