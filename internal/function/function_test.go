package function

import (
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
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
	assert.NilError(t, err)

	oldJSON, err := json.Marshal(oldInstance)
	assert.NilError(t, err)
	assert.Assert(t, string(oldJSON) == string(newJSON))

	oldInstance = new(OldInstance)
	assert.NilError(t, json.Unmarshal(newJSON, oldInstance))

	newInstance = new(Instance)
	assert.NilError(t, json.Unmarshal(oldJSON, newInstance))

	assert.Assert(t, newInstance.GetFunction().GetNamespace() == oldInstance.Namespace)
	assert.Assert(t, newInstance.GetFunction().GetDeployment() == oldInstance.Deployment)
	assert.Assert(t, newInstance.GetFunction().GetTenant() == oldInstance.Tenant)
	assert.Assert(t, newInstance.GetFunction().GetMetadata() == oldInstance.Metadata)
	assert.Assert(t, int(newInstance.GetFunction().GetScale().GetMinInstances()) == oldInstance.Scale.MinInstances)
	assert.Assert(t, int(newInstance.GetFunction().GetScale().GetMaxInstances()) == oldInstance.Scale.MaxInstances)
	assert.Assert(t, int(newInstance.GetFunction().GetScale().GetTargetCpuUsageMilli()) == oldInstance.Scale.TargetCPUUsageMilli)
	assert.Assert(t, int(newInstance.GetFunction().GetScale().GetTargetMemoryUsageMib()) == oldInstance.Scale.TargetMemoryUsageMiB)
	assert.Assert(t, int(newInstance.GetFunction().GetScale().GetTargetInFlightRequests()) == oldInstance.Scale.TargetInFlightRequests)
	assert.Assert(t, newInstance.GetName() == oldInstance.Name)
	assert.Assert(t, newInstance.GetAddr() == oldInstance.Addr)
	assert.Assert(t, newInstance.GetReplicaSet() == oldInstance.ReplicaSet)
	assert.Assert(t, newInstance.GetAssignedAt().AsTime().Equal(oldInstance.AssignedAt))
	assert.Assert(t, newInstance.GetReadyAt().AsTime().Equal(oldInstance.ReadyAt))
	assert.Assert(t, int(newInstance.GetCpuUsageMilli()) == oldInstance.CPUUsageMilli)
	assert.Assert(t, int(newInstance.GetMemoryUsageMib()) == oldInstance.MemoryUsageMiB)
}
