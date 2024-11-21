package buffer

import "sync"

const bufSize = 32 * 1024

type pool struct {
	sync.Pool
}

var Pool = &pool{
	sync.Pool{
		New: func() any {
			buf := make([]byte, 0, bufSize)
			return &buf
		},
	},
}

func (p *pool) Get() []byte {
	return *p.Pool.Get().(*[]byte)
}

func (p *pool) Put(buf []byte) {
	p.Pool.Put(&buf)
}
