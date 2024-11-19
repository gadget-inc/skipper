package key

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
)

type GroupKey[v any] interface {
	Field(value v) slog.Attr
	Attributes(value v) []attribute.KeyValue
}

type GroupValue interface {
	Fields() []slog.Attr
	Attributes() []attribute.KeyValue
}

type groupKey struct{ key }

var _ GroupKey[GroupValue] = groupKey{}

func (g groupKey) Field(gv GroupValue) slog.Attr {
	return slog.Any(string(g.Underscored), slog.GroupValue(gv.Fields()...))
}

func (g groupKey) Attributes(gv GroupValue) []attribute.KeyValue {
	attrs := gv.Attributes()
	for i := range attrs {
		attrs[i].Key = attribute.Key(g.KebabCased) + "." + attrs[i].Key
	}
	return attrs
}
