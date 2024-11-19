package key

var (
	AssignedAt              = timeKey{new("assigned-at")}
	Controller              = stringKey{new("controller")}
	CurrentReplicas         = intKey{new("current-replicas")}
	Deployment              = deploymentKey{new("deployment")}
	DesiredReplicas         = intKey{new("desired-replicas")}
	Error                   = errorKey{new("error")}
	Function                = groupValueKey{new("function")}
	LastRequest             = timeKey{new("last-request")}
	MaxReplicas             = intKey{new("max-replicas")}
	Metadata                = stringKey{new("metadata")}
	MinReplicas             = intKey{new("min-replicas")}
	Namespace               = stringKey{new("namespace")}
	Pod                     = podKey{new("pod")}
	ReadyAt                 = timeKey{new("ready-at")}
	Status                  = stringKey{new("status")}
	TargetCPUUtilization    = intKey{new("target-cpu-utilization")}
	TargetMemoryUtilization = intKey{new("target-memory-utilization")}
	Tenant                  = stringKey{new("tenant")}
	Traffic                 = stringKey{new("traffic")}
)
