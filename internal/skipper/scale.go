package skipper

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
)

var _ slog.LogValuer = (*Scale)(nil)

var ScaleKey = key.NewLogValuer[*Scale]("scale")

func (s *Scale) LogValue() slog.Value {
	return slog.GroupValue(
		key.MinInstances.Slog(s.GetMinInstances()),
		key.MaxInstances.Slog(s.GetMaxInstances()),
		key.TargetCPUUsageMilli.Slog(s.GetTargetCpuUsageMilli()),
		key.TargetMemoryUsageMiB.Slog(s.GetTargetMemoryUsageMib()),
		key.TargetInFlightRequests.Slog(s.GetTargetInFlightRequests()),
	)
}

var _ slog.LogValuer = (*ScaleDecision)(nil)

var ScaleDecisionKey = key.NewLogValuer[*ScaleDecision]("scale_decision")

func (sd *ScaleDecision) LogValue() slog.Value {
	var metricAttrs []slog.Attr
	for _, metric := range sd.GetMetrics() {
		metricAttrs = append(metricAttrs, slog.Float64(metric.GetName(), metric.GetValue()))
	}

	return slog.GroupValue(
		key.DesiredInstances.Slog(sd.GetDesiredInstances()),
		key.UnclampedDesiredInstances.Slog(sd.GetUnclampedDesiredInstances()),
		key.Reason.Slog(sd.GetReason().String()),
		slog.GroupAttrs("metrics", metricAttrs...),
	)
}
