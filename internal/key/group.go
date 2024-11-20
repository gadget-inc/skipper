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
		attribute.String(p.Underscored+".name", pod.Name),
		attribute.String(p.Underscored+".namespace", pod.Namespace),
		attribute.String(p.Underscored+".ip", pod.Status.PodIP),
		attribute.String(p.Underscored+".phase", string(pod.Status.Phase)),
		attribute.Bool(p.Underscored+".ready", ready),
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
			slog.Int("available_replicas", int(deployment.Status.AvailableReplicas)),
			slog.Int("unavailable_replicas", int(deployment.Status.UnavailableReplicas)),
		),
	}
}

func (d deploymentKey) Attributes(deployment *appsv1.Deployment) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(d.Underscored+".name", deployment.Name),
		attribute.String(d.Underscored+".namespace", deployment.Namespace),
		attribute.Int(d.Underscored+".replicas", int(*deployment.Spec.Replicas)),
		attribute.Int(d.Underscored+".available_replicas", int(deployment.Status.AvailableReplicas)),
		attribute.Int(d.Underscored+".unavailable_replicas", int(deployment.Status.UnavailableReplicas)),
	}
}

type replicaSetKey struct{ key }

var _ GroupKey[*appsv1.ReplicaSet] = replicaSetKey{}

func (d replicaSetKey) Field(replicaSet *appsv1.ReplicaSet) slog.Attr {
	return slog.Attr{
		Key: d.Underscored,
		Value: slog.GroupValue(
			slog.String("name", replicaSet.Name),
			slog.String("namespace", replicaSet.Namespace),
			slog.Int("replicas", int(*replicaSet.Spec.Replicas)),
			slog.Int("available_replicas", int(replicaSet.Status.AvailableReplicas)),
			slog.Int("ready_replicas", int(replicaSet.Status.ReadyReplicas)),
		),
	}
}

func (d replicaSetKey) Attributes(replicaSet *appsv1.ReplicaSet) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(d.Underscored+".name", replicaSet.Name),
		attribute.String(d.Underscored+".namespace", replicaSet.Namespace),
		attribute.Int(d.Underscored+".replicas", int(*replicaSet.Spec.Replicas)),
		attribute.Int(d.Underscored+".available_replicas", int(replicaSet.Status.AvailableReplicas)),
		attribute.Int(d.Underscored+".ready_replicas", int(replicaSet.Status.ReadyReplicas)),
	}
}
