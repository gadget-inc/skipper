package function

import (
	"errors"

	"github.com/gadget-inc/fusion/internal/key"
)

var (
	ErrMissingAssignedAt              = errors.New("missing " + key.AssignedAt.Underscored)
	ErrMissingDeployment              = errors.New("missing " + key.Deployment.Underscored)
	ErrMissingMaxInstances            = errors.New("missing " + key.MaxInstances.Underscored)
	ErrMissingMetadata                = errors.New("missing " + key.Metadata.Underscored)
	ErrMissingMinInstances            = errors.New("missing " + key.MinInstances.Underscored)
	ErrMissingNamespace               = errors.New("missing " + key.Namespace.Underscored)
	ErrMissingPort                    = errors.New("missing " + key.Port.Underscored)
	ErrMissingReplicaSet              = errors.New("missing " + key.ReplicaSet.Underscored)
	ErrMissingTargetCPUUtilization    = errors.New("missing " + key.TargetCPUUtilization.Underscored)
	ErrMissingTargetMemoryUtilization = errors.New("missing " + key.TargetMemoryUtilization.Underscored)
	ErrMissingTenant                  = errors.New("missing " + key.Tenant.Underscored)

	ErrInvalidAssignedAt              = errors.New("invalid " + key.AssignedAt.Underscored)
	ErrInvalidMaxInstances            = errors.New("invalid " + key.MaxInstances.Underscored)
	ErrInvalidMinInstances            = errors.New("invalid " + key.MinInstances.Underscored)
	ErrInvalidReadyAt                 = errors.New("invalid " + key.ReadyAt.Underscored)
	ErrInvalidTargetCPUUtilization    = errors.New("invalid " + key.TargetCPUUtilization.Underscored)
	ErrInvalidTargetMemoryUtilization = errors.New("invalid " + key.TargetMemoryUtilization.Underscored)
)
