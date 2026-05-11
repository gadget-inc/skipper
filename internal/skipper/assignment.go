package skipper

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
	lru "github.com/hashicorp/golang-lru/v2"
)

// assignmentHeaderCacheMaxSize is the maximum number of distinct header values
// retained in the parse cache. Each entry holds one parsed *Assignment. The
// router sees at most one distinct header per active assignment × scale variant,
// so 4096 is generous for any realistic fleet size.
const assignmentHeaderCacheMaxSize = 4096

// assignmentHeaderCache is a bounded LRU cache of parsed Assignment pointers,
// keyed by the raw header string to avoid redundant JSON unmarshalling.
type assignmentHeaderCacheType = lru.Cache[string, *Assignment]

func newAssignmentHeaderCache(capacity int) *assignmentHeaderCacheType {
	c, _ := lru.New[string, *Assignment](capacity)
	return c
}

var assignmentHeaderCache = newAssignmentHeaderCache(assignmentHeaderCacheMaxSize)

// AssignmentHash is a unique identifier for an Assignment, suitable for use as a map key.
type AssignmentHash = uint64

// Hash returns a hash of the assignment's identity fields (namespace, deployment,
// tenant, oneshot), suitable for use as a map key. Metadata and scale fields are
// excluded so that changes to those fields don't create a new assignment identity.
func (a *Assignment) Hash() AssignmentHash {
	h := xxhash.New()
	_, _ = h.WriteString(a.GetNamespace())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(a.GetDeployment())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(a.GetTenant())
	_, _ = h.Write([]byte{0})
	if a.GetOneshot() {
		_, _ = h.Write([]byte{1})
	} else {
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

var _ slog.LogValuer = (*Assignment)(nil)

// LegacyFunctionKey is the back-compat shim that emits the "function"
// vocabulary on telemetry, headers, and Kubernetes labels — paired with
// AssignmentKey on every dual-emit site. The Attr is shared across all
// concurrent readers of the same Assignment pointer; treat it as immutable.
var LegacyFunctionKey = key.NewCached("function", (*Assignment).LogValue)

// AssignmentKey is the canonical "assignment"-vocabulary key for telemetry,
// headers, and Kubernetes labels. The Attr is shared across all concurrent
// readers of the same Assignment pointer; treat it as immutable.
var AssignmentKey = key.NewCached("assignment", (*Assignment).LogValue)

func (a *Assignment) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(a.GetNamespace()),
		key.Deployment.Slog(a.GetDeployment()),
		key.Tenant.Slog(a.GetTenant()),
		key.Metadata.Slog(a.GetMetadata()),
		key.Oneshot.Slog(a.GetOneshot()),
		slog.Attr{Key: ScaleKey.Name, Value: a.resolvedScaleValue()},
		// Flat-form companions for the three target_* fields. Each pair
		// reports the same resolved value (flat-preferred-over-Scale) so
		// dashboards keying on either vocabulary observe identical series.
		// The cleanup plan drops the legacy `scale.target_*` emissions.
		key.ScaleTargetCPUMillicores.Slog(a.ScaleTargetCPUMillicores()),
		key.ScaleTargetMemoryMebibytes.Slog(a.ScaleTargetMemoryMebibytes()),
		key.ScaleTargetInFlightRequests.Slog(a.ScaleTargetInFlightRequests()),
	)
}

// resolvedScaleValue builds the slog group emitted under the `scale` key,
// reading each subfield through the resolvers so flat-preferred-over-Scale
// resolution applies before logging.
func (a *Assignment) resolvedScaleValue() slog.Value {
	return slog.GroupValue(
		key.MinInstances.Slog(a.ScaleMinInstances()),
		key.MaxInstances.Slog(a.ScaleMaxInstances()),
		key.TargetCPUUsageMilli.Slog(a.ScaleTargetCPUMillicores()),
		key.TargetMemoryUsageMiB.Slog(a.ScaleTargetMemoryMebibytes()),
		key.TargetInFlightRequests.Slog(a.ScaleTargetInFlightRequests()),
	)
}

func (a *Assignment) Validate() error {
	if a.GetNamespace() == "" {
		return errors.New("missing namespace")
	}
	if a.GetDeployment() == "" {
		return errors.New("missing deployment")
	}
	if a.GetTenant() == "" {
		return errors.New("missing tenant")
	}

	// Resolve min/max across flat scale_* fields and the nested Scale sub-message
	// so a hybrid tenant header (e.g. scale.min_instances + scale_max_instances)
	// is validated against the same value the runtime resolvers see.
	maxInst, hasMax := a.resolvedScaleMax()
	if !hasMax {
		return errors.New("missing scale")
	}
	if maxInst < 1 {
		return errors.New("scale.max_instances must be >= 1")
	}
	if minInst, hasMin := a.resolvedScaleMin(); hasMin && minInst > maxInst {
		return fmt.Errorf("scale.min_instances (%d) must be <= scale.max_instances (%d)", minInst, maxInst)
	}

	if a.HasScaleTolerance() && a.GetScaleTolerance() < 0 {
		return fmt.Errorf("scale_tolerance (%v) must be >= 0", a.GetScaleTolerance())
	}

	// Durations must be non-negative. proto3 admits negative durations because
	// google.protobuf.Duration is a (seconds, nanos) pair; reject them at parse.
	for _, d := range nonNegativeDurations(a) {
		if d.has && d.value < 0 {
			return fmt.Errorf("%s (%s) must be >= 0", d.name, d.value)
		}
	}

	// Degenerate-zero rejection: a value of zero would deadlock the decision
	// site (heartbeat scale-to-zero with zero timeout, retry loop with zero
	// attempts, etc.).
	if a.HasHeartbeatTimeout() && a.GetHeartbeatTimeout().AsDuration() == 0 {
		return errors.New("heartbeat_timeout must be > 0")
	}
	if a.HasAssignTimeout() && a.GetAssignTimeout().AsDuration() == 0 {
		return errors.New("assign_timeout must be > 0")
	}
	if a.HasAssignTokenTtl() && a.GetAssignTokenTtl().AsDuration() == 0 {
		return errors.New("assign_token_ttl must be > 0")
	}
	if a.HasRetryMaxAttempts() && a.GetRetryMaxAttempts() == 0 {
		return errors.New("retry_max_attempts must be > 0")
	}

	if a.HasRetryMinBackoff() && a.HasRetryMaxBackoff() {
		minBackoff := a.GetRetryMinBackoff().AsDuration()
		maxBackoff := a.GetRetryMaxBackoff().AsDuration()
		if minBackoff > maxBackoff {
			return fmt.Errorf("retry_min_backoff (%s) must be <= retry_max_backoff (%s)", minBackoff, maxBackoff)
		}
	}

	return nil
}

type namedDuration struct {
	name  string
	has   bool
	value time.Duration
}

// nonNegativeDurations returns every duration-typed policy field with its
// presence bit and resolved value, so Validate can scan for negative values
// in one place. Fields list mirrors the duration knobs declared on Assignment.
func nonNegativeDurations(a *Assignment) []namedDuration {
	return []namedDuration{
		{"scale_downscale_stabilization", a.HasScaleDownscaleStabilization(), a.GetScaleDownscaleStabilization().AsDuration()},
		{"scale_initial_readiness_delay", a.HasScaleInitialReadinessDelay(), a.GetScaleInitialReadinessDelay().AsDuration()},
		{"assign_timeout", a.HasAssignTimeout(), a.GetAssignTimeout().AsDuration()},
		{"assign_token_ttl", a.HasAssignTokenTtl(), a.GetAssignTokenTtl().AsDuration()},
		{"heartbeat_interval", a.HasHeartbeatInterval(), a.GetHeartbeatInterval().AsDuration()},
		{"heartbeat_timeout", a.HasHeartbeatTimeout(), a.GetHeartbeatTimeout().AsDuration()},
		{"retry_min_backoff", a.HasRetryMinBackoff(), a.GetRetryMinBackoff().AsDuration()},
		{"retry_max_backoff", a.HasRetryMaxBackoff(), a.GetRetryMaxBackoff().AsDuration()},
		{"transport_dial_timeout", a.HasTransportDialTimeout(), a.GetTransportDialTimeout().AsDuration()},
		{"transport_keepalive", a.HasTransportKeepalive(), a.GetTransportKeepalive().AsDuration()},
		{"transport_idle_conn_timeout", a.HasTransportIdleConnTimeout(), a.GetTransportIdleConnTimeout().AsDuration()},
		{"transport_tls_handshake_timeout", a.HasTransportTlsHandshakeTimeout(), a.GetTransportTlsHandshakeTimeout().AsDuration()},
		{"transport_flush_interval", a.HasTransportFlushInterval(), a.GetTransportFlushInterval().AsDuration()},
	}
}

// SetHeader serializes the assignment as JSON and writes it under both the
// legacy ("X-Skipper-Function") and canonical ("X-Skipper-Assignment") header
// names. Receivers that read either name accept the body.
func (a *Assignment) SetHeader(r *http.Request) {
	body, err := json.Marshal(a)
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("failed to marshal assignment: %w", err))
	}
	r.Header[LegacyFunctionKey.Header] = []string{string(body)}
	r.Header[AssignmentKey.Header] = []string{string(body)}
}

// AssignmentFromHeader parses the assignment identity from the request header.
// It accepts either "X-Skipper-Assignment" (canonical) or "X-Skipper-Function"
// (legacy); when both are present the canonical header wins.
//
// The returned *Assignment is shared across all callers that present the same
// header value — it is cached to avoid redundant JSON unmarshalling. Callers
// MUST treat the returned pointer as immutable; mutating any field would
// silently corrupt the cache entry and affect every concurrent request that
// shares that pointer.
func AssignmentFromHeader(req *http.Request) (*Assignment, error) {
	source := AssignmentKey.Header
	header, ok := req.Header[source]
	if !ok || len(header) == 0 {
		source = LegacyFunctionKey.Header
		header, ok = req.Header[source]
		if !ok || len(header) == 0 {
			return nil, errors.New("missing " + LegacyFunctionKey.Header)
		}
	}

	headerVal := header[0]
	if a, ok := assignmentHeaderCache.Get(headerVal); ok {
		return a, nil
	}

	a := &Assignment{}
	if err := json.Unmarshal([]byte(headerVal), a); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s header: %w", source, err)
	}

	if err := a.Validate(); err != nil {
		return nil, err
	}

	assignmentHeaderCache.Add(headerVal, a)
	return a, nil
}
