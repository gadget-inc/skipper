package key

var (
	AssignedAt        = timeKey{new("assigned-at")}
	Assignment        = stringKey{new("assignment")}
	Controller        = stringKey{new("controller")}
	CpuUtilization    = intKey{new("cpu-utilization")}
	Deployment        = stringKey{new("deployment")}
	Destination       = stringerKey{new("destination")}
	Error             = stringerKey{new("error")}
	MemoryUtilization = intKey{new("memory-utilization")}
	Namespace         = stringKey{new("namespace")}
	Replicas          = intKey{new("replicas")}
	Status            = stringKey{new("status")}
	Tenant            = stringKey{new("tenant")}
)
