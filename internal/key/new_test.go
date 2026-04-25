package key

import (
	"log/slog"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"gotest.tools/v3/assert"
)

func TestNew_BasicShapes(t *testing.T) {
	t.Parallel()
	k := New("greeting", func(v *testLogValuer) slog.Value {
		return slog.GroupValue(slog.String("name", v.name))
	})
	v := &testLogValuer{name: "hello"}

	t.Run("Slog returns named group attr", func(t *testing.T) {
		attr := k.Slog(v)
		assert.Equal(t, attr.Key, "greeting")
		assert.Equal(t, attr.Value.Kind(), slog.KindGroup)
		group := attr.Value.Group()
		assert.Equal(t, len(group), 1)
		assert.Equal(t, group[0].Key, "name")
		assert.Equal(t, group[0].Value.String(), "hello")
	})

	t.Run("Otel flattens group with dot notation", func(t *testing.T) {
		attrs := k.Otel(v)
		assert.Equal(t, len(attrs), 1)
		assert.Equal(t, attrs[0].Key, attribute.Key("greeting.name"))
		assert.Equal(t, attrs[0].Value.AsString(), "hello")
	})

	t.Run("Attr returns both Slog and Otel", func(t *testing.T) {
		attr := k.Attr(v)
		assert.Equal(t, attr.Slog.Key, "greeting")
		assert.Equal(t, len(attr.Otel), 1)
		assert.Equal(t, attr.Otel[0].Key, attribute.Key("greeting.name"))
	})

	t.Run("Identifier is embedded", func(t *testing.T) {
		assert.Equal(t, k.Name, "greeting")
		assert.Equal(t, k.Header, "X-Skipper-Greeting")
		assert.Equal(t, k.Label, "skipper/greeting")
	})
}

func TestNewCached_PerPointerMemoized(t *testing.T) {
	t.Parallel()
	var builds atomic.Int64
	k := NewCached("memo", func(v *testLogValuer) slog.Value {
		builds.Add(1)
		return slog.StringValue(v.name)
	})

	v := &testLogValuer{name: "cached"}
	first := k.Attr(v)
	for range 10 {
		got := k.Attr(v)
		assert.Assert(t, got.Slog.Equal(first.Slog), "Attr should return equivalent slog.Attr across calls")
	}
	assert.Equal(t, builds.Load(), int64(1), "valueOf should fire once per pointer")

	// Distinct pointer triggers a fresh build.
	other := &testLogValuer{name: "other"}
	_ = k.Attr(other)
	assert.Equal(t, builds.Load(), int64(2), "valueOf fires for each distinct pointer")
}

func TestWithOtel_OverridesOtelKeepsSlog(t *testing.T) {
	t.Parallel()
	var otelCalls atomic.Int64
	k := New("override",
		func(v *testLogValuer) slog.Value { return slog.StringValue(v.name) },
		WithOtel(func(v *testLogValuer) []attribute.KeyValue {
			otelCalls.Add(1)
			return []attribute.KeyValue{attribute.String("custom.key", v.name+"-otel")}
		}),
	)
	v := &testLogValuer{name: "hi"}

	t.Run("Slog uses valueOf, not the override", func(t *testing.T) {
		attr := k.Slog(v)
		assert.Equal(t, attr.Key, "override")
		assert.Equal(t, attr.Value.String(), "hi")
	})

	t.Run("Otel uses the override", func(t *testing.T) {
		attrs := k.Otel(v)
		assert.Equal(t, len(attrs), 1)
		assert.Equal(t, attrs[0].Key, attribute.Key("custom.key"))
		assert.Equal(t, attrs[0].Value.AsString(), "hi-otel")
	})

	t.Run("Attr.Slog uses valueOf, Attr.Otel uses override", func(t *testing.T) {
		before := otelCalls.Load()
		attr := k.Attr(v)
		assert.Equal(t, attr.Slog.Value.String(), "hi")
		assert.Equal(t, len(attr.Otel), 1)
		assert.Equal(t, attr.Otel[0].Key, attribute.Key("custom.key"))
		assert.Assert(t, otelCalls.Load() > before, "override should be invoked from Attr")
	})
}
