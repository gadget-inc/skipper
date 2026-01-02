package key

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// slogAttrToOtelAttrs converts a slog.Attr to a slice of OTel KeyValue attributes.
// Groups are flattened using dot notation for the key prefix.
func slogAttrToOtelAttrs(attr slog.Attr) []attribute.KeyValue {
	return slogValueToOtelAttrs(attr.Key, attr.Value)
}

func slogValueToOtelAttrs(key string, value slog.Value) []attribute.KeyValue {
	value = value.Resolve()
	if key == "" && value.Equal(slog.Value{}) {
		return nil // skip empty attrs
	}

	switch value.Kind() {
	case slog.KindBool:
		return []attribute.KeyValue{attribute.Bool(key, value.Bool())}
	case slog.KindInt64:
		return []attribute.KeyValue{attribute.Int64(key, value.Int64())}
	case slog.KindUint64:
		return []attribute.KeyValue{attribute.Int64(key, int64(value.Uint64()))}
	case slog.KindFloat64:
		return []attribute.KeyValue{attribute.Float64(key, value.Float64())}
	case slog.KindString:
		return []attribute.KeyValue{attribute.String(key, value.String())}
	case slog.KindTime:
		return []attribute.KeyValue{attribute.String(key, value.Time().Format(time.RFC3339))}
	case slog.KindDuration:
		name := key
		if !strings.HasSuffix(name, "_ms") {
			name += "_ms"
		}
		return []attribute.KeyValue{attribute.Int64(name, value.Duration().Milliseconds())}
	case slog.KindGroup:
		groupAttrs := value.Group()
		otelAttrs := make([]attribute.KeyValue, 0, len(groupAttrs))
		for _, groupAttr := range groupAttrs {
			if groupAttr.Key == "" && groupAttr.Value.Equal(slog.Value{}) {
				continue // skip empty attrs
			}

			childKey := key
			if key != "" && groupAttr.Key != "" {
				childKey += "."
			}
			childKey += groupAttr.Key

			otelAttrs = append(otelAttrs, slogValueToOtelAttrs(childKey, groupAttr.Value)...)
		}
		return otelAttrs
	default:
		switch value := value.Any().(type) {
		case []bool:
			return []attribute.KeyValue{attribute.BoolSlice(key, value)}
		case []string:
			return []attribute.KeyValue{attribute.StringSlice(key, value)}
		case []int:
			return []attribute.KeyValue{attribute.IntSlice(key, value)}
		case []int64:
			return []attribute.KeyValue{attribute.Int64Slice(key, value)}
		case []float64:
			return []attribute.KeyValue{attribute.Float64Slice(key, value)}
		case error:
			return []attribute.KeyValue{attribute.String(key, value.Error())}
		case fmt.Stringer:
			return []attribute.KeyValue{attribute.String(key, value.String())}
		default:
			return []attribute.KeyValue{attribute.String(key, fmt.Sprintf("%v", value))}
		}
	}
}
