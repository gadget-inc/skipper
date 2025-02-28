package key

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type boolKey struct{ key }

var _ Key[bool] = boolKey{}

func (k boolKey) Field(value bool) slog.Attr {
	return slog.Bool(k.Underscored, value)
}

func (k boolKey) Attribute(value bool) attribute.KeyValue {
	return attribute.Bool(k.Underscored, value)
}

type stringKey struct{ key }

func (k stringKey) Field(value string) slog.Attr {
	return slog.String(k.Underscored, value)
}

func (k stringKey) Attribute(value string) attribute.KeyValue {
	return attribute.String(k.Underscored, value)
}

type stringSliceKey struct{ key }

var _ Key[[]string] = stringSliceKey{}

func (k stringSliceKey) Field(value []string) slog.Attr {
	return slog.Any(k.Underscored, value)
}

func (k stringSliceKey) Attribute(value []string) attribute.KeyValue {
	return attribute.StringSlice(k.Underscored, value)
}

type intKey struct{ key }

var _ Key[int] = intKey{}

func (k intKey) Field(value int) slog.Attr {
	return slog.Int(k.Underscored, value)
}

func (k intKey) Attribute(value int) attribute.KeyValue {
	return attribute.Int(k.Underscored, value)
}

type int64Key struct{ key }

var _ Key[int64] = int64Key{}

func (k int64Key) Field(value int64) slog.Attr {
	return slog.Int64(k.Underscored, value)
}

func (k int64Key) Attribute(value int64) attribute.KeyValue {
	return attribute.Int64(k.Underscored, value)
}

type durationKey struct{ key }

var _ Key[time.Duration] = durationKey{}

func (k durationKey) Field(value time.Duration) slog.Attr {
	return slog.Duration(k.Underscored, value)
}

func (k durationKey) Attribute(value time.Duration) attribute.KeyValue {
	return attribute.Float64(k.Underscored, float64(value.Milliseconds()))
}

type float64Key struct{ key }

var _ Key[float64] = float64Key{}

func (k float64Key) Field(value float64) slog.Attr {
	return slog.Float64(k.Underscored, value)
}

func (k float64Key) Attribute(value float64) attribute.KeyValue {
	return attribute.Float64(k.Underscored, value)
}

type timeKey struct{ key }

var _ Key[time.Time] = timeKey{}

func (k timeKey) Field(value time.Time) slog.Attr {
	return slog.Time(k.Underscored, value)
}

func (k timeKey) Attribute(value time.Time) attribute.KeyValue {
	return attribute.String(k.Underscored, value.Format(time.RFC3339))
}

type stringerKey struct{ key }

var _ Key[fmt.Stringer] = stringerKey{}

func (k stringerKey) Field(value fmt.Stringer) slog.Attr {
	return slog.String(k.Underscored, value.String())
}

func (k stringerKey) Attribute(value fmt.Stringer) attribute.KeyValue {
	return attribute.String(k.Underscored, value.String())
}

type errorKey struct{ key }

var _ Key[error] = errorKey{}

func (k errorKey) Field(value error) slog.Attr {
	if value == nil {
		return slog.Attr{}
	}
	return slog.String(k.Underscored, value.Error())
}

func (k errorKey) Attribute(value error) attribute.KeyValue {
	return attribute.String(k.Underscored, value.Error())
}

type GroupValue interface {
	Fields() []slog.Attr
	Attributes() []attribute.KeyValue
}

type groupValueKey struct{ key }

var _ GroupKey[GroupValue] = groupValueKey{}

func (k groupValueKey) Field(value GroupValue) slog.Attr {
	return slog.Attr{Key: k.Underscored, Value: slog.GroupValue(value.Fields()...)}
}

func (k groupValueKey) Attributes(value GroupValue) []attribute.KeyValue {
	attrs := value.Attributes()
	for i := range attrs {
		attrs[i].Key = attribute.Key(k.Underscored) + "." + attrs[i].Key
	}
	return attrs
}

type podKey struct{ key }

var _ GroupKey[*v1.Pod] = podKey{}

func (k podKey) Field(value *v1.Pod) slog.Attr {
	return slog.Attr{
		Key: k.Underscored,
		Value: slog.GroupValue(
			Name.Field(value.Name),
			Namespace.Field(value.Namespace),
			Labels.Field(value.Labels),
		),
	}
}

func (k podKey) Attributes(value *v1.Pod) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(k.Underscored+".name", value.Name),
		attribute.String(k.Underscored+".namespace", value.Namespace),
		attribute.String(k.Underscored+".labels", labels.Set(value.Labels).String()),
	}
}

type deploymentKey struct{ key }

var _ GroupKey[*appsv1.Deployment] = deploymentKey{}

func (k deploymentKey) Field(value *appsv1.Deployment) slog.Attr {
	return slog.Attr{
		Key: k.Underscored,
		Value: slog.GroupValue(
			Name.Field(value.Name),
			Namespace.Field(value.Namespace),
		),
	}
}

func (k deploymentKey) Attributes(value *appsv1.Deployment) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(k.Underscored+".name", value.Name),
		attribute.String(k.Underscored+".namespace", value.Namespace),
	}
}

type replicaSetKey struct{ key }

var _ GroupKey[*appsv1.ReplicaSet] = replicaSetKey{}

func (k replicaSetKey) Field(value *appsv1.ReplicaSet) slog.Attr {
	return slog.Attr{
		Key: k.Underscored,
		Value: slog.GroupValue(
			Name.Field(value.Name),
			Namespace.Field(value.Namespace),
			slog.Int("replicas", int(value.Status.Replicas)),
			slog.Int("available_replicas", int(value.Status.AvailableReplicas)),
		),
	}
}

func (k replicaSetKey) Attributes(value *appsv1.ReplicaSet) []attribute.KeyValue {
	var desiredReplicas int64
	if value.Spec.Replicas != nil {
		desiredReplicas = int64(*value.Spec.Replicas)
	}

	return []attribute.KeyValue{
		attribute.String(k.Underscored+".name", value.Name),
		attribute.String(k.Underscored+".namespace", value.Namespace),
		attribute.Int64(k.Underscored+".desired_replicas", desiredReplicas),
	}
}

type mapStringString struct{ key }

var _ GroupKey[map[string]string] = mapStringString{}

func (k mapStringString) Field(value map[string]string) slog.Attr {
	keyValues := make([]slog.Attr, 0, len(value))
	for k, v := range value {
		keyValues = append(keyValues, slog.String(k, v))
	}

	return slog.Attr{
		Key:   k.Underscored,
		Value: slog.GroupValue(keyValues...),
	}
}

func (k mapStringString) Attributes(value map[string]string) []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, len(value))
	for mapKey, v := range value {
		attributes = append(attributes, attribute.String(k.Underscored+"."+mapKey, v))
	}
	return attributes
}

type requestKey struct{ key }

var _ GroupKey[*http.Request] = requestKey{}

func (r requestKey) Field(value *http.Request) slog.Attr {
	return slog.Attr{
		Key: r.Underscored,
		Value: slog.GroupValue(
			slog.String("method", value.Method),
			slog.String("url", value.URL.String()),
			slog.String("host", value.Host),
			slog.String("remote_address", value.RemoteAddr),
		),
	}
}

func (r requestKey) Attributes(value *http.Request) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(r.Underscored+".method", value.Method),
		attribute.String(r.Underscored+".url", value.URL.String()),
		attribute.String(r.Underscored+".host", value.Host),
		attribute.String(r.Underscored+".remote_address", value.RemoteAddr),
	}
}

type responseKey struct{ key }

var _ GroupKey[*http.Response] = responseKey{}

func (r responseKey) Field(value *http.Response) slog.Attr {
	return slog.Attr{
		Key: r.Underscored,
		Value: slog.GroupValue(
			slog.String("method", value.Request.Method),
			slog.String("url", value.Request.URL.String()),
			slog.String("host", value.Request.Host),
			slog.String("remote_address", value.Request.RemoteAddr),
			slog.Int("status_code", value.StatusCode),
			slog.String("status", value.Status),
		),
	}
}

func (r responseKey) Attributes(value *http.Response) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(r.Underscored+".method", value.Request.Method),
		attribute.String(r.Underscored+".url", value.Request.URL.String()),
		attribute.String(r.Underscored+".host", value.Request.Host),
		attribute.String(r.Underscored+".remote_address", value.Request.RemoteAddr),
		attribute.Int(r.Underscored+".status_code", value.StatusCode),
		attribute.String(r.Underscored+".status", value.Status),
	}
}
