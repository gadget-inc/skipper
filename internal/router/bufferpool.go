package router

import "sync"

const (
	bufCapacity    = 32 * 1024   // 32KB
	maxBufCapacity = 1024 * 1024 // 1MB
)

type bufPool struct {
	sync.Pool
}

var bufferPool = &bufPool{
	sync.Pool{
		New: func() any {
			return make([]byte, 0, bufCapacity)
		},
	},
}

func (bp *bufPool) Get() []byte {
	return bp.Pool.Get().([]byte)
}

func (bp *bufPool) Put(buf []byte) {
	if cap(buf) > maxBufCapacity {
		// don't put large buffers back in the pool
		return
	}
	buf = buf[:0] // reset the buffer
	bp.Pool.Put(buf)
}
