package key

// This file contains factory functions for creating typed keys.
// Each function creates a Key specialized for a particular Go type,
// defining how values of that type are converted to slog attributes.

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

// newKey creates a Key with a custom slog.Attr builder. Reserved for primitive
// helpers that need to vary the attribute's key (e.g. a "_ms" suffix) or
// suppress the attribute on a nil input. For the typical case where the
// slog.Attr key is the Names.Name and the value comes from a
// slog.Value-returning function, use [New] instead.
func newKey[V any](name string, toSlogAttr func(Names, V) slog.Attr) *Key[V] {
	n := newNames(name)
	return &Key[V]{
		Names: n,
		toSlogAttr: func(value V) slog.Attr {
			return toSlogAttr(n, value)
		},
	}
}

// New creates a typed Key whose slog representation comes from valueOf.
// The slog.Attr key is fixed to the Names.Name.
func New[T any](name string, valueOf func(T) slog.Value) *Key[T] {
	return newKey(name, func(n Names, v T) slog.Attr {
		return slog.Attr{Key: n.Name, Value: valueOf(v)}
	})
}

// NewCached creates a typed Key for pointer values whose Attr is memoized
// per pointer via a weak cache. Cache entries shrink automatically once *T
// becomes unreachable.
//
// Use NewCached for keys whose values are long-lived pointers reused across
// many calls. For values with no pointer identity worth caching, use [New].
func NewCached[T any](name string, valueOf func(*T) slog.Value) *Key[*T] {
	k := New(name, valueOf)
	c := &memoizedCache[T]{build: k.buildAttr}
	k.cache = c.attr
	return k
}

// NewWithOtel creates a typed Key whose OTel attributes come from a direct
// otelOf function instead of the default slog -> OTel walk. Used on hot paths
// where the slog walk's allocation cost is measurable.
func NewWithOtel[T any](name string, valueOf func(T) slog.Value, otelOf func(T) []attribute.KeyValue) *Key[T] {
	k := New(name, valueOf)
	k.otelOverride = otelOf
	return k
}

func boolKey(name string) *Key[bool] {
	return newKey(name, func(n Names, v bool) slog.Attr { return slog.Bool(n.Name, v) })
}

func stringKey(name string) *Key[string] {
	return newKey(name, func(n Names, v string) slog.Attr { return slog.String(n.Name, v) })
}

func intKey(name string) *Key[int] {
	return newKey(name, func(n Names, v int) slog.Attr { return slog.Int(n.Name, v) })
}

func uint32Key(name string) *Key[uint32] {
	return newKey(name, func(n Names, v uint32) slog.Attr { return slog.Int(n.Name, int(v)) })
}

func float64Key(name string) *Key[float64] {
	return newKey(name, func(n Names, v float64) slog.Attr { return slog.Float64(n.Name, v) })
}

func stringSliceKey(name string) *Key[[]string] {
	return newKey(name, func(n Names, v []string) slog.Attr { return slog.Any(n.Name, v) })
}

func durationKey(name string) *Key[time.Duration] {
	return newKey(name, func(n Names, v time.Duration) slog.Attr {
		name := n.Name
		if !strings.HasSuffix(name, "_ms") {
			name += "_ms"
		}
		return slog.Int64(name, v.Milliseconds())
	})
}

func timeKey(name string) *Key[time.Time] {
	return newKey(name, func(n Names, v time.Time) slog.Attr { return slog.Time(n.Name, v) })
}

func errorKey(name string) *Key[error] {
	return newKey(name, func(n Names, err error) slog.Attr {
		if err == nil {
			return slog.Attr{}
		}
		return slog.Any(n.Name, err)
	})
}

func mapStringStringKey(name string) *Key[map[string]string] {
	return newKey(name, func(n Names, value map[string]string) slog.Attr {
		if value == nil {
			return slog.Attr{}
		}
		attrs := make([]slog.Attr, 0, len(value))
		for mapKey, mapValue := range value {
			attrs = append(attrs, slog.String(mapKey, mapValue))
		}
		return slog.GroupAttrs(n.Name, attrs...)
	})
}

func requestKey(name string) *Key[*http.Request] {
	return newKey(name, func(n Names, req *http.Request) slog.Attr {
		if req == nil {
			return slog.Attr{}
		}

		var contentTypeAttr slog.Attr
		if contentType, ok := req.Header["Content-Type"]; ok {
			contentTypeAttr = slog.Any("header.content-type", contentType)
		}

		var contentLengthAttr slog.Attr
		if contentLength, ok := req.Header["Content-Length"]; ok {
			contentLengthAttr = slog.Any("header.content-length", contentLength)
		}

		return slog.GroupAttrs(
			n.Name,
			slog.String("method", req.Method),
			slog.Int64("body.size", req.ContentLength),
			contentTypeAttr,
			contentLengthAttr,
		)
	})
}

func responseKey(name string) *Key[*http.Response] {
	return newKey(name, func(n Names, resp *http.Response) slog.Attr {
		if resp == nil {
			return slog.Attr{}
		}

		var contentTypeAttr slog.Attr
		if contentType, ok := resp.Header["Content-Type"]; ok {
			contentTypeAttr = slog.Any("header.content-type", contentType)
		}

		var contentLengthAttr slog.Attr
		if contentLength, ok := resp.Header["Content-Length"]; ok {
			contentLengthAttr = slog.Any("header.content-length", contentLength)
		}

		return slog.GroupAttrs(
			n.Name,
			slog.Int("status_code", resp.StatusCode),
			slog.Int64("body.size", resp.ContentLength),
			contentTypeAttr,
			contentLengthAttr,
		)
	})
}

func podKey(name string) *Key[*v1.Pod] {
	return newKey(name, func(n Names, pod *v1.Pod) slog.Attr {
		if pod == nil {
			return slog.Attr{}
		}

		var deletionTimestamp slog.Attr
		if pod.DeletionTimestamp != nil {
			deletionTimestamp = slog.Time("deletion_timestamp", pod.DeletionTimestamp.Time)
		}

		conditions := make([]slog.Attr, 0, len(pod.Status.Conditions))
		for _, condition := range pod.Status.Conditions {
			conditions = append(conditions, slog.String(string(condition.Type), string(condition.Status)))
		}

		// Build label attributes with singular "label" per OTEL k8s semantic conventions.
		labels := make([]slog.Attr, 0, len(pod.Labels))
		for k, v := range pod.Labels {
			labels = append(labels, slog.String(k, v))
		}

		return slog.GroupAttrs(n.Name,
			slog.String("name", pod.Name),
			slog.String("namespace", pod.Namespace),
			slog.String("uid", string(pod.UID)),
			slog.GroupAttrs("label", labels...),
			slog.GroupAttrs("conditions", conditions...),
			slog.GroupAttrs("status",
				slog.String("phase", string(pod.Status.Phase)),
				slog.String("ip", pod.Status.PodIP),
			),
			deletionTimestamp,
		)
	})
}

func replicaSetKey(name string) *Key[*appsv1.ReplicaSet] {
	return newKey(name, func(n Names, rs *appsv1.ReplicaSet) slog.Attr {
		if rs == nil {
			return slog.Attr{}
		}
		return slog.GroupAttrs(n.Name,
			slog.String("name", rs.Name),
			slog.String("namespace", rs.Namespace),
			slog.String("uid", string(rs.UID)),
			slog.Int("replicas", int(rs.Status.Replicas)),
			slog.Int("available_replicas", int(rs.Status.AvailableReplicas)),
		)
	})
}

func urlKey(name string) *Key[*url.URL] {
	return newKey(name, func(n Names, url *url.URL) slog.Attr {
		if url == nil {
			return slog.Attr{}
		}

		var portAttr slog.Attr
		if port := url.Port(); port != "" {
			portAttr = slog.String("port", port)
		}

		var fragmentAttr slog.Attr
		if url.Fragment != "" {
			fragmentAttr = slog.String("fragment", url.Fragment)
		}

		return slog.GroupAttrs(n.Name,
			slog.String("full", url.Redacted()),
			slog.String("scheme", url.Scheme),
			slog.String("domain", url.Hostname()),
			portAttr,
			slog.String("path", url.Path),
			slog.String("query", url.RawQuery),
			fragmentAttr,
		)
	})
}
