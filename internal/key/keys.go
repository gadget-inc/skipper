package key

var (
	Addr                    = stringKey{new("address")}
	AssignedAt              = timeKey{new("assigned-at")}
	Attempt                 = intKey{new("attempt")}
	Controller              = stringKey{new("controller")}
	CurrentInstances        = intKey{new("current-instances")}
	Deployment              = stringKey{new("deployment")}
	DesiredInstances        = intKey{new("desired-instances")}
	Duration                = durationKey{new("duration")}
	Error                   = errorKey{new("error")}
	ForwardedFor            = stringSliceKey{new("forwarded-for")}
	Function                = groupValueKey{new("function")}
	Instance                = groupValueKey{new("instance")}
	MaxInstances            = intKey{new("max-instances")}
	Metadata                = stringKey{new("metadata")}
	MinInstances            = intKey{new("min-instances")}
	Name                    = stringKey{new("name")}
	Namespace               = stringKey{new("namespace")}
	Pod                     = podKey{new("pod")}
	ReadyAt                 = timeKey{new("ready-at")}
	ReplicaSet              = replicaSetKey{new("replica-set")}
	Status                  = stringKey{new("status")}
	TargetCPUUtilization    = intKey{new("target-cpu-utilization")}
	TargetMemoryUtilization = intKey{new("target-memory-utilization")}
	Tenant                  = stringKey{new("tenant")}
	Timestamp               = timeKey{new("timestamp")}
)
