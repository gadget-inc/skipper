package function

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
	"go.opentelemetry.io/otel/attribute"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

func (instance *Instance) Equal(other *Instance) bool {
	return instance.GetFunction().Equal(other.GetFunction()) &&
		instance.GetName() == other.GetName() &&
		instance.GetAddr() == other.GetAddr() &&
		instance.GetReplicaSet() == other.GetReplicaSet() &&
		instance.GetAssignedAt().AsTime().Equal(other.GetAssignedAt().AsTime()) &&
		instance.GetReadyAt().AsTime().Equal(other.GetReadyAt().AsTime()) &&
		instance.GetCpuUsageMilli() == other.GetCpuUsageMilli() &&
		instance.GetMemoryUsageMib() == other.GetMemoryUsageMib()
}

func (instance *Instance) MarshalJSON() ([]byte, error) {
	return json.Marshal(OldInstance{
		OldFunction: OldFunction{
			Namespace:  instance.GetFunction().GetNamespace(),
			Deployment: instance.GetFunction().GetDeployment(),
			Tenant:     instance.GetFunction().GetTenant(),
			Metadata:   instance.GetFunction().GetMetadata(),
			Scale: OldScale{
				MinInstances:           int(instance.GetFunction().GetScale().GetMinInstances()),
				MaxInstances:           int(instance.GetFunction().GetScale().GetMaxInstances()),
				TargetCPUUsageMilli:    int(instance.GetFunction().GetScale().GetTargetCpuUsageMilli()),
				TargetMemoryUsageMiB:   int(instance.GetFunction().GetScale().GetTargetMemoryUsageMib()),
				TargetInFlightRequests: int(instance.GetFunction().GetScale().GetTargetInFlightRequests()),
			},
		},
		Name:           instance.GetName(),
		Addr:           instance.GetAddr(),
		ReplicaSet:     instance.GetReplicaSet(),
		AssignedAt:     instance.GetAssignedAt().AsTime(),
		ReadyAt:        instance.GetReadyAt().AsTime(),
		CPUUsageMilli:  int(instance.GetCpuUsageMilli()),
		MemoryUsageMiB: int(instance.GetMemoryUsageMib()),
	})
}

func (instance *Instance) UnmarshalJSON(data []byte) error {
	var oldInstance OldInstance
	if err := json.Unmarshal(data, &oldInstance); err != nil {
		return err
	}

	scale := new(Scale)
	scale.SetMinInstances(uint32(oldInstance.Scale.MinInstances))
	scale.SetMaxInstances(uint32(oldInstance.Scale.MaxInstances))
	scale.SetTargetCpuUsageMilli(uint32(oldInstance.Scale.TargetCPUUsageMilli))
	scale.SetTargetMemoryUsageMib(uint32(oldInstance.Scale.TargetMemoryUsageMiB))
	scale.SetTargetInFlightRequests(uint32(oldInstance.Scale.TargetInFlightRequests))

	function := new(Function)
	function.SetNamespace(oldInstance.Namespace)
	function.SetDeployment(oldInstance.Deployment)
	function.SetTenant(oldInstance.Tenant)
	function.SetMetadata(oldInstance.Metadata)
	function.SetScale(scale)

	instance.Reset()
	instance.SetFunction(function)
	instance.SetName(oldInstance.Name)
	instance.SetAddr(oldInstance.Addr)
	instance.SetReplicaSet(oldInstance.ReplicaSet)
	instance.SetAssignedAt(timestamppb.New(oldInstance.AssignedAt))
	instance.SetReadyAt(timestamppb.New(oldInstance.ReadyAt))
	instance.SetCpuUsageMilli(uint32(oldInstance.CPUUsageMilli))
	instance.SetMemoryUsageMib(uint32(oldInstance.MemoryUsageMiB))

	return nil
}

func (instance *Instance) Fields() []slog.Attr {
	return []slog.Attr{
		key.Function.Field(instance.GetFunction()),
		key.Name.Field(instance.GetName()),
		key.Addr.Field(instance.GetAddr()),
		slog.String("replica_set", instance.GetReplicaSet()),
		key.AssignedAt.Field(instance.GetAssignedAt().AsTime()),
		key.ReadyAt.Field(instance.GetReadyAt().AsTime()),
		key.CPUUsageMilli.Field(int(instance.GetCpuUsageMilli())),
		key.MemoryUsageMiB.Field(int(instance.GetMemoryUsageMib())),
	}
}

func (instance *Instance) Attributes() []attribute.KeyValue {
	return append(
		key.Function.Attributes(instance.GetFunction()),
		key.Name.Attribute(instance.GetName()),
		key.Addr.Attribute(instance.GetAddr()),
		attribute.String("replica_set", instance.GetReplicaSet()),
		key.AssignedAt.Attribute(instance.GetAssignedAt().AsTime()),
		key.ReadyAt.Attribute(instance.GetReadyAt().AsTime()),
		key.CPUUsageMilli.Attribute(int(instance.GetCpuUsageMilli())),
		key.MemoryUsageMiB.Attribute(int(instance.GetMemoryUsageMib())),
	)
}

func (instance *Instance) AttributesToNotPrefix() []string {
	return []string{"function"}
}
