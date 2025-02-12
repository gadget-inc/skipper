package key

var (
	Addr                    = stringKey{new("address")}
	AssignedAt              = timeKey{new("assigned-at")}
	Attempt                 = intKey{new("attempt")}
	Body                    = stringKey{new("body")}
	Controller              = stringKey{new("controller")}
	ControllerIP            = stringKey{new("controller-ip")}
	ControllerIPs           = stringSliceKey{new("controller-ips")}
	Count                   = intKey{new("count")}
	CurrentInstances        = intKey{new("current-instances")}
	Deployment              = stringKey{new("deployment")}
	DesiredInstances        = intKey{new("desired-instances")}
	Duration                = durationKey{new("duration")}
	Error                   = errorKey{new("error")}
	ForwardedFor            = stringSliceKey{new("forwarded-for")}
	Function                = groupValueKey{new("function")}
	IP                      = stringKey{new("ip")}
	Instance                = groupValueKey{new("instance")}
	Labels                  = mapStringString{new("labels")}
	MaxInstances            = intKey{new("max-instances")}
	MaxRecommendedInstances = intKey{new("max-recommended-instances")}
	Metadata                = stringKey{new("metadata")}
	MinInstances            = intKey{new("min-instances")}
	Name                    = stringKey{new("name")}
	Namespace               = stringKey{new("namespace")}
	Namespaces              = stringSliceKey{new("namespaces")}
	Pod                     = podKey{new("pod")}
	Port                    = intKey{new("port")}
	ReadyAt                 = timeKey{new("ready-at")}
	ReplicaSet              = replicaSetKey{new("replica-set")}
	Signal                  = stringKey{new("signal")}
	Status                  = stringKey{new("status")}
	StatusCode              = intKey{new("status-code")}
	TargetCPUUtilization    = intKey{new("target-cpu-utilization")}
	TargetMemoryUtilization = intKey{new("target-memory-utilization")}
	Tenant                  = stringKey{new("tenant")}
	Timestamp               = timeKey{new("timestamp")}
	Token                   = stringKey{new("token")}
)
