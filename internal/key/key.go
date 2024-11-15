package key

import (
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

type Key[v any] interface {
	Field(value v) slog.Attr
	Attribute(value v) attribute.KeyValue
}

type key struct {
	name        string
	underscored string
	Header      string
	Label       string
	PatchLabel  string
}

func new(name string) key {
	return key{
		name:        name,
		Header:      "x-fusion-" + name,
		Label:       "fusion/" + name,
		PatchLabel:  "/metadata/labels/" + "fusion~1" + strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1"),
		underscored: strings.ReplaceAll(name, "-", "_"),
	}
}
