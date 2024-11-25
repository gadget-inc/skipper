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

type key struct {
	KebabCased  string
	Underscored string
	Header      string
	Label       string
	PatchLabel  string
}

func new(kebabCasedName string) key {
	return key{
		KebabCased:  kebabCasedName,
		Underscored: strings.ReplaceAll(kebabCasedName, "-", "_"),
		Header:      "X-Fusion-" + textproto.CanonicalMIMEHeaderKey(kebabCasedName),
		Label:       "fusion/" + kebabCasedName,
		PatchLabel:  "/metadata/labels/" + "fusion~1" + strings.ReplaceAll(strings.ReplaceAll(kebabCasedName, "~", "~0"), "/", "~1"),
	}
}
