package key

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
)

// Key is a typed key for creating structured log and telemetry attributes.
type Key[V any] struct {
	Identifier
	toSlogAttr func(v V) slog.Attr
}

// Slog returns a slog.Attr for use in logging calls.
// This is the most efficient method when only logging is needed.
func (k Key[V]) Slog(v V) slog.Attr {
	return k.toSlogAttr(v)
}

// Otel returns OpenTelemetry attributes for use in span creation.
// Groups are flattened using dot notation. This is efficient when only
// tracing attributes are needed without logging.
func (k Key[V]) Otel(v V) []attribute.KeyValue {
	return slogAttrToOtelAttrs(k.Slog(v))
}

// Attr returns both slog and OTel representations, pre-computed for efficiency.
// Use this with telemetry.With() when propagating attributes to both
// logging and tracing contexts.
//
// The slog.Value is resolved eagerly so the returned Attr never retains a
// LogValuer's underlying pointer. This keeps the Attr safe to cache (see
// key.Memoized) without leaking the source value past its natural lifetime,
// and it saves the re-resolution cost at every subsequent log emit.
func (k Key[V]) Attr(v V) Attr {
	slogAttr := k.toSlogAttr(v)
	slogAttr.Value = slogAttr.Value.Resolve()
	return Attr{
		Slog: slogAttr,
		Otel: slogAttrToOtelAttrs(slogAttr),
	}
}

// Attr holds a pre-computed slog attribute and its OTel equivalent
// for efficient telemetry context propagation.
type Attr struct {
	Slog slog.Attr
	Otel []attribute.KeyValue
}
