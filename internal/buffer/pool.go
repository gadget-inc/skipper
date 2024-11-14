package buffer

import "sync"

const (
	kib = 1024
)

type pool struct {
	inner sync.Pool
}

var Pool = &pool{
	inner: sync.Pool{
		New: func() any {
			return make([]byte, 0, 32*kib)
		},
	},
}

func (p *pool) Get() []byte {
	return p.inner.Get().([]byte)
}

func (p *pool) Put(buf []byte) {
	p.inner.Put(buf)
}
