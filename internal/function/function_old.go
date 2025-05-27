package function

import "time"

type OldScale struct {
	MinInstances           int `json:"min_instances"`
	MaxInstances           int `json:"max_instances"`
	TargetCPUUsageMilli    int `json:"target_cpu_usage_milli"`
	TargetMemoryUsageMiB   int `json:"target_memory_usage_mib"`
	TargetInFlightRequests int `json:"target_in_flight_requests"`
}

type OldFunction struct {
	Namespace  string   `json:"namespace"`
	Deployment string   `json:"deployment"`
	Tenant     string   `json:"tenant"`
	Metadata   string   `json:"metadata"`
	Scale      OldScale `json:"scale"`
}

type OldInstance struct {
	OldFunction
	Name           string    // pod name
	Addr           string    // pod ip : pod port
	ReplicaSet     string    // replica set name
	AssignedAt     time.Time // time the instance was assigned to the pod
	ReadyAt        time.Time // time the instance was ready to receive traffic
	CPUUsageMilli  int       // cpu usage in millicores
	MemoryUsageMiB int       // memory usage in MiB
}

type OldHeartbeat struct {
	Function         Function  `json:"function"`
	Timestamp        time.Time `json:"timestamp"`
	InFlightRequests int       `json:"in_flight_requests"`
}
