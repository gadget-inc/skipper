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
	ReplicaSet  string // replica set name
	AssignedAt  time.Time
	ReadyAt     time.Time
	CPUUsage    *int64
	MemoryUsage *int64
}

func FromPod(pod *v1.Pod) (*Instance, error) {
	if pod == nil {
		return nil, fmt.Errorf("pod is nil")
	}

	instance := &Instance{
		Name: pod.Name,
	}

	if fnJson, ok := pod.Annotations[key.Function.Label]; ok {
		err := json.Unmarshal([]byte(fnJson), &instance.Function)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal function from pod annotation: %w", err)
		}
	} else {
		return nil, fmt.Errorf("missing function annotation")
	}

	instance.ReplicaSet = pod.Annotations[key.ReplicaSet.Label]
	if instance.ReplicaSet == "" {
		return nil, fmt.Errorf("missing replica set annotation")
	}

	var err error
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
	cpuUsage := "unknown"
	if instance.CPUUsage != nil {
		cpuUsage = strconv.FormatInt(*instance.CPUUsage, 10)
	}
	memoryUsage := "unknown"
	if instance.MemoryUsage != nil {
		memoryUsage = strconv.FormatInt(*instance.MemoryUsage/1024/1024, 10)
	}
	return []slog.Attr{
		key.Name.Field(instance.Name),
		key.Addr.Field(instance.Addr),
		key.AssignedAt.Field(instance.AssignedAt),
		key.ReadyAt.Field(instance.ReadyAt),
		key.Function.Field(instance.Function),
		slog.String("cpu_usage", cpuUsage),
		slog.String("memory_usage", memoryUsage),
	}
}

func (instance *Instance) Attributes() []attribute.KeyValue {
	cpuUsage := "unknown"
	if instance.CPUUsage != nil {
		cpuUsage = strconv.FormatInt(*instance.CPUUsage, 10)
	}
	memoryUsage := "unknown"
	if instance.MemoryUsage != nil {
		memoryUsage = strconv.FormatInt(*instance.MemoryUsage/1024/1024, 10)
	}
	return append(key.Function.Attributes(instance.Function),
		key.Name.Attribute(instance.Name),
		key.Addr.Attribute(instance.Addr),
		attribute.String("replica_set", instance.ReplicaSet),
		key.AssignedAt.Attribute(instance.AssignedAt),
		key.ReadyAt.Attribute(instance.ReadyAt),
		attribute.String("cpu_usage", cpuUsage),
		attribute.String("memory_usage", memoryUsage),
	)
}
