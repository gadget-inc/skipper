package skipper

import "time"

// This file declares the per-tenant policy resolvers on *Assignment. Each
// resolver gives a single edit point where a decision site can pull a knob
// from the tenant-supplied assignment, falling back to a cluster default
// (passed as an argument) when the tenant did not override it.
//
// Resolver semantics:
//
//   - Hybrid scale fields (the five flat fields with a Scale sub-message
//     twin): resolvedScaleX returns (value, present). Public resolvers
//     prefer the flat field, then fall back to Scale. Cluster defaults
//     do not apply -- scale.min/max/target_* originate from the header.
//   - Wired non-scale fields: prefer the explicit tenant value (proto3
//     Has*() returns true), else fall back to the cluster default arg.
//   - Placeholder fields: same resolver shape so a followup plan wiring
//     the knob has a single edit point. No decision site reads these in
//     the current release.

// resolvedScaleMin returns the resolved minimum-instances value across the
// flat scale_min_instances field and the nested Scale sub-message, preferring
// flat. The bool reports whether either form provided a value.
func (a *Assignment) resolvedScaleMin() (uint32, bool) {
	if a.HasScaleMinInstances() {
		return a.GetScaleMinInstances(), true
	}
	if scale := a.GetScale(); scale != nil && scale.HasMinInstances() {
		return scale.GetMinInstances(), true
	}
	return 0, false
}

// resolvedScaleMax returns the resolved maximum-instances value across flat
// and nested. The bool reports whether either form provided a value.
func (a *Assignment) resolvedScaleMax() (uint32, bool) {
	if a.HasScaleMaxInstances() {
		return a.GetScaleMaxInstances(), true
	}
	if scale := a.GetScale(); scale != nil && scale.HasMaxInstances() {
		return scale.GetMaxInstances(), true
	}
	return 0, false
}

// resolvedScaleTargetCPUMillicores resolves the per-instance CPU target
// across the flat field and the nested target_cpu_usage_milli field.
func (a *Assignment) resolvedScaleTargetCPUMillicores() (uint32, bool) {
	if a.HasScaleTargetCpuMillicores() {
		return a.GetScaleTargetCpuMillicores(), true
	}
	if scale := a.GetScale(); scale != nil && scale.HasTargetCpuUsageMilli() {
		return scale.GetTargetCpuUsageMilli(), true
	}
	return 0, false
}

// resolvedScaleTargetMemoryMebibytes resolves the per-instance memory target
// across the flat field and the nested target_memory_usage_mib field.
func (a *Assignment) resolvedScaleTargetMemoryMebibytes() (uint32, bool) {
	if a.HasScaleTargetMemoryMebibytes() {
		return a.GetScaleTargetMemoryMebibytes(), true
	}
	if scale := a.GetScale(); scale != nil && scale.HasTargetMemoryUsageMib() {
		return scale.GetTargetMemoryUsageMib(), true
	}
	return 0, false
}

// resolvedScaleTargetInFlightRequests resolves the per-instance in-flight
// request target across the flat field and the nested
// target_in_flight_requests field.
func (a *Assignment) resolvedScaleTargetInFlightRequests() (uint32, bool) {
	if a.HasScaleTargetInFlightRequests() {
		return a.GetScaleTargetInFlightRequests(), true
	}
	if scale := a.GetScale(); scale != nil && scale.HasTargetInFlightRequests() {
		return scale.GetTargetInFlightRequests(), true
	}
	return 0, false
}

// ScaleMinInstances returns the assignment's resolved minimum-instances value.
// Prefers the flat scale_min_instances field, falls back to scale.min_instances.
func (a *Assignment) ScaleMinInstances() uint32 {
	v, _ := a.resolvedScaleMin()
	return v
}

// ScaleMaxInstances returns the assignment's resolved maximum-instances value.
// Prefers the flat scale_max_instances field, falls back to scale.max_instances.
func (a *Assignment) ScaleMaxInstances() uint32 {
	v, _ := a.resolvedScaleMax()
	return v
}

// ScaleTargetCPUMillicores returns the assignment's resolved per-instance CPU
// target in millicores. Prefers the flat scale_target_cpu_millicores field,
// falls back to scale.target_cpu_usage_milli.
func (a *Assignment) ScaleTargetCPUMillicores() uint32 {
	v, _ := a.resolvedScaleTargetCPUMillicores()
	return v
}

// ScaleTargetMemoryMebibytes returns the assignment's resolved per-instance
// memory target in mebibytes. Prefers the flat scale_target_memory_mebibytes
// field, falls back to scale.target_memory_usage_mib.
func (a *Assignment) ScaleTargetMemoryMebibytes() uint32 {
	v, _ := a.resolvedScaleTargetMemoryMebibytes()
	return v
}

// ScaleTargetInFlightRequests returns the assignment's resolved per-instance
// in-flight request target. Prefers the flat field, falls back to the nested
// scale.target_in_flight_requests.
func (a *Assignment) ScaleTargetInFlightRequests() uint32 {
	v, _ := a.resolvedScaleTargetInFlightRequests()
	return v
}

// ScaleTolerance returns the HPA usage-ratio tolerance. Returns the tenant
// override if scale_tolerance is set, else the cluster default.
func (a *Assignment) ScaleTolerance(cluster float64) float64 {
	if a.HasScaleTolerance() {
		return a.GetScaleTolerance()
	}
	return cluster
}

// ScaleDownscaleStabilization returns the downscale stabilization window.
// Returns the tenant override if scale_downscale_stabilization is set, else
// the cluster default.
func (a *Assignment) ScaleDownscaleStabilization(cluster time.Duration) time.Duration {
	if a.HasScaleDownscaleStabilization() {
		return a.GetScaleDownscaleStabilization().AsDuration()
	}
	return cluster
}

// ScaleInitialReadinessDelay returns the per-pod initial-readiness delay
// used by the HPA's CPU metric. Returns the tenant override if set, else the
// cluster default.
func (a *Assignment) ScaleInitialReadinessDelay(cluster time.Duration) time.Duration {
	if a.HasScaleInitialReadinessDelay() {
		return a.GetScaleInitialReadinessDelay().AsDuration()
	}
	return cluster
}

// ZoneSpread returns the resolved zone-spread mode. Placeholder (followup:
// zone-aware-placement); no decision site reads this in the current release.
func (a *Assignment) ZoneSpread() ZoneSpread {
	return a.GetZoneSpread()
}

// ZoneMin returns the minimum number of zones the controller should spread
// instances across. Placeholder (followup: zone-aware-placement); no decision
// site reads this in the current release.
func (a *Assignment) ZoneMin() uint32 {
	return a.GetZoneMin()
}

// ZoneAffinity returns the resolved zone-affinity mode used by the router.
// Placeholder (followup: zone-aware-affinity); no decision site reads this
// in the current release.
func (a *Assignment) ZoneAffinity() ZoneAffinity {
	return a.GetZoneAffinity()
}

// AssignPath returns the per-assignment override of the controller's assign
// POST path. Placeholder (followup: per-assignment-assign-path); the
// controller does not read this in the current release. Returns the cluster
// default when unset.
func (a *Assignment) AssignPath(cluster string) string {
	if a.HasAssignPath() {
		return a.GetAssignPath()
	}
	return cluster
}

// AssignTimeout returns the per-assignment override of the controller's
// assign POST timeout, falling back to the cluster default when unset.
func (a *Assignment) AssignTimeout(cluster time.Duration) time.Duration {
	if a.HasAssignTimeout() {
		return a.GetAssignTimeout().AsDuration()
	}
	return cluster
}

// AssignTokenTTL returns the per-assignment override of the PASETO token's
// time-to-live, falling back to the cluster default when unset.
func (a *Assignment) AssignTokenTTL(cluster time.Duration) time.Duration {
	if a.HasAssignTokenTtl() {
		return a.GetAssignTokenTtl().AsDuration()
	}
	return cluster
}

// HeartbeatInterval returns the per-assignment override of the router's
// heartbeat interval. Placeholder (followup: per-assignment-heartbeat-
// interval); no decision site reads this in the current release. Returns the
// cluster default when unset.
func (a *Assignment) HeartbeatInterval(cluster time.Duration) time.Duration {
	if a.HasHeartbeatInterval() {
		return a.GetHeartbeatInterval().AsDuration()
	}
	return cluster
}

// HeartbeatTimeout returns the per-assignment override of the controller's
// heartbeat timeout used for scale-to-zero and pod-cleanup decisions,
// falling back to the cluster default when unset.
func (a *Assignment) HeartbeatTimeout(cluster time.Duration) time.Duration {
	if a.HasHeartbeatTimeout() {
		return a.GetHeartbeatTimeout().AsDuration()
	}
	return cluster
}

// RetryMaxAttempts returns the per-assignment override of the router's
// max round-trip attempts. Returns the tenant override if retry_max_attempts
// is set, else the cluster default.
func (a *Assignment) RetryMaxAttempts(cluster int) int {
	if a.HasRetryMaxAttempts() {
		return int(a.GetRetryMaxAttempts())
	}
	return cluster
}

// RetryMinBackoff returns the per-assignment override of the router's
// minimum retry backoff, falling back to the cluster default when unset.
func (a *Assignment) RetryMinBackoff(cluster time.Duration) time.Duration {
	if a.HasRetryMinBackoff() {
		return a.GetRetryMinBackoff().AsDuration()
	}
	return cluster
}

// RetryMaxBackoff returns the per-assignment override of the router's
// maximum retry backoff, falling back to the cluster default when unset.
func (a *Assignment) RetryMaxBackoff(cluster time.Duration) time.Duration {
	if a.HasRetryMaxBackoff() {
		return a.GetRetryMaxBackoff().AsDuration()
	}
	return cluster
}

// RetryBackpressure returns the per-assignment override of the router's
// backpressure policy. Placeholder (followup: backpressure); no decision
// site reads this in the current release.
func (a *Assignment) RetryBackpressure() Backpressure {
	return a.GetRetryBackpressure()
}

// RetryStatusCodes returns the per-assignment override of the HTTP status
// codes that should trigger a retry. Placeholder (followup: backpressure);
// no decision site reads this in the current release.
func (a *Assignment) RetryStatusCodes() []uint32 {
	return a.GetRetryStatusCodes()
}

// TransportDialTimeout returns the per-assignment override of the router
// transport's dial timeout. Placeholder (followup: per-assignment-transport);
// no decision site reads this in the current release.
func (a *Assignment) TransportDialTimeout(cluster time.Duration) time.Duration {
	if a.HasTransportDialTimeout() {
		return a.GetTransportDialTimeout().AsDuration()
	}
	return cluster
}

// TransportKeepalive returns the per-assignment override of the router
// transport's keepalive interval. Placeholder (followup: per-assignment-
// transport); no decision site reads this in the current release.
func (a *Assignment) TransportKeepalive(cluster time.Duration) time.Duration {
	if a.HasTransportKeepalive() {
		return a.GetTransportKeepalive().AsDuration()
	}
	return cluster
}

// TransportIdleConnTimeout returns the per-assignment override of the router
// transport's idle-connection timeout. Placeholder (followup: per-assignment-
// transport); no decision site reads this in the current release.
func (a *Assignment) TransportIdleConnTimeout(cluster time.Duration) time.Duration {
	if a.HasTransportIdleConnTimeout() {
		return a.GetTransportIdleConnTimeout().AsDuration()
	}
	return cluster
}

// TransportTLSHandshakeTimeout returns the per-assignment override of the
// router transport's TLS handshake timeout. Placeholder (followup:
// per-assignment-transport); no decision site reads this in the current
// release.
func (a *Assignment) TransportTLSHandshakeTimeout(cluster time.Duration) time.Duration {
	if a.HasTransportTlsHandshakeTimeout() {
		return a.GetTransportTlsHandshakeTimeout().AsDuration()
	}
	return cluster
}

// TransportMaxIdleConns returns the per-assignment override of the router
// transport's max idle connections. Placeholder (followup: per-assignment-
// transport); no decision site reads this in the current release.
func (a *Assignment) TransportMaxIdleConns(cluster int) int {
	if a.HasTransportMaxIdleConns() {
		return int(a.GetTransportMaxIdleConns())
	}
	return cluster
}

// TransportForceHTTP2 returns the per-assignment override of the router
// transport's force-HTTP/2 setting. Placeholder (followup: per-assignment-
// transport); no decision site reads this in the current release.
func (a *Assignment) TransportForceHTTP2(cluster bool) bool {
	if a.HasTransportForceHttp2() {
		return a.GetTransportForceHttp2()
	}
	return cluster
}

// TransportDisableCompression returns the per-assignment override of the
// router transport's disable-compression setting. Placeholder (followup:
// per-assignment-transport); no decision site reads this in the current
// release.
func (a *Assignment) TransportDisableCompression(cluster bool) bool {
	if a.HasTransportDisableCompression() {
		return a.GetTransportDisableCompression()
	}
	return cluster
}

// TransportFlushInterval returns the per-assignment override of the router
// reverse-proxy's response flush interval. Placeholder (followup: per-
// assignment-transport); no decision site reads this in the current release.
func (a *Assignment) TransportFlushInterval(cluster time.Duration) time.Duration {
	if a.HasTransportFlushInterval() {
		return a.GetTransportFlushInterval().AsDuration()
	}
	return cluster
}
