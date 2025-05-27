package function

import (
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	"github.com/shoenig/test/must"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

func TestInstanceJSON(t *testing.T) {
	oldInstance := &OldInstance{
		OldFunction: OldFunction{
			Namespace:  "test-namespace",
			Deployment: "test-deployment",
			Tenant:     "test-tenant",
			Metadata:   "test-metadata",
			Scale: OldScale{
				MinInstances:           1,
				MaxInstances:           10,
				TargetCPUUsageMilli:    500,
				TargetMemoryUsageMiB:   1024,
				TargetInFlightRequests: 100,
			},
		},
		Name:           "test-name",
		Addr:           "test-addr",
		ReplicaSet:     "test-replica-set",
		AssignedAt:     time.Now().UTC(),
		ReadyAt:        time.Now().UTC(),
		CPUUsageMilli:  100,
		MemoryUsageMiB: 1024,
	}

	newInstance := Instance_builder{
		Function: Function_builder{
			Namespace:  oldInstance.Namespace,
			Deployment: oldInstance.Deployment,
			Tenant:     oldInstance.Tenant,
			Metadata:   oldInstance.Metadata,
			Scale: Scale_builder{
				MinInstances:           uint32(oldInstance.Scale.MinInstances),
				MaxInstances:           uint32(oldInstance.Scale.MaxInstances),
				TargetCpuUsageMilli:    uint32(oldInstance.Scale.TargetCPUUsageMilli),
				TargetMemoryUsageMib:   uint32(oldInstance.Scale.TargetMemoryUsageMiB),
				TargetInFlightRequests: uint32(oldInstance.Scale.TargetInFlightRequests),
			}.Build(),
		}.Build(),
		Name:           oldInstance.Name,
		Addr:           oldInstance.Addr,
		ReplicaSet:     oldInstance.ReplicaSet,
		AssignedAt:     timestamppb.New(oldInstance.AssignedAt),
		ReadyAt:        timestamppb.New(oldInstance.ReadyAt),
		CpuUsageMilli:  uint32(oldInstance.CPUUsageMilli),
		MemoryUsageMib: uint32(oldInstance.MemoryUsageMiB),
	}.Build()

	newJSON, err := json.Marshal(newInstance)
	must.NoError(t, err)

	oldJSON, err := json.Marshal(oldInstance)
	must.NoError(t, err)
	must.EqOp(t, string(oldJSON), string(newJSON))

	oldInstance = new(OldInstance)
	must.NoError(t, json.Unmarshal(newJSON, oldInstance))

	newInstance = new(Instance)
	must.NoError(t, json.Unmarshal(oldJSON, newInstance))

	must.Eq(t, oldInstance.Namespace, newInstance.GetFunction().GetNamespace())
	must.Eq(t, oldInstance.Deployment, newInstance.GetFunction().GetDeployment())
	must.Eq(t, oldInstance.Tenant, newInstance.GetFunction().GetTenant())
	must.Eq(t, oldInstance.Metadata, newInstance.GetFunction().GetMetadata())
	must.Eq(t, oldInstance.Scale.MinInstances, int(newInstance.GetFunction().GetScale().GetMinInstances()))
	must.Eq(t, oldInstance.Scale.MaxInstances, int(newInstance.GetFunction().GetScale().GetMaxInstances()))
	must.Eq(t, oldInstance.Scale.TargetCPUUsageMilli, int(newInstance.GetFunction().GetScale().GetTargetCpuUsageMilli()))
	must.Eq(t, oldInstance.Scale.TargetMemoryUsageMiB, int(newInstance.GetFunction().GetScale().GetTargetMemoryUsageMib()))
	must.Eq(t, oldInstance.Scale.TargetInFlightRequests, int(newInstance.GetFunction().GetScale().GetTargetInFlightRequests()))
	must.Eq(t, oldInstance.Name, newInstance.GetName())
	must.Eq(t, oldInstance.Addr, newInstance.GetAddr())
	must.Eq(t, oldInstance.ReplicaSet, newInstance.GetReplicaSet())
	must.Eq(t, oldInstance.AssignedAt, newInstance.GetAssignedAt().AsTime())
	must.Eq(t, oldInstance.ReadyAt, newInstance.GetReadyAt().AsTime())
	must.Eq(t, oldInstance.CPUUsageMilli, int(newInstance.GetCpuUsageMilli()))
	must.Eq(t, oldInstance.MemoryUsageMiB, int(newInstance.GetMemoryUsageMib()))
}
