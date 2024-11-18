package key

var (
	AssignedAt              = timeKey{new("assigned-at")}
	Controller              = stringKey{new("controller")}
	Deployment              = stringKey{new("deployment")}
	Function                = stringerKey{new("function")}
	Error                   = stringerKey{new("error")}
	MaxReplicas             = intKey{new("max-replicas")}
	Metadata                = stringKey{new("metadata")}
	MinReplicas             = intKey{new("min-replicas")}
	Namespace               = stringKey{new("namespace")}
	ReadyAt                 = timeKey{new("ready-at")}
	Status                  = stringKey{new("status")}
	TargetCPUUtilization    = intKey{new("target-cpu-utilization")}
	TargetMemoryUtilization = intKey{new("target-memory-utilization")}
	Tenant                  = stringKey{new("tenant")}
)
