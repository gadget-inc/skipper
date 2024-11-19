package function

import (
	"errors"

	"github.com/gadget-inc/fusion/internal/key"
)

var (
	ErrMissingAssignedAt              = errors.New("missing " + key.AssignedAt.Underscored)
	ErrMissingDeployment              = errors.New("missing " + key.Deployment.Underscored)
	ErrMissingMaxReplicas             = errors.New("missing " + key.MaxReplicas.Underscored)
	ErrMissingMetadata                = errors.New("missing " + key.Metadata.Underscored)
	ErrMissingMinReplicas             = errors.New("missing " + key.MinReplicas.Underscored)
	ErrMissingNamespace               = errors.New("missing " + key.Namespace.Underscored)
	ErrMissingTargetCPUUtilization    = errors.New("missing " + key.TargetCPUUtilization.Underscored)
	ErrMissingTargetMemoryUtilization = errors.New("missing " + key.TargetMemoryUtilization.Underscored)
	ErrMissingTenant                  = errors.New("missing " + key.Tenant.Underscored)

	ErrInvalidAssignedAt              = errors.New("invalid " + key.AssignedAt.Underscored)
	ErrInvalidMaxReplicas             = errors.New("invalid " + key.MaxReplicas.Underscored)
	ErrInvalidMinReplicas             = errors.New("invalid " + key.MinReplicas.Underscored)
	ErrInvalidReadyAt                 = errors.New("invalid " + key.ReadyAt.Underscored)
	ErrInvalidTargetCPUUtilization    = errors.New("invalid " + key.TargetCPUUtilization.Underscored)
	ErrInvalidTargetMemoryUtilization = errors.New("invalid " + key.TargetMemoryUtilization.Underscored)
)
