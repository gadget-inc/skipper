package docssite

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gadget-inc/skipper/internal/skipper"
)

// errScaleReasonDescriptionDrift is returned (wrapped) when the
// scaleReasonDescriptions map disagrees with the canonical
// [skipper.ScaleReason_name] -- a missing entry, an extra entry, or
// any other shape mismatch. Callers detect drift with [errors.Is].
var errScaleReasonDescriptionDrift = errors.New("docssite: ScaleReason description drift")

// scaleReasonDescriptions is the prose for each ScaleReason value,
// keyed by enum number. The proto file does not carry this prose;
// keeping it in Go keeps the docs build self-contained while the
// drift check (renderScaleReasonRows) ensures it stays in lockstep
// with the enum values protoc generates.
var scaleReasonDescriptions = map[int32]string{
	0: "Default/unknown",
	1: "CPU usage triggered scaling",
	2: "No heartbeat within timeout",
	3: "Request count triggered scaling",
	4: "Memory usage triggered scaling",
	5: "No ready instances available",
}

// renderScaleReasonTable emits the markdown body of the ScaleReason
// reference table -- header, separator, and one data row per entry
// in [skipper.ScaleReason_name], ordered by ascending enum number.
func renderScaleReasonTable() (string, error) {
	return renderScaleReasonRows(skipper.ScaleReason_name, scaleReasonDescriptions)
}

// renderScaleReasonRows is the deterministic core of
// renderScaleReasonTable, exposed (unexported) so tests can feed
// drift fixtures without mutating the package-level description
// map.
func renderScaleReasonRows(names map[int32]string, descriptions map[int32]string) (string, error) {
	if err := checkScaleReasonDrift(names, descriptions); err != nil {
		return "", err
	}

	numbers := make([]int32, 0, len(names))
	for n := range names {
		numbers = append(numbers, n)
	}
	slices.Sort(numbers)

	var b strings.Builder
	b.WriteString("| Value | Number | Description |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, n := range numbers {
		fmt.Fprintf(&b, "| `%s` | %d | %s |\n",
			escapePipes(names[n]),
			n,
			escapePipes(descriptions[n]),
		)
	}
	return b.String(), nil
}

// checkScaleReasonDrift returns errScaleReasonDescriptionDrift
// (wrapped with the offending enum number(s)) when descriptions and
// names disagree on the set of keys. Both directions matter: a
// missing description silently drops prose; an extra description
// names a value the proto enum does not declare.
func checkScaleReasonDrift(names, descriptions map[int32]string) error {
	for n := range names {
		if _, ok := descriptions[n]; !ok {
			return fmt.Errorf("%w: missing description for ScaleReason number %d (%s)",
				errScaleReasonDescriptionDrift, n, names[n])
		}
	}
	for n := range descriptions {
		if _, ok := names[n]; !ok {
			return fmt.Errorf("%w: description references unknown ScaleReason number %d",
				errScaleReasonDescriptionDrift, n)
		}
	}
	return nil
}
