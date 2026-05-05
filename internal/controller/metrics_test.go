package controller

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	"gotest.tools/v3/assert"
)

// TestMetricsSliceCoversAllControllerRegistrations asserts the
// captured metric slice exposes every metric the controller
// registers. Adding a metric without going through the [metric.Set]
// wrapper would leave the slice short and fail this test.
func TestMetricsSliceCoversAllControllerRegistrations(t *testing.T) {
	t.Parallel()

	want := []string{
		"assignments_total",
		"heartbeats_total",
		"informer_event_lag_seconds",
		"informer_events_total",
		"informer_last_event_time_seconds",
		"scale_downs_total",
		"scale_ups_total",
		"waiting_for_unassigned_pods",
	}

	got := []string{}
	for _, m := range Metrics() {
		got = append(got, m.Name)
	}
	slices.Sort(got)

	assert.DeepEqual(t, got, want)
}

// TestControllerMetricsHelpFormat asserts each Help string is a
// single sentence ending in `.`, optionally with a trailing
// parenthetical clause, and at most 120 characters long. The
// rendered docs cite Help verbatim; a multi-sentence or overlong
// Help would blow out the Purpose column.
func TestControllerMetricsHelpFormat(t *testing.T) {
	t.Parallel()

	for _, m := range Metrics() {
		t.Run(m.Name, func(t *testing.T) {
			t.Parallel()
			assert.NilError(t, validateHelp(m.Help),
				"%s: %q", m.Name, m.Help)
		})
	}
}

// helpFormatRe matches a single sentence ending in `.`, optionally
// with a trailing `(...)` parenthetical clause that may itself
// contain interior punctuation.
var helpFormatRe = regexp.MustCompile(`^[^.]+( \([^)]*\))?\.$`)

func validateHelp(h string) error {
	if h == "" {
		return fmt.Errorf("help: empty")
	}
	if len(h) > 120 {
		return fmt.Errorf("help: over 120 chars (%d)", len(h))
	}
	if !helpFormatRe.MatchString(h) {
		return fmt.Errorf("help: not a single sentence ending in '.' (optional trailing parenthetical OK)")
	}
	return nil
}
