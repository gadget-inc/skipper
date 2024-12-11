package flag

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
)

func (f Flag[T]) Fields() []slog.Attr {
	if f.Sensitive {
		return []slog.Attr{
			slog.String("name", f.Name),
			slog.String("shorthand", f.Shorthand),
			slog.String("description", f.Description),
			slog.Bool("required", f.Required),
			slog.String("separator", f.Separator),
			slog.Bool("was_set", f.WasProvided),
		}
	}

	return []slog.Attr{
		slog.String("name", f.Name),
		slog.String("shorthand", f.Shorthand),
		slog.String("description", f.Description),
		slog.Bool("required", f.Required),
		slog.String("separator", f.Separator),
		slog.Bool("was_set", f.WasProvided),
		slog.String("value", f.String()),
	}
}

func (f Flag[T]) Attributes() []attribute.KeyValue {
	if f.Sensitive {
		return []attribute.KeyValue{
			attribute.String("name", f.Name),
			attribute.String("shorthand", f.Shorthand),
			attribute.String("description", f.Description),
			attribute.Bool("required", f.Required),
			attribute.String("separator", f.Separator),
			attribute.Bool("was_set", f.WasProvided),
		}
	}

	return []attribute.KeyValue{
		attribute.String("name", f.Name),
		attribute.String("shorthand", f.Shorthand),
		attribute.String("description", f.Description),
		attribute.Bool("required", f.Required),
		attribute.String("separator", f.Separator),
		attribute.Bool("was_set", f.WasProvided),
		attribute.String("value", f.String()),
	}
}
