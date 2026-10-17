package router

import (
	"fmt"
	"net/http"

	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
)

// instanceFailure is the JSON body of the 502 the router returns when a
// oneshot instance stops before it responds. Callers parse these field
// names (Gadget's api reads them), so treat them as a public contract.
type instanceFailure struct {
	// Error is a human-readable sentence saying what happened.
	Error string `json:"error"`
	// Reason is one of the instanceFailureReason* values.
	Reason string `json:"reason"`
	// Instance is the pod name.
	Instance string `json:"instance"`
	// Container is the container that was OOM-killed or exited.
	Container string `json:"container,omitzero"`
	// ExitCode is the container's exit code. A pointer because 0 is a
	// real exit code.
	ExitCode *int32 `json:"exitCode,omitzero"`
	// MemoryLimitBytes is the OOM-killed container's memory limit.
	MemoryLimitBytes int64 `json:"memoryLimitBytes,omitzero"`
	// TerminatedBy names who terminated the pod: "HeartbeatTimeout" when
	// skipper deleted it, or Kubernetes' DisruptionTarget reason (e.g.
	// "EvictionByEvictionAPI", "TerminationByKubelet").
	TerminatedBy string `json:"terminatedBy,omitzero"`
}

const (
	instanceFailureReasonOOMKilled       = "OOMKilled"
	instanceFailureReasonContainerExited = "ContainerExited"
	instanceFailureReasonPodTerminated   = "PodTerminated"
	instanceFailureReasonUnknown         = "Unknown"
)

func instanceFailureReason(reason skipper.TerminationReason) string {
	switch reason {
	case skipper.TerminationReason_TERMINATION_REASON_OOM_KILLED:
		return instanceFailureReasonOOMKilled
	case skipper.TerminationReason_TERMINATION_REASON_CONTAINER_EXITED:
		return instanceFailureReasonContainerExited
	case skipper.TerminationReason_TERMINATION_REASON_POD_TERMINATED:
		return instanceFailureReasonPodTerminated
	default:
		return instanceFailureReasonUnknown
	}
}

func newInstanceFailure(inst *skipper.Instance, termination *skipper.InstanceTermination, err error) instanceFailure {
	failure := instanceFailure{
		Reason:       instanceFailureReason(termination.GetReason()),
		Instance:     inst.GetName(),
		Container:    termination.GetContainer(),
		TerminatedBy: termination.GetTerminatedBy(),
	}
	if termination.HasExitCode() {
		failure.ExitCode = new(termination.GetExitCode())
	}
	if termination.HasMemoryLimitBytes() {
		failure.MemoryLimitBytes = termination.GetMemoryLimitBytes()
	}

	if failure.Reason == instanceFailureReasonUnknown {
		failure.Error = fmt.Sprintf("lost the connection to instance %s before it responded (%v), and Kubernetes reported no reason the pod stopped", inst.GetName(), err)
	} else {
		failure.Error = fmt.Sprintf("instance %s stopped before it responded: %s", inst.GetName(), termination.Summary())
	}
	return failure
}

// handleProxyError is the reverse proxy's ErrorHandler. It keeps the
// default behaviour (log, empty 502) except when RoundTrip recorded a
// oneshot instance that stopped mid-request, in which case the 502 carries
// an [instanceFailure] body saying why.
func (r *Router) handleProxyError(rw http.ResponseWriter, req *http.Request, err error) {
	r.reverseProxy.ErrorLog.Printf("http: proxy error: %v", err)

	ir := instanceResultFromContext(req.Context())
	if ir == nil || ir.failed == nil {
		rw.WriteHeader(http.StatusBadGateway)
		return
	}

	body, marshalErr := json.Marshal(newInstanceFailure(ir.failed, ir.termination, err))
	if marshalErr != nil {
		r.reverseProxy.ErrorLog.Printf("http: failed to marshal instance failure: %v", marshalErr)
		rw.WriteHeader(http.StatusBadGateway)
		return
	}

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(http.StatusBadGateway)
	_, _ = rw.Write(body)
}
