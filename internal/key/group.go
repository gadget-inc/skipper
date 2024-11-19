package key

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

type GroupKey[v any] interface {
	Field(value v) slog.Attr
	Attributes(value v) []attribute.KeyValue
}

type GroupValue interface {
	Fields() []slog.Attr
	Attributes() []attribute.KeyValue
}

type groupValueKey struct{ key }

var _ GroupKey[GroupValue] = groupValueKey{}

func (gvk groupValueKey) Field(gv GroupValue) slog.Attr {
	return slog.Attr{Key: gvk.Underscored, Value: slog.GroupValue(gv.Fields()...)}
}

func (gvk groupValueKey) Attributes(gv GroupValue) []attribute.KeyValue {
	attrs := gv.Attributes()
	for i := range attrs {
		attrs[i].Key = attribute.Key(gvk.Underscored) + "." + attrs[i].Key
	}
	return attrs
}

type podKey struct{ key }

var _ GroupKey[*v1.Pod] = podKey{}

func (p podKey) Field(pod *v1.Pod) slog.Attr {
	var ready bool
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady {
			ready = condition.Status == v1.ConditionTrue
			break
		}
	}

	return slog.Attr{
		Key: p.Underscored,
		Value: slog.GroupValue(
			slog.String("name", pod.Name),
			slog.String("namespace", pod.Namespace),
			slog.String("ip", pod.Status.PodIP),
			slog.Bool("ready", ready),
			slog.String("phase", string(pod.Status.Phase)),
		),
	}
}

func (p podKey) Attributes(pod *v1.Pod) []attribute.KeyValue {
	var ready bool
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady {
			ready = condition.Status == v1.ConditionTrue
			break
		}
	}

	return []attribute.KeyValue{
		attribute.String(p.KebabCased+".name", pod.Name),
		attribute.String(p.KebabCased+".namespace", pod.Namespace),
		attribute.String(p.KebabCased+".ip", pod.Status.PodIP),
		attribute.String(p.KebabCased+".phase", string(pod.Status.Phase)),
		attribute.Bool(p.KebabCased+".ready", ready),
	}
}

type deploymentKey struct{ key }

var _ GroupKey[*appsv1.Deployment] = deploymentKey{}

func (d deploymentKey) Field(deployment *appsv1.Deployment) slog.Attr {
	return slog.Attr{
		Key: d.Underscored,
		Value: slog.GroupValue(
			slog.String("name", deployment.Name),
			slog.String("namespace", deployment.Namespace),
			slog.Int("replicas", int(*deployment.Spec.Replicas)),
			slog.Int("available", int(deployment.Status.AvailableReplicas)),
			slog.Int("unavailable", int(deployment.Status.UnavailableReplicas)),
		),
	}
}

func (d deploymentKey) Attributes(deployment *appsv1.Deployment) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(d.KebabCased+".name", deployment.Name),
		attribute.String(d.KebabCased+".namespace", deployment.Namespace),
		attribute.Int(d.KebabCased+".replicas", int(*deployment.Spec.Replicas)),
		attribute.Int(d.KebabCased+".available", int(deployment.Status.AvailableReplicas)),
		attribute.Int(d.KebabCased+".unavailable", int(deployment.Status.UnavailableReplicas)),
	}
}
