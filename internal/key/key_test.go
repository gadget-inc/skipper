package key_test

import (
	"testing"

	"github.com/gadget-inc/skipper/internal/key"
	"go.opentelemetry.io/otel/attribute"
	"gotest.tools/v3/assert"
)

func TestKey_SlogAttr(t *testing.T) {
	t.Run("string key", func(t *testing.T) {
		attr := key.Namespace.Slog("default")

		assert.Equal(t, attr.Key, "namespace")
		assert.Equal(t, attr.Value.String(), "default")
	})

	t.Run("int key", func(t *testing.T) {
		attr := key.Count.Slog(42)

		assert.Equal(t, attr.Key, "count")
		assert.Equal(t, attr.Value.Int64(), int64(42))
	})
}

func TestKey_Attr(t *testing.T) {
	t.Run("contains both slog and otel", func(t *testing.T) {
		attr := key.Namespace.Attr("default")

		// Slog attr
		assert.Equal(t, attr.Slog.Key, "namespace")
		assert.Equal(t, attr.Slog.Value.String(), "default")

		// OTel attrs
		assert.Equal(t, len(attr.Otel), 1)
		assert.Equal(t, attr.Otel[0].Key, attribute.Key("namespace"))
		assert.Equal(t, attr.Otel[0].Value.AsString(), "default")
	})
}

func TestKey_Names(t *testing.T) {
	t.Run("key embeds Names", func(t *testing.T) {
		assert.Equal(t, key.Namespace.Name, "namespace")
		assert.Equal(t, key.Namespace.Header, "X-Skipper-Namespace")
		assert.Equal(t, key.Namespace.Label, "skipper/namespace")
	})
}
