package function

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/key"
	"go.opentelemetry.io/otel/attribute"
	v1 "k8s.io/api/core/v1"
)

type Instance struct {
	Function
	Name       string // pod name
	Addr       string // pod IP : function port
	Version    string // replica set name
	AssignedAt time.Time
	ReadyAt    time.Time
}

func FromPod(pod *v1.Pod) (*Instance, error) {
	fn, err := new(
		pod.Labels[key.Deployment.Label],
		pod.Labels[key.MaxInstances.Label],
		"", // we don't store metadata in labels because they may contain sensitive information
		pod.Labels[key.MinInstances.Label],
		pod.Labels[key.Namespace.Label],
		pod.Labels[key.TargetCPUUtilization.Label],
		pod.Labels[key.TargetMemoryUtilization.Label],
		pod.Labels[key.Tenant.Label],
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get function from pod: %w", err)
	}

	assignedAtInt, err := strconv.ParseInt(pod.Labels[key.AssignedAt.Label], 10, 64)
	if err != nil {
		return nil, ErrInvalidAssignedAt
	}
	assignedAt := time.Unix(assignedAtInt, 0)

	var readyAt time.Time
	if readyAtStr, ok := pod.Labels[key.ReadyAt.Label]; ok {
		readyAtInt, err := strconv.ParseInt(readyAtStr, 10, 64)
		if err != nil {
			return nil, ErrInvalidReadyAt
		}
		readyAt = time.Unix(readyAtInt, 0)
	}

	replicaSet := pod.Labels[key.ReplicaSet.Label]
	if replicaSet == "" {
		return nil, ErrMissingReplicaSet
	}

	return &Instance{
		Function:   fn,
		Name:       pod.Name,
		Addr:       pod.Status.PodIP + ":" + strconv.Itoa(FlagPort.Value),
		Version:    replicaSet,
		AssignedAt: assignedAt,
		ReadyAt:    readyAt,
	}, nil
}

func (instance *Instance) Fields() []slog.Attr {
	return []slog.Attr{
		key.Name.Field(instance.Name),
		key.Addr.Field(instance.Addr),
		key.AssignedAt.Field(instance.AssignedAt),
		key.ReadyAt.Field(instance.ReadyAt),
		key.Function.Field(instance.Function),
	}
}

func (instance *Instance) Attributes() []attribute.KeyValue {
	return append(key.Function.Attributes(instance.Function),
		key.Name.Attribute(instance.Name),
		key.Addr.Attribute(instance.Addr),
		key.AssignedAt.Attribute(instance.AssignedAt),
		key.ReadyAt.Attribute(instance.ReadyAt),
	)
}
