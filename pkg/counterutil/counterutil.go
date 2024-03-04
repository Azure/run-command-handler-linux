package counterutil

import "sync/atomic"

type AtomicCount int32

func (c *AtomicCount) Increment() int32 {
	return atomic.AddInt32((*int32)(c), 1)
}

func (c *AtomicCount) Decrement() int32 {
	return atomic.AddInt32((*int32)(c), -1)
}

func (c *AtomicCount) Get() int32 {
	return atomic.LoadInt32((*int32)(c))
}
