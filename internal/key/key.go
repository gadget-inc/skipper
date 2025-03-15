package key

import (
	"log/slog"
	"net/textproto"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

type Key[v any] interface {
	Field(value v) slog.Attr
	Attribute(value v) attribute.KeyValue
}

type GroupKey[v any] interface {
	Field(value v) slog.Attr
	Attributes(value v) []attribute.KeyValue
	AttributesSet(value v) attribute.Set
}

type key struct {
	KebabCased      string
	Underscored     string
	Header          string
	Label           string
	PatchLabel      string
	PatchAnnotation string
}

func new(kebabCasedName string) key {
	return key{
		KebabCased:      kebabCasedName,
		Underscored:     strings.ReplaceAll(kebabCasedName, "-", "_"),
		Header:          "X-Skipper-" + textproto.CanonicalMIMEHeaderKey(kebabCasedName),
		Label:           "skipper/" + kebabCasedName,
		PatchLabel:      "/metadata/labels/skipper~1" + strings.ReplaceAll(strings.ReplaceAll(kebabCasedName, "~", "~0"), "/", "~1"),
		PatchAnnotation: "/metadata/annotations/skipper~1" + strings.ReplaceAll(strings.ReplaceAll(kebabCasedName, "~", "~0"), "/", "~1"),
	}
}
