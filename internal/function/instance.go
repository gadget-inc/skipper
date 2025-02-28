package function

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gadget-inc/fusion/internal/key"
	"github.com/goccy/go-json"
	"go.opentelemetry.io/otel/attribute"
	v1 "k8s.io/api/core/v1"
)

type Instance struct {
	Function
	Name        string // pod name
	Addr        string // pod IP : function port
	Version     string // replica set name
	AssignedAt  time.Time
	ReadyAt     time.Time
	CPUUsage    *int64
	MemoryUsage *int64
}

func FromPod(pod *v1.Pod) (*Instance, error) {
	instance := &Instance{
		Name: pod.Name,
	}

	instance.Version = pod.Annotations[key.ReplicaSet.Label]
	if instance.Version == "" {
		return nil, fmt.Errorf("missing replica set annotation")
	}

	err := json.Unmarshal([]byte(pod.Annotations[key.Function.Label]), &instance.Function)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal function from pod annotation: %w", err)
	}

	instance.AssignedAt, err = time.Parse(time.RFC3339, pod.Annotations[key.AssignedAt.Label])
	if err != nil {
		return nil, fmt.Errorf("failed to parse assigned at annotation: %w", err)
	}

	if readyAtStr, ok := pod.Annotations[key.ReadyAt.Label]; ok {
		instance.ReadyAt, err = time.Parse(time.RFC3339, readyAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ready at annotation: %w", err)
		}
	}

	var port string
	for _, container := range pod.Spec.Containers {
		for _, containerPort := range container.Ports {
			port = strconv.Itoa(int(containerPort.ContainerPort))
			break
		}
	}
	if port == "" {
		return nil, fmt.Errorf("missing container port")
	}

	instance.Addr = pod.Status.PodIP + ":" + port

	return instance, nil
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
