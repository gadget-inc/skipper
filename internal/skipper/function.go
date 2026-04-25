package skipper

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

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

// FunctionKey is the typed telemetry key for a Function. Attr is per-pointer
// memoized via a weak cache so the converge hot path stays allocation-free
// once a Function has been seen at least once.
//
// Callers MUST treat the returned Attr as immutable; it is shared across all
// concurrent readers of the same Function pointer.
var FunctionKey = key.NewCached("function", (*Function).LogValue)

func (f *Function) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(f.GetNamespace()),
		key.Deployment.Slog(f.GetDeployment()),
		key.Tenant.Slog(f.GetTenant()),
		key.Metadata.Slog(f.GetMetadata()),
		key.Oneshot.Slog(f.GetOneshot()),
		ScaleKey.Slog(f.GetScale()),
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
	return nil
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
