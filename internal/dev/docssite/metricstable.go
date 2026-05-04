package docssite

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/metric"
	"github.com/gadget-inc/skipper/internal/router"
)

// errMetricsComponentUnknown is returned (wrapped) when a
// metricsTable shortcode names a component not in
// metricsTableComponents.
var errMetricsComponentUnknown = errors.New("docssite: metricsTable shortcode references unknown component")

// metricsTableComponents maps a docs-side component name to the
// accessor that returns the package's captured [metric.Metric]
// slice. Adding a third metric-owning package means adding it here.
var metricsTableComponents = map[string]func() []metric.Metric{
	"controller": controller.Metrics,
	"router":     router.Metrics,
}

// renderMetricsTable returns the markdown body of the Prometheus
// metrics table for component (`controller` or `router`). Rows are
// sorted alphabetically by metric name; histogram metrics get a
// `(buckets: <first>--<last>)` clause appended to the Purpose
// column.
func renderMetricsTable(component string) (string, error) {
	accessor, ok := metricsTableComponents[component]
	if !ok {
		registered := make([]string, 0, len(metricsTableComponents))
		for k := range metricsTableComponents {
			registered = append(registered, k)
		}
		slices.Sort(registered)
		return "", fmt.Errorf("%w: %s (registered: %s)",
			errMetricsComponentUnknown, component, strings.Join(registered, ", "))
	}

	rows := slices.Clone(accessor())
	slices.SortFunc(rows, func(a, b metric.Metric) int {
		return strings.Compare(a.Name, b.Name)
	})

	var b strings.Builder
	b.WriteString("| Metric | Type | Labels | Purpose |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, m := range rows {
		row, err := renderMetricRow(m)
		if err != nil {
			return "", err
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// renderMetricRow formats one [metric.Metric] as a markdown table
// row. Vec metrics list their labels comma-separated; non-Vec
// metrics render `--` in the Labels column. Histogram metrics
// receive a trailing `(buckets: <first>--<last>)` clause -- formatted
// with [time.Duration] when the metric name ends in `_seconds`,
// plain numbers otherwise.
func renderMetricRow(m metric.Metric) (string, error) {
	labels := "--"
	if len(m.Labels) > 0 {
		labels = strings.Join(m.Labels, ", ")
	}

	purpose := m.Help
	if m.Type == metric.TypeHistogram && len(m.Buckets) > 0 {
		purpose = purpose + " (buckets: " + formatBucketRange(m.Name, m.Buckets) + ")"
	}

	return fmt.Sprintf("| `%s` | %s | %s | %s |",
		escapePipes(m.Name),
		m.Type.String(),
		escapePipes(labels),
		escapePipes(purpose),
	), nil
}

// formatBucketRange returns `<first>--<last>` for a non-empty
// bucket slice. When the metric name ends in `_seconds` the bounds
// are formatted via [time.Duration.String], matching the existing
// `0.0625s--512s` prose; otherwise they are formatted as plain
// numbers (no trailing zeros).
func formatBucketRange(name string, buckets []float64) string {
	first := buckets[0]
	last := buckets[len(buckets)-1]
	if strings.HasSuffix(name, "_seconds") {
		return formatSeconds(first) + "--" + formatSeconds(last)
	}
	return formatNumber(first) + "--" + formatNumber(last)
}

func formatSeconds(v float64) string {
	return time.Duration(v * float64(time.Second)).String()
}

func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
