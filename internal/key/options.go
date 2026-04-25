package key

import "go.opentelemetry.io/otel/attribute"

// Option configures a Key during construction. Apply via [New] or [NewCached].
type Option[V any] func(*Key[V])

// WithOtel overrides Otel attribute construction for the key, bypassing the
// slog.Value -> []attribute.KeyValue conversion. Use it on hot paths where
// producing OTel attributes directly avoids the slog.GroupValue detour. The
// Slog representation is unaffected -- it still derives from the constructor's
// valueOf.
func WithOtel[V any](otelFn func(V) []attribute.KeyValue) Option[V] {
	return func(k *Key[V]) {
		k.otelOverride = otelFn
	}
}
