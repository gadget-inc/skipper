package skipper

import (
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/puzpuzpuz/xsync/v4"
)

// functionAttrCache memoizes the derived key.Attr for each *Function pointer
// seen by the hot telemetry path so Supervisor.converge does not rebuild the
// same slog/OTel attribute tuple on every 15s tick.
//
// Pointer-keyed lookup works because Function pointers are already
// deduplicated upstream (functionHeaderCache, Controller.functionCache): one
// distinct pointer per parsed Function header/annotation.
//
// The cache grows monotonically over the process lifetime -- entries for
// Functions evicted from upstream caches are not removed. In practice the
// distinct-Function population is bounded (thousands, not millions) and the
// controller restarts on every deploy, so the resident set stays small.
//
// Implemented as a global xsync.Map rather than an atomic.Pointer field on
// *Function because the proto opaque struct cannot be extended outside of
// types.pb.go. Callers see the same Attr() contract either way.
var functionAttrCache = xsync.NewMap[*Function, *key.Attr]()

// Attr returns the telemetry Attr for this function, memoized per pointer so
// repeated converge ticks pay the build cost once per *Function.
//
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
