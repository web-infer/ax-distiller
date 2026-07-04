package structure

import (
	"sync"
)

var histogramPool = &sync.Pool{
	New: func() any {
		return make(map[uint64]uint32)
	},
}

func getHistogram() map[uint64]uint32 {
	return histogramPool.Get().(map[uint64]uint32)
}

func putHistogram(h map[uint64]uint32) {
	clear(h)
	histogramPool.Put(h)
}

var byteslicePool = &sync.Pool{
	New: func() any {
		return make([]byte, 0)
	},
}

func getByteslice() []byte {
	return byteslicePool.Get().([]byte)
}

func putByteslice(b []byte) {
	b = b[:0]
	byteslicePool.Put(b)
}
