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

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

// newKey creates a Key with the given name and slog attribute converter.
func newKey[V any](name string, toSlogAttr func(Identifier, V) slog.Attr) Key[V] {
	id := newIdentifier(name)
	return Key[V]{
		Identifier: id,
		toSlogAttr: func(value V) slog.Attr {
			return toSlogAttr(id, value)
		},
	}
}

// New creates a typed Key whose slog representation comes from valueOf.
// Apply options like [WithOtel] for direct OTel construction. The returned
// pointer ensures any internal state (e.g. an [Option]-supplied override) is
// shared across copies of the Key.
func New[T any](name string, valueOf func(T) slog.Value, opts ...Option[T]) *Key[T] {
	id := newIdentifier(name)
	k := &Key[T]{
		Identifier: id,
		toSlogAttr: func(v T) slog.Attr {
			return slog.Attr{Key: id.Name, Value: valueOf(v)}
		},
	}
	for _, opt := range opts {
		opt(k)
	}
	return k
}

// NewCached creates a typed Key for pointer values whose Attr is memoized
// per pointer via a weak cache. Cache entries shrink automatically once *T
// becomes unreachable.
//
// Use NewCached for keys whose values are long-lived pointers reused across
// many calls. For values with no pointer identity worth caching, use [New].
func NewCached[T any](name string, valueOf func(*T) slog.Value, opts ...Option[*T]) *Key[*T] {
	k := New(name, valueOf, opts...)
	k.cache = memoized(k.buildAttr)
	return k
}

func boolKey(name string) Key[bool] {
	return newKey(name, func(id Identifier, v bool) slog.Attr { return slog.Bool(id.Name, v) })
}

func stringKey(name string) Key[string] {
	return newKey(name, func(id Identifier, v string) slog.Attr { return slog.String(id.Name, v) })
}

func intKey(name string) Key[int] {
	return newKey(name, func(id Identifier, v int) slog.Attr { return slog.Int(id.Name, v) })
}

func uint32Key(name string) Key[uint32] {
	return newKey(name, func(id Identifier, v uint32) slog.Attr { return slog.Int(id.Name, int(v)) })
}

func stringSliceKey(name string) Key[[]string] {
	return newKey(name, func(id Identifier, v []string) slog.Attr { return slog.Any(id.Name, v) })
}

func durationKey(name string) Key[time.Duration] {
	return newKey(name, func(id Identifier, v time.Duration) slog.Attr {
		name := id.Name
		if !strings.HasSuffix(name, "_ms") {
			name += "_ms"
		}
		return slog.Int64(name, v.Milliseconds())
	})
}

func timeKey(name string) Key[time.Time] {
	return newKey(name, func(id Identifier, v time.Time) slog.Attr { return slog.Time(id.Name, v) })
}

func errorKey(name string) Key[error] {
	return newKey(name, func(id Identifier, err error) slog.Attr {
		if err == nil {
			return slog.Attr{}
		}
		return slog.Any(id.Name, err)
	})
}

func mapStringStringKey(name string) Key[map[string]string] {
	return newKey(name, func(id Identifier, value map[string]string) slog.Attr {
		if value == nil {
			return slog.Attr{}
		}
		attrs := make([]slog.Attr, 0, len(value))
		for mapKey, mapValue := range value {
			attrs = append(attrs, slog.String(mapKey, mapValue))
		}
		return slog.GroupAttrs(id.Name, attrs...)
	})
}

func requestKey(name string) Key[*http.Request] {
	return newKey(name, func(id Identifier, req *http.Request) slog.Attr {
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
			id.Name,
			slog.String("method", req.Method),
			slog.Int64("body.size", req.ContentLength),
			contentTypeAttr,
			contentLengthAttr,
		)
	})
}

func responseKey(name string) Key[*http.Response] {
	return newKey(name, func(id Identifier, resp *http.Response) slog.Attr {
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
			id.Name,
			slog.Int("status_code", resp.StatusCode),
			slog.Int64("body.size", resp.ContentLength),
			contentTypeAttr,
			contentLengthAttr,
		)
	})
}

func podKey(name string) Key[*v1.Pod] {
	return newKey(name, func(id Identifier, pod *v1.Pod) slog.Attr {
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

		return slog.GroupAttrs(id.Name,
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

func replicaSetKey(name string) Key[*appsv1.ReplicaSet] {
	return newKey(name, func(id Identifier, rs *appsv1.ReplicaSet) slog.Attr {
		if rs == nil {
			return slog.Attr{}
		}
		return slog.GroupAttrs(id.Name,
			slog.String("name", rs.Name),
			slog.String("namespace", rs.Namespace),
			slog.String("uid", string(rs.UID)),
			slog.Int("replicas", int(rs.Status.Replicas)),
			slog.Int("available_replicas", int(rs.Status.AvailableReplicas)),
		)
	})
}

func urlKey(name string) Key[*url.URL] {
	return newKey(name, func(id Identifier, url *url.URL) slog.Attr {
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

		return slog.GroupAttrs(id.Name,
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
