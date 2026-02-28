package router

import (
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
)

func TestForwardedBufPool(t *testing.T) {
	t.Run("get returns usable buffer", func(t *testing.T) {
		buf := getForwardedBuf()
		assert.Assert(t, buf != nil)
		assert.Equal(t, buf.Len(), 0)
		assert.Assert(t, buf.Cap() >= forwardedBufCapacity)

		buf.WriteString("for=192.0.2.1;host=example.com;proto=https")
		assert.Assert(t, buf.Len() > 0)
		putForwardedBuf(buf)
	})

	t.Run("put get cycle returns reset buffer", func(t *testing.T) {
		buf := getForwardedBuf()
		buf.WriteString("for=192.0.2.1;host=example.com;proto=https")
		putForwardedBuf(buf)

		buf = getForwardedBuf()
		assert.Equal(t, buf.Len(), 0, "buffer should be reset after put/get cycle")
		putForwardedBuf(buf)
	})

	t.Run("discards oversized buffers", func(t *testing.T) {
		buf := getForwardedBuf()
		buf.Grow(maxForwardedBufCapacity + 1)
		putForwardedBuf(buf)

		// next get should return a fresh buffer with default capacity
		buf = getForwardedBuf()
		assert.Assert(t, buf.Cap() <= maxForwardedBufCapacity,
			"expected capacity <= %d, got %d", maxForwardedBufCapacity, buf.Cap())
		putForwardedBuf(buf)
	})
}

func TestForwardedBufPoolAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("skipping allocation test with race detector enabled")
	}

	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))

	// warm up the pool
	putForwardedBuf(getForwardedBuf())

	var memstats runtime.MemStats
	runtime.ReadMemStats(&memstats)
	heap := 0 - memstats.TotalAlloc

	// get and put 3 buffers
	putForwardedBuf(getForwardedBuf())
	putForwardedBuf(getForwardedBuf())
	putForwardedBuf(getForwardedBuf())

	runtime.ReadMemStats(&memstats)
	heap += memstats.TotalAlloc

	// sync.Pool with *bytes.Buffer should produce zero heap allocations
	// on the get/put path (the pointer doesn't need wrapping)
	assert.Equal(t, heap, uint64(0))
}

func BenchmarkForwardedBufPool(b *testing.B) {
	b.Run("sequential", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			buf := getForwardedBuf()
			putForwardedBuf(buf)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := getForwardedBuf()
				putForwardedBuf(buf)
			}
		})
	})
}
