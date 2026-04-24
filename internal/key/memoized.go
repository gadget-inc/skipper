package key

import (
	"runtime"
	"sync"
	"unsafe"
	"weak"
)

// Memoized wraps build in a weak per-pointer cache. The returned closure
// returns the same Attr for the same *T across calls, paying the build cost
// once per pointer. Entries are released automatically when *T becomes
// unreachable, so the cache's resident set tracks the live *T population.
//
// The Attr cache is safe precisely because Key.Attr pre-resolves its
// slog.Value -- the cached Attr never pins the source *T. Build closures
// that construct an Attr by other means MUST preserve the same property
// (no LogValuer or any-boxed *T in the returned slog.Attr), otherwise the
// weak ref and cleanup cannot free *T.
//
// Uses the canonical weak-cache idiom from
// https://go.dev/blog/cleanups-and-weak.
func Memoized[T any](build func(*T) Attr) func(*T) Attr {
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
		if prior := actual.(*memoizedEntry[T]); prior.weak.Value() == v {
			return *prior.attr
		}
		// Defensive: a concurrent inserter's weak ref resolves to something
		// other than v. This shouldn't be reachable while v is alive at k,
		// but overwrite rather than return a mismatched Attr.
		m.cache.Store(k, entry)
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
