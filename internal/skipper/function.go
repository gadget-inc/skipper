package skipper

import (
	"container/list"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/go-json-experiment/json"
)

// functionHeaderCacheMaxSize is the maximum number of distinct header values
// retained in the parse cache. Each entry holds one parsed *Function. The
// router sees at most one distinct header per active function × scale variant,
// so 4096 is generous for any realistic fleet size.
const functionHeaderCacheMaxSize = 4096

// lruCache is a simple, mutex-protected LRU cache.
type lruCache[K comparable, V any] struct {
	mu    sync.Mutex
	cap   int
	items map[K]*list.Element
	order *list.List // front = most recently used
}

type lruEntry[K comparable, V any] struct {
	key K
	val V
}

func newLRUCache[K comparable, V any](capacity int) *lruCache[K, V] {
	return &lruCache[K, V]{
		cap:   capacity,
		items: make(map[K]*list.Element, capacity),
		order: list.New(),
	}
}

// load returns the cached value and true, or the zero value and false.
func (c *lruCache[K, V]) load(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*lruEntry[K, V]).val, true
	}
	var zero V
	return zero, false
}

// store adds or updates a key, evicting the least recently used entry if the
// cache is at capacity.
func (c *lruCache[K, V]) store(key K, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		el.Value.(*lruEntry[K, V]).val = val
		return
	}
	if len(c.items) >= c.cap {
		// Evict the least recently used entry (back of the list).
		lru := c.order.Back()
		if lru != nil {
			c.order.Remove(lru)
			delete(c.items, lru.Value.(*lruEntry[K, V]).key)
		}
	}
	el := c.order.PushFront(&lruEntry[K, V]{key: key, val: val})
	c.items[key] = el
}

var functionHeaderCache = newLRUCache[string, *Function](functionHeaderCacheMaxSize)

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

func (f *Function) LogValue() slog.Value {
	return slog.GroupValue(
		key.Namespace.Slog(f.GetNamespace()),
		key.Deployment.Slog(f.GetDeployment()),
		key.Tenant.Slog(f.GetTenant()),
		key.Metadata.Slog(f.GetMetadata()),
		key.Oneshot.Slog(f.GetOneshot()),
		key.Scale.Slog(f.GetScale()),
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
	if f.GetScale() == nil {
		return errors.New("missing scale")
	}
	return nil
}

func (f *Function) SetHeader(r *http.Request) {
	fnJSON, err := json.Marshal(f)
	if err != nil {
		// this should never happen
		panic(fmt.Errorf("failed to marshal function: %w", err))
	}
	r.Header[key.Function.Header] = []string{string(fnJSON)}
}

func FunctionFromHeader(req *http.Request) (*Function, error) {
	header, ok := req.Header[key.Function.Header]
	if !ok || len(header) == 0 {
		return nil, errors.New("missing " + key.Function.Header)
	}

	headerVal := header[0]
	if fn, ok := functionHeaderCache.load(headerVal); ok {
		return fn, nil
	}

	fn := &Function{}
	if err := json.Unmarshal([]byte(headerVal), fn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s header: %w", key.Function.Header, err)
	}

	if err := fn.Validate(); err != nil {
		return nil, err
	}

	functionHeaderCache.store(headerVal, fn)
	return fn, nil
}
