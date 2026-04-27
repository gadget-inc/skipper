package skipper

import (
	"github.com/gadget-inc/skipper/internal/key"
)

var functionAttr = key.Memoized(func(f *Function) key.Attr {
	return key.Function.Attr(f)
})

// Attr returns the telemetry Attr for this function, memoized per pointer.
// The cached Attr shrinks automatically once *Function is GC'd.
//
// Callers MUST treat the returned Attr as immutable; it is shared across all
// concurrent readers of the same Function pointer.
func (f *Function) Attr() key.Attr {
	return functionAttr(f)
}
