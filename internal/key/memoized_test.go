package key

import (
	"log/slog"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/poll"
)

func TestMemoizedReturnsEquivalentAttr(t *testing.T) {
	t.Parallel()
	k := New("memo-test", func(v *testLogValuer) slog.Value { return v.LogValue() })
	attr := memoized(func(v *testLogValuer) Attr { return k.Attr(v) })

	v := &testLogValuer{name: "hello"}
	got1 := attr(v)
	got2 := attr(v)
	want := k.Attr(v)

	assert.Assert(t, got1.Slog.Equal(want.Slog))
	assert.Assert(t, got1.Slog.Equal(got2.Slog), "second call should return equivalent Attr")
}

func TestMemoizedSkipsBuildOnCacheHit(t *testing.T) {
	t.Parallel()
	var builds atomic.Int64
	k := New("memo-count", func(v *testLogValuer) slog.Value { return v.LogValue() })
	attr := memoized(func(v *testLogValuer) Attr {
		builds.Add(1)
		return k.Attr(v)
	})

	v := &testLogValuer{name: "hit"}
	for range 10 {
		_ = attr(v)
	}
	assert.Equal(t, builds.Load(), int64(1), "builder should fire once per pointer")
}

func TestMemoizedCacheShrinksAfterGC(t *testing.T) {
	// Insert entries for N distinct pointers, drop references, GC. Entries
	// must be released -- otherwise the cache leaks one per pointer parsed.
	k := New("memo-gc", func(v *testLogValuer) slog.Value { return v.LogValue() })
	c := &memoizedCache[testLogValuer]{build: func(v *testLogValuer) Attr { return k.Attr(v) }}

	const n = 100
	for range n {
		v := &testLogValuer{name: "gc"}
		_ = c.attr(v)
	}
	assert.Equal(t, c.size(), n, "cache should have one entry per distinct pointer")

	poll.WaitOn(t, func(poll.LogT) poll.Result {
		runtime.GC()
		if size := c.size(); size == 0 {
			return poll.Success()
		} else {
			return poll.Continue("cache size %d", size)
		}
	}, poll.WithDelay(10*time.Millisecond), poll.WithTimeout(2*time.Second))
}

func BenchmarkMemoizedHit(b *testing.B) {
	k := New("memo-bench", func(v *testLogValuer) slog.Value { return v.LogValue() })
	attr := memoized(func(v *testLogValuer) Attr { return k.Attr(v) })
	v := &testLogValuer{name: "bench"}
	_ = attr(v) // prime
	b.ReportAllocs()
	for b.Loop() {
		sinkAttr = attr(v)
	}
}

var sinkAttr Attr
