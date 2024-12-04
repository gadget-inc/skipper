package key

var (
	AssignedAt              = timeKey{new("assigned-at")}
	Controller              = stringKey{new("controller")}
	CurrentReplicas         = intKey{new("current-replicas")}
	Deployment              = deploymentKey{new("deployment")}
	DesiredInstances        = intKey{new("desired-instances")}
	Error                   = errorKey{new("error")}
	ForwardedFor            = stringSliceKey{new("forwarded-for")}
	Function                = groupValueKey{new("function")}
	IP                      = stringKey{new("ip")}
	Instance                = groupValueKey{new("instance")}
	LastRequest             = timeKey{new("last-request")}
	MaxReplicas             = intKey{new("max-replicas")}
	Metadata                = stringKey{new("metadata")}
	MinReplicas             = intKey{new("min-replicas")}
	Name                    = stringKey{new("name")}
	Namespace               = stringKey{new("namespace")}
	Pod                     = podKey{new("pod")}
	ReadyAt                 = timeKey{new("ready-at")}
	ReplicaSet              = replicaSetKey{new("replica-set")}
	Status                  = stringKey{new("status")}
	TargetCPUUtilization    = intKey{new("target-cpu-utilization")}
	TargetMemoryUtilization = intKey{new("target-memory-utilization")}
	Tenant                  = stringKey{new("tenant")}
	Traffic                 = stringKey{new("traffic")}
)
