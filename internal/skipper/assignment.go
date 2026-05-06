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

// LegacyFunctionKey emits the externally-visible "function" vocabulary on
// telemetry, headers, and Kubernetes labels. Phase 1 keeps every existing
// emission site pointed at this key; Phase 2 introduces dual-emit alongside
// AssignmentKey.
//
// The Attr is shared across all concurrent readers of the same Assignment
// pointer; treat the returned Attr as immutable.
var LegacyFunctionKey = key.NewCached("function", (*Assignment).LogValue)

// AssignmentKey emits the canonical "assignment" vocabulary. Phase 1
// declares it but does not route any existing emission site through it
// (except K8s annotation dual-write, which writes both this key's
// PatchAnnotation and LegacyFunctionKey's). Phase 2 wires header dual-parse
// and telemetry dual-emit via this key.
var AssignmentKey = key.NewCached("assignment", (*Assignment).LogValue)

func (a *Assignment) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(a.GetNamespace()),
		key.Deployment.Slog(a.GetDeployment()),
		key.Tenant.Slog(a.GetTenant()),
		key.Metadata.Slog(a.GetMetadata()),
		key.Oneshot.Slog(a.GetOneshot()),
		ScaleKey.Slog(a.GetScale()),
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
	scale := a.GetScale()
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

func (a *Assignment) SetHeader(r *http.Request) {
	body, err := json.Marshal(a)
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("failed to marshal assignment: %w", err))
	}
	r.Header[LegacyFunctionKey.Header] = []string{string(body)}
}

// AssignmentFromHeader parses the assignment identity from the request header.
// The returned *Assignment is shared across all callers that present the same
// header value — it is cached to avoid redundant JSON unmarshalling. Callers
// MUST treat the returned pointer as immutable; mutating any field would
// silently corrupt the cache entry and affect every concurrent request that
// shares that pointer.
func AssignmentFromHeader(req *http.Request) (*Assignment, error) {
	header, ok := req.Header[LegacyFunctionKey.Header]
	if !ok || len(header) == 0 {
		return nil, errors.New("missing " + LegacyFunctionKey.Header)
	}

	headerVal := header[0]
	if a, ok := assignmentHeaderCache.Get(headerVal); ok {
		return a, nil
	}

	a := &Assignment{}
	if err := json.Unmarshal([]byte(headerVal), a); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s header: %w", LegacyFunctionKey.Header, err)
	}

	if err := a.Validate(); err != nil {
		return nil, err
	}

	assignmentHeaderCache.Add(headerVal, a)
	return a, nil
}
