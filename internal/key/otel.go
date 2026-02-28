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
	return appendOtelAttrs(nil, attr.Key, attr.Value)
}

// appendOtelAttrs appends OTel KeyValue attributes converted from a slog value
// to dst, returning the extended slice. This avoids per-attribute slice
// allocations by reusing the caller's backing array.
func appendOtelAttrs(dst []attribute.KeyValue, key string, value slog.Value) []attribute.KeyValue {
	value = value.Resolve()
	if key == "" && value.Equal(slog.Value{}) {
		return dst // skip empty attrs
	}

	switch value.Kind() {
	case slog.KindBool:
		return append(dst, attribute.Bool(key, value.Bool()))
	case slog.KindInt64:
		return append(dst, attribute.Int64(key, value.Int64()))
	case slog.KindUint64:
		return append(dst, attribute.Int64(key, int64(value.Uint64())))
	case slog.KindFloat64:
		return append(dst, attribute.Float64(key, value.Float64()))
	case slog.KindString:
		return append(dst, attribute.String(key, value.String()))
	case slog.KindTime:
		return append(dst, attribute.String(key, value.Time().Format(time.RFC3339)))
	case slog.KindDuration:
		name := key
		if !strings.HasSuffix(name, "_ms") {
			name += "_ms"
		}
		return append(dst, attribute.Int64(name, value.Duration().Milliseconds()))
	case slog.KindGroup:
		groupAttrs := value.Group()
		if dst == nil {
			dst = make([]attribute.KeyValue, 0, len(groupAttrs))
		}
		for _, groupAttr := range groupAttrs {
			if groupAttr.Key == "" && groupAttr.Value.Equal(slog.Value{}) {
				continue // skip empty attrs
			}

			childKey := key
			if key != "" && groupAttr.Key != "" {
				childKey += "."
			}
			childKey += groupAttr.Key

			dst = appendOtelAttrs(dst, childKey, groupAttr.Value)
		}
		return dst
	default:
		switch value := value.Any().(type) {
		case []bool:
			return append(dst, attribute.BoolSlice(key, value))
		case []string:
			return append(dst, attribute.StringSlice(key, value))
		case []int:
			return append(dst, attribute.IntSlice(key, value))
		case []int64:
			return append(dst, attribute.Int64Slice(key, value))
		case []float64:
			return append(dst, attribute.Float64Slice(key, value))
		case error:
			return append(dst, attribute.String(key, value.Error()))
		case fmt.Stringer:
			return append(dst, attribute.String(key, value.String()))
		default:
			return append(dst, attribute.String(key, fmt.Sprintf("%v", value)))
		}
	}
}
