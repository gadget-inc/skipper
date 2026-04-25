package key

import (
	"runtime"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/poll"
)

func TestMemoizedCacheShrinksAfterGC(t *testing.T) {
	// Insert entries for N distinct pointers, drop references, GC. Entries
	// must be released -- otherwise the cache leaks one per pointer parsed.
	k := New("memo-gc", (*testLogValuer).LogValue)
	c := &memoizedCache[testLogValuer]{build: k.Attr}

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
	k := NewCached("memo-bench", (*testLogValuer).LogValue)
	v := &testLogValuer{name: "bench"}
	_ = k.Attr(v) // prime
	b.ReportAllocs()
	for b.Loop() {
		sinkAttr = k.Attr(v)
	}
}

var sinkAttr Attr
