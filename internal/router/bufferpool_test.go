package router

import (
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
)

func TestPoolAllocations(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))

	// warm up the pool
	bufferPool.Put(bufferPool.Get())

	// measure the starting statistics
	var memstats runtime.MemStats
	runtime.ReadMemStats(&memstats)
	heap := 0 - memstats.TotalAlloc

	// get and put 3 buffers
	bufferPool.Put(bufferPool.Get())
	bufferPool.Put(bufferPool.Get())
	bufferPool.Put(bufferPool.Get())

	// read the final statistics
	runtime.ReadMemStats(&memstats)
	heap += memstats.TotalAlloc

	// the pool returns []byte, not *[]byte, so we need to account for
	// the allocation of the slice header (24 bytes) across the 3 calls.
	assert.Assert(t, heap == 72)
}

func TestPoolLargeBuffer(t *testing.T) {
	// create and discard a large buffer
	bufferPool.Put(make([]byte, maxBufCapacity+1))

	// verify the buffer was discarded by checking that we get a new buffer
	// with the default size when we Get from the pool
	assert.Assert(t, bufCapacity == cap(bufferPool.Get()))
}
