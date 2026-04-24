package skipper

import (
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/puzpuzpuz/xsync/v4"
)

// functionAttrCache is a global map rather than a field on *Function because
// the proto opaque struct cannot be extended outside types.pb.go. Pointer-key
// lookup is safe because Function pointers are already deduplicated upstream
// (functionHeaderCache, Controller.functionCache).
var functionAttrCache = xsync.NewMap[*Function, *key.Attr]()

// Attr returns the telemetry Attr for this function, memoized per pointer.
// Callers MUST treat the returned Attr as immutable; it is shared across all
// concurrent readers of the same Function pointer.
func (f *Function) Attr() key.Attr {
	if cached, ok := functionAttrCache.Load(f); ok {
		return *cached
	}
	built := key.Function.Attr(f)
	functionAttrCache.Store(f, &built)
	return built
}
