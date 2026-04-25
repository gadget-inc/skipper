package key

import (
	"runtime"
	"sync"
	"unsafe"
	"weak"
)

// memoized memoizes build per-pointer with a weak cache; entries are
// released once *T becomes unreachable.
//
// The public entry point is [NewCached] -- it constructs a typed Key and
// wires this cache automatically. Callers shouldn't reach for memoized
// directly; the contract on build (must not retain *T) lives inside
// (*Key[V]).buildAttr, which Key.Attr's eager Resolve() guarantees.
//
// Follows the canonical Go 1.24 weak-cache idiom:
// https://go.dev/blog/cleanups-and-weak.
func memoized[T any](build func(*T) Attr) func(*T) Attr {
	c := &memoizedCache[T]{build: build}
	return c.attr
}

type memoizedEntry[T any] struct {
	weak weak.Pointer[T]
	attr *Attr
}

type memoizedCache[T any] struct {
	build func(*T) Attr
	cache sync.Map
}

func (m *memoizedCache[T]) attr(v *T) Attr {
	k := uintptr(unsafe.Pointer(v))
	if e, ok := m.cache.Load(k); ok {
		entry := e.(*memoizedEntry[T])
		if entry.weak.Value() == v {
			return *entry.attr
		}
		m.cache.CompareAndDelete(k, e)
	}

	built := m.build(v)
	entry := &memoizedEntry[T]{weak: weak.Make(v), attr: &built}
	if actual, loaded := m.cache.LoadOrStore(k, entry); loaded {
		return *actual.(*memoizedEntry[T]).attr
	}
	runtime.AddCleanup(v, func(k uintptr) {
		m.cache.CompareAndDelete(k, entry)
	}, k)
	return built
}

func (m *memoizedCache[T]) size() int {
	n := 0
	m.cache.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}
