package key

import (
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"gotest.tools/v3/assert"
)

func TestSlogAttrToOtelAttrs(t *testing.T) {
	t.Run("KindBool", func(t *testing.T) {
		attr := slog.Bool("enabled", true)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("enabled"))
		assert.Equal(t, result[0].Value.AsBool(), true)
	})

	t.Run("KindInt64", func(t *testing.T) {
		attr := slog.Int64("count", 42)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("count"))
		assert.Equal(t, result[0].Value.AsInt64(), int64(42))
	})

	t.Run("KindInt", func(t *testing.T) {
		attr := slog.Int("count", 42)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("count"))
		assert.Equal(t, result[0].Value.AsInt64(), int64(42))
	})

	t.Run("KindUint64", func(t *testing.T) {
		attr := slog.Uint64("count", 42)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("count"))
		assert.Equal(t, result[0].Value.AsInt64(), int64(42))
	})

	t.Run("KindFloat64", func(t *testing.T) {
		attr := slog.Float64("ratio", 3.14)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("ratio"))
		assert.Equal(t, result[0].Value.AsFloat64(), 3.14)
	})

	t.Run("KindString", func(t *testing.T) {
		attr := slog.String("name", "test")
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("name"))
		assert.Equal(t, result[0].Value.AsString(), "test")
	})

	t.Run("KindTime", func(t *testing.T) {
		ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		attr := slog.Time("timestamp", ts)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("timestamp"))
		assert.Equal(t, result[0].Value.AsString(), "2024-01-15T10:30:00Z")
	})

	t.Run("KindDuration", func(t *testing.T) {
		attr := slog.Duration("elapsed", 1500*time.Millisecond)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("elapsed_ms"))
		assert.Equal(t, result[0].Value.AsInt64(), int64(1500))
	})

	t.Run("KindGroup", func(t *testing.T) {
		attr := slog.Group("request",
			slog.String("method", "GET"),
			slog.Int("status", 200),
		)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 2)

		// Find attributes by key (order may vary)
		resultMap := make(map[string]attribute.Value)
		for _, kv := range result {
			resultMap[string(kv.Key)] = kv.Value
		}

		assert.Equal(t, resultMap["request.method"].AsString(), "GET")
		assert.Equal(t, resultMap["request.status"].AsInt64(), int64(200))
	})

	t.Run("KindGroup nested", func(t *testing.T) {
		attr := slog.Group("outer",
			slog.Group("inner",
				slog.String("value", "deep"),
			),
		)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("outer.inner.value"))
		assert.Equal(t, result[0].Value.AsString(), "deep")
	})

	t.Run("KindGroup empty prefix", func(t *testing.T) {
		// Test GroupAttrs which creates an attr with empty key
		attr := slog.GroupAttrs("", slog.String("name", "test"))
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("name"))
		assert.Equal(t, result[0].Value.AsString(), "test")
	})

	t.Run("KindAny", func(t *testing.T) {
		type custom struct{ X int }
		attr := slog.Any("data", custom{X: 42})
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("data"))
		assert.Equal(t, result[0].Value.AsString(), "{42}")
	})

	t.Run("KindAny with []string", func(t *testing.T) {
		attr := slog.Any("tags", []string{"a", "b"})
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("tags"))
		assert.DeepEqual(t, result[0].Value.AsStringSlice(), []string{"a", "b"})
	})

	t.Run("KindAny with []int", func(t *testing.T) {
		attr := slog.Any("ids", []int{1, 2, 3})
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("ids"))
		assert.DeepEqual(t, result[0].Value.AsInt64Slice(), []int64{1, 2, 3})
	})

	t.Run("KindAny with []int64", func(t *testing.T) {
		attr := slog.Any("big", []int64{1, 2})
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("big"))
		assert.DeepEqual(t, result[0].Value.AsInt64Slice(), []int64{1, 2})
	})

	t.Run("KindAny with []float64", func(t *testing.T) {
		attr := slog.Any("rates", []float64{1.1, 2.2})
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("rates"))
		assert.DeepEqual(t, result[0].Value.AsFloat64Slice(), []float64{1.1, 2.2})
	})

	t.Run("KindAny with []bool", func(t *testing.T) {
		attr := slog.Any("flags", []bool{true, false})
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("flags"))
		assert.DeepEqual(t, result[0].Value.AsBoolSlice(), []bool{true, false})
	})

	t.Run("KindAny with int", func(t *testing.T) {
		attr := slog.Any("count", 42)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("count"))
		assert.Equal(t, result[0].Value.AsInt64(), int64(42))
	})

	t.Run("KindAny with bool", func(t *testing.T) {
		attr := slog.Any("ok", true)
		result := slogAttrToOtelAttrs(attr)

		assert.Equal(t, len(result), 1)
		assert.Equal(t, result[0].Key, attribute.Key("ok"))
		assert.Equal(t, result[0].Value.AsBool(), true)
	})
}
