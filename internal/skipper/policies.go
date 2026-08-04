package skipper

import (
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
)

var (
	_ slog.LogValuer = (*HpaPolicy)(nil)
	_ slog.LogValuer = (*HeartbeatPolicy)(nil)
	_ slog.LogValuer = (*ProxyPolicy)(nil)
	_ slog.LogValuer = (*LifecyclePolicy)(nil)
)

var (
	HpaPolicyKey       = key.New("hpa", (*HpaPolicy).LogValue)
	HeartbeatPolicyKey = key.New("heartbeat_policy", (*HeartbeatPolicy).LogValue)
	ProxyPolicyKey     = key.New("proxy", (*ProxyPolicy).LogValue)
	LifecyclePolicyKey = key.New("lifecycle", (*LifecyclePolicy).LogValue)
)

func (p *HpaPolicy) LogValue() slog.Value {
	return slog.GroupValue(
		key.Tolerance.Slog(p.GetTolerance()),
		key.DownscaleStabilization.Slog(p.GetDownscaleStabilization().AsDuration()),
		key.InitialReadinessDelay.Slog(p.GetInitialReadinessDelay().AsDuration()),
	)
}

func (p *HeartbeatPolicy) LogValue() slog.Value {
	return slog.GroupValue(
		key.HeartbeatTimeout.Slog(p.GetTimeout().AsDuration()),
	)
}

func (p *ProxyPolicy) LogValue() slog.Value {
	return slog.GroupValue(
		key.MaxAttempts.Slog(p.GetMaxAttempts()),
		key.RetryMinBackoff.Slog(p.GetRetryMinBackoff().AsDuration()),
		key.RetryMaxBackoff.Slog(p.GetRetryMaxBackoff().AsDuration()),
	)
}

func (p *LifecyclePolicy) LogValue() slog.Value {
	return slog.GroupValue(
		key.AssignTimeout.Slog(p.GetAssignTimeout().AsDuration()),
		key.TokenTTL.Slog(p.GetTokenTtl().AsDuration()),
	)
}
