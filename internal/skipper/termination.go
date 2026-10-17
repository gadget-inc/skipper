package skipper

import (
	"fmt"
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"k8s.io/apimachinery/pkg/api/resource"
)

// TerminatedByHeartbeatTimeout is the terminated_by value the controller
// records on a oneshot pod it deletes because its function's heartbeats
// timed out.
const TerminatedByHeartbeatTimeout = "HeartbeatTimeout"

var _ slog.LogValuer = (*InstanceTermination)(nil)

// TerminationKey is uncached: terminations are built once per failed
// oneshot request.
var TerminationKey = key.New("termination", (*InstanceTermination).LogValue)

func (t *InstanceTermination) LogValue() slog.Value {
	attrs := []slog.Attr{key.Reason.Slog(t.GetReason().String())}
	if t.HasContainer() {
		attrs = append(attrs, key.Container.Slog(t.GetContainer()))
	}
	if t.HasExitCode() {
		attrs = append(attrs, key.ExitCode.Slog(int(t.GetExitCode())))
	}
	if t.HasMemoryLimitBytes() {
		attrs = append(attrs, key.MemoryLimitBytes.Slog(int(t.GetMemoryLimitBytes())))
	}
	if t.HasTerminatedBy() {
		attrs = append(attrs, key.TerminatedBy.Slog(t.GetTerminatedBy()))
	}
	if t.HasMessage() {
		attrs = append(attrs, key.Message.Slog(t.GetMessage()))
	}
	return slog.GroupValue(attrs...)
}

// Summary describes the termination in one clause, e.g. `container "main"
// ran out of memory and was killed at its 6Gi limit (exit code 137)`.
func (t *InstanceTermination) Summary() string {
	switch t.GetReason() {
	case TerminationReason_TERMINATION_REASON_OOM_KILLED:
		s := fmt.Sprintf("container %q ran out of memory and was killed", t.GetContainer())
		if t.HasMemoryLimitBytes() {
			s += " at its " + resource.NewQuantity(t.GetMemoryLimitBytes(), resource.BinarySI).String() + " limit"
		}
		return s + fmt.Sprintf(" (exit code %d)", t.GetExitCode())
	case TerminationReason_TERMINATION_REASON_CONTAINER_EXITED:
		s := fmt.Sprintf("container %q exited with code %d", t.GetContainer(), t.GetExitCode())
		if t.HasMessage() {
			s += ": " + t.GetMessage()
		}
		return s
	case TerminationReason_TERMINATION_REASON_POD_TERMINATED:
		var s string
		switch t.GetTerminatedBy() {
		case "":
			s = "the pod was terminated"
		case TerminatedByHeartbeatTimeout:
			s = "skipper deleted the pod because its function's heartbeats timed out"
		default:
			s = "the pod was terminated by " + t.GetTerminatedBy()
		}
		if t.HasMessage() {
			s += ": " + t.GetMessage()
		}
		return s
	default:
		return "Kubernetes reported no reason the pod stopped"
	}
}
