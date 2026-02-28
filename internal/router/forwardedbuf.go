package router

import (
	"bytes"
	"sync"
)

const (
	forwardedBufCapacity    = 256      // typical Forwarded header ~100-200 bytes
	maxForwardedBufCapacity = 4 * 1024 // 4KB max
)

var forwardedBufPool = &sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, forwardedBufCapacity))
	},
}

func getForwardedBuf() *bytes.Buffer {
	return forwardedBufPool.Get().(*bytes.Buffer)
}

func putForwardedBuf(buf *bytes.Buffer) {
	if buf.Cap() > maxForwardedBufCapacity {
		return // don't put oversized buffers back
	}
	buf.Reset()
	forwardedBufPool.Put(buf)
}
