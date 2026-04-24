package skipper

import (
	"log/slog"
	"runtime"
	"sync"
	"unsafe"
	"weak"

	"github.com/gadget-inc/skipper/internal/key"
)

// functionAttrEntry is the cached entry for a *Function. It holds a weak
// reference to the Function so the entry does not keep it alive; the strong
// pointers live solely with the Function's real owners (FunctionFromHeader's
// LRU, Controller.functionCache, in-flight requests).
type functionAttrEntry struct {
	fn   weak.Pointer[Function]
	attr *key.Attr
}

// functionAttrCache keys by pointer bits (uintptr) rather than *Function so
// the map itself retains no Functions. The stored weak.Pointer lets Attr()
// validate on lookup that the entry still corresponds to the Function the
// caller passed in (defence against address reuse after GC).
//
// Entries are removed via runtime.AddCleanup when a Function becomes
// unreachable, matching the canonical weak-cache idiom from
// https://go.dev/blog/cleanups-and-weak.
var functionAttrCache sync.Map

// Attr returns the telemetry Attr for this function, memoized per pointer.
// The cached Attr shrinks automatically once the *Function is GC'd.
//
// Callers MUST treat the returned Attr as immutable; it is shared across all
// concurrent readers of the same Function pointer.
func (f *Function) Attr() key.Attr {
	k := uintptr(unsafe.Pointer(f))
	if v, ok := functionAttrCache.Load(k); ok {
		entry := v.(*functionAttrEntry)
		if entry.fn.Value() == f {
			return *entry.attr
		}
		// Address reuse: the prior Function at this address was collected and
		// a new one was allocated at the same address before its cleanup
		// ran. Drop the stale entry and rebuild.
		functionAttrCache.CompareAndDelete(k, v)
	}

	built := key.Function.Attr(f)
	// Pre-resolve the LogValuer so the cached Slog does not hold a strong
	// reference to *Function via slog.Any -- otherwise weak.Pointer and
	// AddCleanup could never free the Function even though the map key and
	// entry are weak. Pre-resolution produces the same final log output as
	// lazy resolution for an immutable Function.
	built.Slog = slog.Attr{Key: built.Slog.Key, Value: built.Slog.Value.Resolve()}

	entry := &functionAttrEntry{fn: weak.Make(f), attr: &built}
	if actual, loaded := functionAttrCache.LoadOrStore(k, entry); loaded {
		if prior := actual.(*functionAttrEntry); prior.fn.Value() == f {
			return *prior.attr
		}
		// Defensive: a concurrent inserter's weak ref resolves to something
		// other than f. This shouldn't be reachable while f is alive at k,
		// but overwrite rather than return a mismatched Attr.
		functionAttrCache.Store(k, entry)
	}
	runtime.AddCleanup(f, func(k uintptr) {
		functionAttrCache.CompareAndDelete(k, entry)
	}, k)
	return built
}
