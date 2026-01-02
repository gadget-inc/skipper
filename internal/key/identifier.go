package key

import (
	"net/textproto"
	"strings"
)

// Identifier provides naming conventions for a key across different contexts:
// logs, HTTP headers, Kubernetes labels, and JSON patches.
type Identifier struct {
	// Name is the canonical name for the key in OTEL dot-snake format.
	// e.g. "http.response.status_code"
	Name string

	// Header is the HTTP header name for the key.
	// e.g. "http.response.status_code" -> "X-Skipper-Http-Response-Status-Code"
	Header string

	// Label is the Kubernetes label name for the key.
	// e.g. "http.response.status_code" -> "skipper/http-response-status-code"
	Label string

	// Annotation is the Kubernetes annotation name for the key.
	// e.g. "http.response.status_code" -> "skipper/http-response-status-code"
	Annotation string

	// PatchLabel is the Kubernetes patch label name for the key.
	// e.g. "http.response.status_code" -> "/metadata/labels/skipper~1http-response-status-code"
	PatchLabel string

	// PatchAnnotation is the Kubernetes patch annotation name for the key.
	// e.g. "http.response.status_code" -> "/metadata/annotations/skipper~1http-response-status-code"
	PatchAnnotation string
}

// newIdentifier creates an Identifier from a dot-snake-cased name (OTEL attribute style).
// e.g. "http.response.status_code"
func newIdentifier(name string) Identifier {
	// Convert dots and underscores to dashes for kebab-case contexts
	kebab := strings.NewReplacer(".", "-", "_", "-").Replace(name)
	return Identifier{
		Name:            name,
		Header:          "X-Skipper-" + textproto.CanonicalMIMEHeaderKey(kebab),
		Label:           "skipper/" + kebab,
		Annotation:      "skipper/" + kebab,
		PatchLabel:      "/metadata/labels/skipper~1" + strings.ReplaceAll(strings.ReplaceAll(kebab, "~", "~0"), "/", "~1"),
		PatchAnnotation: "/metadata/annotations/skipper~1" + strings.ReplaceAll(strings.ReplaceAll(kebab, "~", "~0"), "/", "~1"),
	}
}
