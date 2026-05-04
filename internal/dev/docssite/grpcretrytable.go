package docssite

import (
	"fmt"
	"strings"

	"github.com/gadget-inc/skipper/internal/controller"
)

// renderGrpcRetryTable returns the markdown body of the gRPC retry
// policy table -- header row, separator, and one row per entry in
// [controller.DefaultRetryPolicies] in declaration order. The values
// are read at render time so the docs and the runtime gRPC service
// config cannot drift.
func renderGrpcRetryTable() (string, error) {
	var b strings.Builder
	b.WriteString("| Method | Max Attempts | Max Backoff | Retry On |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, p := range controller.DefaultRetryPolicies {
		b.WriteString(renderGrpcRetryRow(p))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// renderGrpcRetryRow formats one policy as a markdown table row.
// Exposed (unexported) so the test file can exercise the per-row
// formatting without rebuilding the whole table.
func renderGrpcRetryRow(p controller.RetryPolicy) string {
	return fmt.Sprintf(
		"| %s | %d | %s | %s |",
		escapePipes(p.Method),
		p.MaxAttempts,
		escapePipes(p.MaxBackoff.String()),
		escapePipes(strings.Join(p.RetryableStatusCodes, ", ")),
	)
}
