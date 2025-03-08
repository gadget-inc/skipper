package key

var (
	Addr                      = stringKey{new("address")}
	AssignedAt                = timeKey{new("assigned-at")}
	Attempt                   = intKey{new("attempt")}
	Controller                = stringKey{new("controller")}
	ControllerIP              = stringKey{new("controller-ip")}
	Count                     = intKey{new("count")}
	CurrentInstances          = intKey{new("current-instances")}
	Deployment                = stringKey{new("deployment")}
	DesiredInstances          = intKey{new("desired-instances")}
	Error                     = errorKey{new("error")}
	ForwardedFor              = stringSliceKey{new("forwarded-for")}
	Function                  = groupValueKey{new("function")}
	Heartbeat                 = groupValueKey{new("heartbeat")}
	Instance                  = groupValueKey{new("instance")}
	Labels                    = mapStringString{new("labels")}
	MaxInstances              = intKey{new("max-instances")}
	Metadata                  = stringKey{new("metadata")}
	MinInstances              = intKey{new("min-instances")}
	Name                      = stringKey{new("name")}
	Namespace                 = stringKey{new("namespace")}
	Pod                       = podKey{new("pod")}
	Port                      = stringKey{new("port")}
	ReadyAt                   = timeKey{new("ready-at")}
	ReplicaSet                = replicaSetKey{new("replica-set")}
	Request                   = requestKey{new("request")}
	Requests                  = intKey{new("requests")}
	Response                  = responseKey{new("response")}
	RouterIP                  = stringKey{new("router-ip")}
	Scale                     = groupValueKey{new("scale")}
	TargetCPUUsageMilli       = intKey{new("target-cpu-usage-milli")}
	TargetMemoryUsageMiB      = intKey{new("target-memory-usage-mib")}
	TargetRequestsPerInstance = intKey{new("target-requests-per-instance")}
	Tenant                    = stringKey{new("tenant")}
	Timestamp                 = timeKey{new("timestamp")}
	Token                     = stringKey{new("token")}
)
