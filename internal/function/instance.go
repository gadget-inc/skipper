package function

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
	v1 "k8s.io/api/core/v1"
)

var emptyInstance = Instance{Function: emptyFunction}

type Instance struct {
	Function
	Pod        *v1.Pod
	ReplicaSet string
	AssignedAt time.Time
	ReadyAt    time.Time
}

func FromPod(pod *v1.Pod) (Instance, error) {
	fn, err := new(
		pod.Labels[key.Deployment.Label],
		pod.Labels[key.MaxReplicas.Label],
		"", // we don't store metadata in labels because they may contain sensitive information
		pod.Labels[key.MinReplicas.Label],
		pod.Labels[key.Namespace.Label],
		pod.Labels[key.TargetCPUUtilization.Label],
		pod.Labels[key.TargetMemoryUtilization.Label],
		pod.Labels[key.Tenant.Label],
	)
	if err != nil {
		return emptyInstance, err
	}

	assignedAtStr := pod.Labels[key.AssignedAt.Label]
	if assignedAtStr == "" {
		return emptyInstance, ErrMissingAssignedAt
	}

	assignedAt, err := strconv.ParseInt(assignedAtStr, 10, 64)
	if err != nil || assignedAt == 0 {
		return emptyInstance, ErrInvalidAssignedAt
	}

	var readyAt time.Time
	if readyAtStr, ok := pod.Labels[key.ReadyAt.Label]; ok {
		readyAtInt, err := strconv.ParseInt(readyAtStr, 10, 64)
		if err != nil || readyAtInt == 0 {
			return emptyInstance, ErrInvalidReadyAt
		}
		readyAt = time.Unix(readyAtInt, 0)
	}

	replicaSet := pod.Labels[key.ReplicaSet.Label]
	if replicaSet == "" {
		return emptyInstance, ErrMissingReplicaSet
	}

	return Instance{
		Function:   fn,
		Pod:        pod,
		AssignedAt: time.Unix(assignedAt, 0),
		ReadyAt:    readyAt,
		ReplicaSet: replicaSet,
	}, nil
}

func (instance Instance) Fields() []slog.Attr {
	return []slog.Attr{
		key.Name.Field(instance.Pod.Name),
		key.IP.Field(instance.Pod.Status.PodIP),
		// key.AssignedAt.Field(instance.AssignedAt),
		// key.ReadyAt.Field(instance.ReadyAt),
		key.Function.Field(instance.Function),
	}
}

func (instance Instance) Attributes() []attribute.KeyValue {
	return append(key.Function.Attributes(instance.Function),
		key.Name.Attribute(instance.Pod.Name),
		key.IP.Attribute(instance.Pod.Status.PodIP),
		key.AssignedAt.Attribute(instance.AssignedAt),
		key.ReadyAt.Attribute(instance.ReadyAt),
	)
}
