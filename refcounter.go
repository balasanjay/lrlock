package lrlock

import (
	"math"
	"sync"
	"sync/atomic"
)

// refCount is a distributed reference counter. It supports multiple goroutines
// acquiring and releasing a reference, but it only supports a single goroutine
// waiting for the count to hit zero.
type refCount struct {
	// A mangled form of GOMAXPROCS when the instance was created.
	// Specifically, we round up GOMAXPROCS+1 to the nearest power-of-two,
	// and store the  number of bits needed to represent that here.
	pbits uint

	// An array of counters. The effective size is GOMAXPROCS()*CacheLineSize().
	// Normally, the list needs empty space to prevent false sharing; in an
	// attempt to conserve memory, multiple instances of refCount can share
	// the same counters array, as long as they use distinct values of 'offset'.
	counters []int32
	offset   int

	// A channel with buffer size of 1. Its used by the last release-er to
	// wake up the waiter, if the waiter has finished scanning the distributed
	// counters and is now sleeping.
	waitch chan struct{}
}

func (r *refCount) acquire() int {
	idx := r.idxForP(getp() + 1)
	if atomic.AddInt32(&r.counters[idx], 1) == math.MaxInt32 {
		panic("refCount does not support more than 2 billion acquires.")
	}
	return idx
}

func (r *refCount) release(idx int) {
	if atomic.AddInt32(&r.counters[idx], -1) >= 0 {
		return
	}

	if atomic.AddInt32(&r.counters[r.idxForP(0)], -1) != 0 {
		return
	}

	r.waitch <- struct{}{}
}

func (r *refCount) wait() {
	pendingSlots := int32(0)
	for i := 1; i <= r.maxPid(); i++ {
		if atomic.AddInt32(&r.counters[r.idxForP(i)], -1) >= 0 {
			pendingSlots++
		}
	}

	// If there were pending slots, we need to wait for more calls to release().
	if pendingSlots > 0 {
		if atomic.AddInt32(&r.counters[r.idxForP(0)], pendingSlots) > 0 {
			// Wait for the last release-er.
			<-r.waitch
		}
	}
}

func (r *refCount) idxForP(p int) int {
	// Mask the pid down into range, in case GOMAXPROCS has
	// increased since this was allocated.
	pid := p & r.maxPid()
	return (pid << slotsPerCacheLineBits()) + r.offset
}

func (r *refCount) maxPid() int {
	return 1<<r.pbits - 1
}

// clear resets a refCount for reuse. It should not be called concurrent with any other operation.
func (r *refCount) clear() {
	// TODO: ask dvyukov if these can be non-atomic operations. Technically, nothing else should be
	// affecting these slots, yet there are probably other refCounts writing atomically to the
	// same cache line.
	atomic.StoreInt32(&r.counters[r.idxForP(0)], 0)
	for i := 1; i <= r.maxPid(); i++ {
		atomic.StoreInt32(&r.counters[r.idxForP(i)], 0)
	}
}

var refCountPool = &sync.Pool{}

func allocateRefCount(reuse *refCount) *refCount {
	pbits := uint(roundNearestPowerOf2(gomaxprocs() + 1))

	// If we can reuse the given refCount instance, then do that.
	if reuse != nil && reuse.pbits == pbits {
		reuse.clear()
		return reuse
	}

	// Try reusing one from the pool.
	pooledRc, _ := refCountPool.Get().(*refCount)
	if pooledRc != nil && pooledRc.pbits == pbits {
		// If this pool entry has more juice in it, put it back.
		if pooledRc.offset < ((1 << slotsPerCacheLineBits()) - 1) {
			nextRc := *pooledRc
			nextRc.offset++
			refCountPool.Put(&nextRc)
		}

		pooledRc.waitch = make(chan struct{}, 1)
		return pooledRc
	}

	newRc := refCount{
		pbits:    pbits,
		counters: make([]int32, (1<<pbits)<<slotsPerCacheLineBits()),
	}

	// Push the remaining slots into the pool
	nextRc := newRc
	nextRc.offset++
	refCountPool.Put(&nextRc)

	newRc.waitch = make(chan struct{}, 1)
	return &newRc
}
