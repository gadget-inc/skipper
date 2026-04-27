package key

import (
	"net/textproto"
	"strings"
)

// Names holds a key's renderings across different contexts: logs, HTTP
// headers, Kubernetes labels and annotations, and JSON patches.
type Names struct {
	// Name is the canonical name for the key in OTEL dot-snake format.
	// e.g. "http.response.status_code"
	Name string

	// Header is the HTTP header name for the key.
	// e.g. "http.response.status_code" -> "X-Skipper-Http-Response-Status-Code"
	Header string

	// Label is the Kubernetes label name for the key. Annotations use the same
	// string, so map reads/writes against pod.Annotations also key on Label.
	// e.g. "http.response.status_code" -> "skipper/http-response-status-code"
	Label string

	// PatchLabel is the JSON-Pointer path under /metadata/labels for the key.
	// e.g. "http.response.status_code" -> "/metadata/labels/skipper~1http-response-status-code"
	PatchLabel string

	// PatchAnnotation is the JSON-Pointer path under /metadata/annotations for the key.
	// e.g. "http.response.status_code" -> "/metadata/annotations/skipper~1http-response-status-code"
	PatchAnnotation string
}

// newNames creates a Names from a dot-snake-cased name (OTEL attribute style).
// e.g. "http.response.status_code"
func newNames(name string) Names {
	kebab := strings.NewReplacer(".", "-", "_", "-").Replace(name)
	escaped := strings.ReplaceAll(strings.ReplaceAll(kebab, "~", "~0"), "/", "~1")
	return Names{
		Name:            name,
		Header:          "X-Skipper-" + textproto.CanonicalMIMEHeaderKey(kebab),
		Label:           "skipper/" + kebab,
		PatchLabel:      "/metadata/labels/skipper~1" + escaped,
		PatchAnnotation: "/metadata/annotations/skipper~1" + escaped,
	}
}
