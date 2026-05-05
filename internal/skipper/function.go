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

// functionHeaderCacheMaxSize is the maximum number of distinct header values
// retained in the parse cache. Each entry holds one parsed *Function. The
// router sees at most one distinct header per active function × scale variant,
// so 4096 is generous for any realistic fleet size.
const functionHeaderCacheMaxSize = 4096

// functionHeaderCache is a bounded LRU cache of parsed Function pointers,
// keyed by the raw header string to avoid redundant JSON unmarshalling.
type functionHeaderCacheType = lru.Cache[string, *Function]

func newFunctionHeaderCache(capacity int) *functionHeaderCacheType {
	c, _ := lru.New[string, *Function](capacity)
	return c
}

var functionHeaderCache = newFunctionHeaderCache(functionHeaderCacheMaxSize)

// FunctionHash is a unique identifier for a Function, suitable for use as a map key.
type FunctionHash = uint64

// Hash returns a hash of the function's identity fields (namespace, deployment,
// tenant, oneshot), suitable for use as a map key. Metadata and scale fields are
// excluded so that changes to those fields don't create a new function identity.
func (f *Function) Hash() FunctionHash {
	h := xxhash.New()
	_, _ = h.WriteString(f.GetNamespace())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.GetDeployment())
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(f.GetTenant())
	_, _ = h.Write([]byte{0})
	if f.GetOneshot() {
		_, _ = h.Write([]byte{1})
	} else {
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

var _ slog.LogValuer = (*Function)(nil)

// FunctionKey's Attr is shared across all concurrent readers of the same
// Function pointer; treat the returned Attr as immutable.
var FunctionKey = key.NewCached("function", (*Function).LogValue)

func (f *Function) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(f.GetNamespace()),
		key.Deployment.Slog(f.GetDeployment()),
		key.Tenant.Slog(f.GetTenant()),
		key.Metadata.Slog(f.GetMetadata()),
		key.Oneshot.Slog(f.GetOneshot()),
		ScaleKey.Slog(f.GetScale()),
		HpaPolicyKey.Slog(f.GetHpa()),
		HeartbeatPolicyKey.Slog(f.GetHeartbeat()),
		ProxyPolicyKey.Slog(f.GetProxy()),
		LifecyclePolicyKey.Slog(f.GetLifecycle()),
	)
}

func (f *Function) Validate() error {
	if f.GetNamespace() == "" {
		return errors.New("missing namespace")
	}
	if f.GetDeployment() == "" {
		return errors.New("missing deployment")
	}
	if f.GetTenant() == "" {
		return errors.New("missing tenant")
	}
	scale := f.GetScale()
	if scale == nil {
		return errors.New("missing scale")
	}
	if scale.GetMaxInstances() < 1 {
		return errors.New("scale.max_instances must be >= 1")
	}
	if scale.GetMinInstances() > scale.GetMaxInstances() {
		return fmt.Errorf("scale.min_instances (%d) must be <= scale.max_instances (%d)", scale.GetMinInstances(), scale.GetMaxInstances())
	}
	if hpa := f.GetHpa(); hpa != nil {
		// Tolerance gates the HPA dead-band via `usageDiscrepancy <= tolerance`,
		// where usageDiscrepancy is non-negative (math.Abs). A negative tolerance
		// would silently disable the dead-band and trigger continuous scaling.
		if t := hpa.GetTolerance(); t < 0 {
			return fmt.Errorf("hpa.tolerance (%g) must be >= 0", t)
		}
		if d := hpa.GetDownscaleStabilization().AsDuration(); d < 0 {
			return fmt.Errorf("hpa.downscale_stabilization (%s) must be >= 0", d)
		}
		if d := hpa.GetInitialReadinessDelay().AsDuration(); d < 0 {
			return fmt.Errorf("hpa.initial_readiness_delay (%s) must be >= 0", d)
		}
	}
	if hb := f.GetHeartbeat(); hb != nil && hb.HasTimeout() {
		// Zero would scale the function to zero on the first converge tick;
		// honour the cluster default by leaving the field unset.
		if d := hb.GetTimeout().AsDuration(); d <= 0 {
			return fmt.Errorf("heartbeat.timeout (%s) must be > 0 when set", d)
		}
	}
	if proxy := f.GetProxy(); proxy != nil {
		minBackoff := proxy.GetRetryMinBackoff().AsDuration()
		maxBackoff := proxy.GetRetryMaxBackoff().AsDuration()
		if minBackoff < 0 {
			return fmt.Errorf("proxy.retry_min_backoff (%s) must be >= 0", minBackoff)
		}
		if maxBackoff < 0 {
			return fmt.Errorf("proxy.retry_max_backoff (%s) must be >= 0", maxBackoff)
		}
		if minBackoff > 0 && maxBackoff > 0 && minBackoff > maxBackoff {
			return fmt.Errorf("proxy.retry_min_backoff (%s) must be <= proxy.retry_max_backoff (%s)", minBackoff, maxBackoff)
		}
		// Zero attempts fails every request before the first proxy try.
		if proxy.HasMaxAttempts() && proxy.GetMaxAttempts() == 0 {
			return fmt.Errorf("proxy.max_attempts (0) must be > 0 when set")
		}
	}
	if lc := f.GetLifecycle(); lc != nil {
		if lc.HasAssignTimeout() {
			// Zero immediately cancels the assign context; every assignment
			// fails. Honour the cluster default by leaving the field unset.
			if d := lc.GetAssignTimeout().AsDuration(); d <= 0 {
				return fmt.Errorf("lifecycle.assign_timeout (%s) must be > 0 when set", d)
			}
		}
		if lc.HasTokenTtl() {
			// Zero issues an already-expired token; every backend rejects.
			if d := lc.GetTokenTtl().AsDuration(); d <= 0 {
				return fmt.Errorf("lifecycle.token_ttl (%s) must be > 0 when set", d)
			}
		}
	}
	return nil
}

// HPATolerance returns the per-function HPA tolerance, falling back to
// clusterDefault when the function does not set one. Presence is checked via
// HasTolerance so a tenant who explicitly sets zero (strict mode) is honored
// rather than silently overridden by the cluster default.
func (f *Function) HPATolerance(clusterDefault float64) float64 {
	if hpa := f.GetHpa(); hpa.HasTolerance() {
		return hpa.GetTolerance()
	}
	return clusterDefault
}

// HPADownscaleStabilization returns the per-function downscale-stabilization
// window, falling back to clusterDefault when the function does not set one.
func (f *Function) HPADownscaleStabilization(clusterDefault time.Duration) time.Duration {
	if hpa := f.GetHpa(); hpa.HasDownscaleStabilization() {
		return hpa.GetDownscaleStabilization().AsDuration()
	}
	return clusterDefault
}

// HPAInitialReadinessDelay returns the per-function initial-readiness delay,
// falling back to clusterDefault when the function does not set one.
func (f *Function) HPAInitialReadinessDelay(clusterDefault time.Duration) time.Duration {
	if hpa := f.GetHpa(); hpa.HasInitialReadinessDelay() {
		return hpa.GetInitialReadinessDelay().AsDuration()
	}
	return clusterDefault
}

// HeartbeatTimeout returns the per-function heartbeat timeout, falling back to
// clusterDefault when the function does not set one.
func (f *Function) HeartbeatTimeout(clusterDefault time.Duration) time.Duration {
	if hb := f.GetHeartbeat(); hb.HasTimeout() {
		return hb.GetTimeout().AsDuration()
	}
	return clusterDefault
}

// MaxRoundTripAttempts returns the per-function maximum number of retry
// attempts, falling back to clusterDefault when the function does not set one.
func (f *Function) MaxRoundTripAttempts(clusterDefault uint32) uint32 {
	if proxy := f.GetProxy(); proxy.HasMaxAttempts() {
		return proxy.GetMaxAttempts()
	}
	return clusterDefault
}

// RetryMinBackoff returns the per-function minimum retry backoff, falling
// back to clusterDefault when the function does not set one.
func (f *Function) RetryMinBackoff(clusterDefault time.Duration) time.Duration {
	if proxy := f.GetProxy(); proxy.HasRetryMinBackoff() {
		return proxy.GetRetryMinBackoff().AsDuration()
	}
	return clusterDefault
}

// RetryMaxBackoff returns the per-function maximum retry backoff, falling
// back to clusterDefault when the function does not set one.
func (f *Function) RetryMaxBackoff(clusterDefault time.Duration) time.Duration {
	if proxy := f.GetProxy(); proxy.HasRetryMaxBackoff() {
		return proxy.GetRetryMaxBackoff().AsDuration()
	}
	return clusterDefault
}

// AssignTimeout returns the per-function instance-assignment timeout, falling
// back to clusterDefault when the function does not set one.
func (f *Function) AssignTimeout(clusterDefault time.Duration) time.Duration {
	if lc := f.GetLifecycle(); lc.HasAssignTimeout() {
		return lc.GetAssignTimeout().AsDuration()
	}
	return clusterDefault
}

// TokenTTL returns the per-function PASETO token lifetime, falling back to
// clusterDefault when the function does not set one.
func (f *Function) TokenTTL(clusterDefault time.Duration) time.Duration {
	if lc := f.GetLifecycle(); lc.HasTokenTtl() {
		return lc.GetTokenTtl().AsDuration()
	}
	return clusterDefault
}

func (f *Function) SetHeader(r *http.Request) {
	fnJSON, err := json.Marshal(f)
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("failed to marshal function: %w", err))
	}
	r.Header[FunctionKey.Header] = []string{string(fnJSON)}
}

// FunctionFromHeader parses the function identity from the request header.
// The returned *Function is shared across all callers that present the same
// header value — it is cached to avoid redundant JSON unmarshalling. Callers
// MUST treat the returned pointer as immutable; mutating any field would
// silently corrupt the cache entry and affect every concurrent request that
// shares that pointer.
func FunctionFromHeader(req *http.Request) (*Function, error) {
	header, ok := req.Header[FunctionKey.Header]
	if !ok || len(header) == 0 {
		return nil, errors.New("missing " + FunctionKey.Header)
	}

	headerVal := header[0]
	if fn, ok := functionHeaderCache.Get(headerVal); ok {
		return fn, nil
	}

	fn := &Function{}
	if err := json.Unmarshal([]byte(headerVal), fn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s header: %w", FunctionKey.Header, err)
	}

	if err := fn.Validate(); err != nil {
		return nil, err
	}

	functionHeaderCache.Add(headerVal, fn)
	return fn, nil
}
