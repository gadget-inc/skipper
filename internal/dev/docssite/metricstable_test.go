package docssite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gadget-inc/skipper/internal/metric"
	"gotest.tools/v3/assert"
)

func TestRenderMetricsTable_HeaderShape(t *testing.T) {
	t.Parallel()

	out, err := renderMetricsTable("controller")
	assert.NilError(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Assert(t, len(lines) >= 3)

	header := strings.ToLower(lines[0])
	for _, col := range []string{"metric", "type", "labels", "purpose"} {
		assert.Assert(t, strings.Contains(header, col),
			"header missing %q: %q", col, lines[0])
	}
}

func TestRenderMetricsTable_RowsSortedAlphabetically(t *testing.T) {
	t.Parallel()

	for _, component := range []string{"controller", "router"} {
		t.Run(component, func(t *testing.T) {
			t.Parallel()
			out, err := renderMetricsTable(component)
			assert.NilError(t, err)

			prev := ""
			for _, line := range strings.Split(out, "\n") {
				if !strings.HasPrefix(line, "| `") {
					continue
				}
				name := strings.TrimPrefix(line, "| `")
				name = name[:strings.Index(name, "`")]
				assert.Assert(t, name > prev,
					"%s: row %q out of order after %q", component, name, prev)
				prev = name
			}
		})
	}
}

func TestRenderMetricsTable_RouterRowsExpected(t *testing.T) {
	t.Parallel()

	out, err := renderMetricsTable("router")
	assert.NilError(t, err)

	for _, name := range []string{"requests_total", "requests_in_flight", "heartbeats_total"} {
		assert.Assert(t, strings.Contains(out, "`"+name+"`"),
			"router metrics table missing %q\n%s", name, out)
	}
}

func TestRenderMetricsTable_ControllerRowsExpected(t *testing.T) {
	t.Parallel()

	out, err := renderMetricsTable("controller")
	assert.NilError(t, err)

	for _, name := range []string{
		"waiting_for_unassigned_pods",
		"assignments_total",
		"scale_ups_total",
		"scale_downs_total",
		"heartbeats_total",
		"informer_events_total",
		"informer_event_lag_seconds",
		"informer_last_event_time_seconds",
	} {
		assert.Assert(t, strings.Contains(out, "`"+name+"`"),
			"controller metrics table missing %q\n%s", name, out)
	}
}

// TestRenderMetricsTable_HistogramSecondsBuckets asserts a histogram
// metric whose name ends in `_seconds` gets a duration-formatted
// bucket suffix appended to the Purpose column, matching the
// existing `(buckets: 0.0625s--512s)` prose.
func TestRenderMetricsTable_HistogramSecondsBuckets(t *testing.T) {
	t.Parallel()

	row, err := renderMetricRow(metric.Metric{
		Name:    "informer_event_lag_seconds",
		Type:    metric.TypeHistogram,
		Labels:  []string{"resource", "event"},
		Help:    "Lag between Kubernetes object change and informer processing.",
		Buckets: []float64{0.0625, 0.125, 0.25, 512},
	})
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(row, "(buckets: 62.5ms--8m32s)"),
		"row missing duration-formatted buckets: %q", row)
}

// TestRenderMetricsTable_HistogramPlainBuckets asserts a histogram
// metric whose name does NOT end in `_seconds` formats buckets as
// plain numbers.
func TestRenderMetricsTable_HistogramPlainBuckets(t *testing.T) {
	t.Parallel()

	row, err := renderMetricRow(metric.Metric{
		Name:    "queue_depth",
		Type:    metric.TypeHistogram,
		Help:    "Items waiting in the queue.",
		Buckets: []float64{1, 4, 16, 64},
	})
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(row, "(buckets: 1--64)"),
		"row missing plain-number buckets: %q", row)
}

// TestRenderMetricsTable_NonHistogramNoBucketSuffix asserts non-histogram
// metrics never receive the bucket parenthetical.
func TestRenderMetricsTable_NonHistogramNoBucketSuffix(t *testing.T) {
	t.Parallel()

	row, err := renderMetricRow(metric.Metric{
		Name:   "assignments_total",
		Type:   metric.TypeCounter,
		Labels: []string{"function_deployment"},
		Help:   "Total pod assignments performed by the controller.",
	})
	assert.NilError(t, err)
	assert.Assert(t, !strings.Contains(row, "buckets"),
		"counter row should not mention buckets: %q", row)
}

// TestRenderMetricsTable_NonVecLabelsCell asserts a non-Vec metric
// renders an em-dash or hyphen placeholder in the Labels column,
// matching `informer_last_event_time_seconds` in the existing doc.
func TestRenderMetricsTable_NonVecLabelsCell(t *testing.T) {
	t.Parallel()

	row, err := renderMetricRow(metric.Metric{
		Name: "informer_last_event_time_seconds",
		Type: metric.TypeGauge,
		Help: "Unix timestamp of the last informer event processed.",
	})
	assert.NilError(t, err)
	cells := strings.Split(row, "|")
	// row format: "" | name | type | labels | purpose | "" -> 6 entries with leading/trailing empties
	assert.Equal(t, len(cells), 6, "row must have exactly four data cells: %q", row)
	labelsCell := strings.TrimSpace(cells[3])
	assert.Equal(t, labelsCell, "--",
		"non-Vec metric should render `--` in Labels column: %q", row)
}

func TestRenderMetricsTable_UnknownComponentFails(t *testing.T) {
	t.Parallel()

	_, err := renderMetricsTable("widget")
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errMetricsComponentUnknown))
	assert.ErrorContains(t, err, "widget")
	for _, c := range []string{"controller", "router"} {
		assert.Assert(t, strings.Contains(err.Error(), c),
			"error should list registered component %q: %v", c, err)
	}
}

func TestBuild_MetricsTableShortcodeRendersAllNames(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: Observability
---

## Controller

{{< metricsTable controller >}}

## Router

{{< metricsTable router >}}
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "obs.md"), []byte(page), 0o644))

	err := Build(srcDir, outDir, BuildOptions{})
	assert.NilError(t, err)

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "obs", "index.html"))
	assert.NilError(t, err)
	out := string(htmlBytes)

	// Spot-check one metric per package; the per-package coverage
	// tests in their own packages already assert the full set.
	for _, name := range []string{
		"waiting_for_unassigned_pods",
		"informer_event_lag_seconds",
		"requests_in_flight",
		"heartbeats_total",
	} {
		assert.Assert(t, strings.Contains(out, name),
			"rendered HTML missing %q", name)
	}
}
