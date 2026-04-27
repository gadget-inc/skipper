package key

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
)

// Key is a typed key for creating structured log and telemetry attributes.
type Key[V any] struct {
	Names
	toSlogAttr   func(v V) slog.Attr
	otelOverride func(v V) []attribute.KeyValue
	cache        func(v V) Attr
}

// Slog returns a slog.Attr for use in logging calls.
// This is the most efficient method when only logging is needed.
func (k *Key[V]) Slog(v V) slog.Attr {
	return k.toSlogAttr(v)
}

// Attr returns both slog and OTel representations, pre-computed for efficiency.
// Use this with telemetry.With() when propagating attributes to both
// logging and tracing contexts.
//
// The slog.Value is resolved eagerly so the returned Attr never retains a
// LogValuer's underlying pointer, keeping the Attr safe to cache (see
// NewCached) without leaking the source value past its natural lifetime.
func (k *Key[V]) Attr(v V) Attr {
	if k.cache != nil {
		return k.cache(v)
	}
	return k.buildAttr(v)
}

// buildAttr is the cache-miss path; NewCached wires it as memoizedCache.build.
func (k *Key[V]) buildAttr(v V) Attr {
	slogAttr := k.toSlogAttr(v)
	slogAttr.Value = slogAttr.Value.Resolve()
	if k.otelOverride != nil {
		return Attr{Slog: slogAttr, Otel: k.otelOverride(v)}
	}
	return Attr{Slog: slogAttr, Otel: slogAttrToOtelAttrs(slogAttr)}
}

// Attr holds a pre-computed slog attribute and its OTel equivalent
// for efficient telemetry context propagation.
type Attr struct {
	Slog slog.Attr
	Otel []attribute.KeyValue
}
