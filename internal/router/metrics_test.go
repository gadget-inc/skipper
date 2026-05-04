package router

import (
	"regexp"
	"slices"
	"testing"

	"gotest.tools/v3/assert"
)

// TestMetricsSliceCoversAllRouterRegistrations asserts the captured
// metric slice exposes every metric the router registers. Adding a
// metric without going through the [metric.Set] wrapper would leave
// the slice short and fail this test.
func TestMetricsSliceCoversAllRouterRegistrations(t *testing.T) {
	t.Parallel()

	want := []string{
		"heartbeats_total",
		"requests_in_flight",
		"requests_total",
	}

	got := []string{}
	for _, m := range Metrics() {
		got = append(got, m.Name)
	}
	slices.Sort(got)

	assert.DeepEqual(t, got, want)
}

// TestRouterMetricsHelpFormat asserts each Help string is a single
// sentence ending in `.`, optionally with a trailing parenthetical
// clause, and at most 120 characters long.
func TestRouterMetricsHelpFormat(t *testing.T) {
	t.Parallel()

	helpRe := regexp.MustCompile(`^[^.]+( \([^)]*\))?\.$`)
	for _, m := range Metrics() {
		t.Run(m.Name, func(t *testing.T) {
			t.Parallel()
			assert.Assert(t, m.Help != "", "%s: empty Help", m.Name)
			assert.Assert(t, len(m.Help) <= 120,
				"%s: Help over 120 chars (%d): %q", m.Name, len(m.Help), m.Help)
			assert.Assert(t, helpRe.MatchString(m.Help),
				"%s: Help not single sentence: %q", m.Name, m.Help)
		})
	}
}
