// Package lrlock implements a (slightly-exotic) cross-core scalable version of sync.RWMutex.
//
// See http://concurrencyfreaks.blogspot.com/2013/12/left-right-classical-algorithm.html.
//
// You should probably just use a sync.RWMutex instead.
package lrlock

import (
	"sync"
	"sync/atomic"
)

// LRMutex fulfills a similar role to sync.RWMutex. It can be used to make an
// arbitrary datastructure wait-free for readers. It does this by requiring
// clients to maintain two copies of the underlying datastructure, and
// carefully mediating access to these two instances.
//
// LRMutex has an extremely high memory cost; a sync.RWMutex uses O(1) memory, this
// lock uses O(P) memory (where P is essentially GOMAXPROCS), and the
// constant-factor on that is relatively high (in the worst case, something
// like 64 bytes). In addition, it requires two copies of the underlying
// datastructure (although the two datastructures can often share substructures
// if they are immutable or otherwise goroutine-safe).
type LRMutex struct {
	// Used for initialization.
	once sync.Once

	// Bit 0 is versionIndex.
	// Bit 1 is leftRight.
	state int32

	// To prevent multiple simulatenous writers.
	wmu sync.Mutex

	refCounts [2]*refCount
}

func (l *LRMutex) init() {
	l.state = 0
	l.refCounts[0] = allocateRefCount(nil)
	l.refCounts[1] = allocateRefCount(nil)
}

// A LockToken is returned when calling the Lock() method, and is used to
// mediate the write.
type LockToken struct {
	// Used to detect use of the zero value.
	nonzero bool

	// The first index read.
	startIdx byte

	// Number of index increments.
	incrs byte

	l *LRMutex
}

// Lock starts a round of the write protocol.
func (l *LRMutex) Lock() LockToken {
	l.once.Do(l.init)

	l.wmu.Lock()
	leftRight := atomic.LoadInt32(&l.state) >> 1

	return LockToken{
		nonzero:  true,
		startIdx: byte((leftRight + 1) % 2),
		incrs:    0,
		l:        l,
	}
}

// Next advances the write protocol to the next step.
func (w *LockToken) Next() bool {
	if !w.nonzero {
		panic("Use of a zero LockToken is invalid.")
	}
	if w.incrs >= 3 {
		panic("Cannot call Next() again after it has returned false.")
	}

	if w.incrs == 0 {
		w.incrs++
		return true
	}

	if w.incrs == 2 {
		w.incrs++
		w.l.wmu.Unlock()
		return false
	}

	state := atomic.LoadInt32(&w.l.state)

	// Toggle leftRight.
	{
		newLeftRight := ((state >> 1) + 1) % 2
		newState := (newLeftRight << 1) | (state & 1)
		atomic.StoreInt32(&w.l.state, newState)
		state = newState
	}

	// Wait for the counter that doesn't match versionIndex.
	w.waitForNonVersionIndex(state)

	// Toggle versionIndex
	{
		newVersionIndex := (state&1 + 1) % 2
		newState := (state & 2) | newVersionIndex
		atomic.StoreInt32(&w.l.state, newState)
		state = newState
	}

	w.waitForNonVersionIndex(state)

	w.incrs++
	return w.incrs < 3
}

// TODO: there should be a way to bail on the write protocol more quickly, if
// you see on the first index that no write is required. In that case, you
// shouldn't need to wait for two rounds of reader-draining.

func (w *LockToken) waitForNonVersionIndex(state int32) {
	rc := &w.l.refCounts[(state&1+1)%2]
	(*rc).wait()

	// Resize the refCount, now that no one is looking at it.
	*rc = allocateRefCount(*rc)
}

// Idx returns the index that the writer should be writing into at this time.
func (w *LockToken) Idx() int {
	if !w.nonzero {
		panic("Use of a zero LockToken is invalid.")
	}
	if w.incrs == 0 {
		panic("Cannot call Idx() before calling Next().")
	}
	if w.incrs > 2 {
		panic("Cannot call Idx() again after Next() has returned false.")
	}
	return int((w.startIdx + w.incrs - 1)) % 2
}

// An RLockToken is returned when calling the RLock() method, and is used to
// mediate the read.
type RLockToken struct {
	idx byte
	tok int16
	rc  *refCount
}

// RLock starts the read protocol.
func (l *LRMutex) RLock() RLockToken {
	l.once.Do(l.init)

	state0 := atomic.LoadInt32(&l.state)
	rc := l.refCounts[state0&1]
	tok := rc.acquire()

	if int(int16(tok)) != tok {
		panic("internal state error: refCount returned a token larger than 2^16.")
	}

	state1 := atomic.LoadInt32(&l.state)
	readIdx := (state1 >> 1) & 1

	return RLockToken{
		idx: byte(readIdx),
		tok: int16(tok),
		rc:  rc,
	}
}

// Idx returns the index that the reader should be reading from.
func (r RLockToken) Idx() int {
	if r.rc == nil {
		panic("Use of a zero RLockToken is invalid.")
	}
	if r.tok == -1 {
		panic("Use of a RLockToken after RUnlock is invalid.")
	}
	return int(r.idx)
}

// RUnlock concludes the read protocol.
func (r RLockToken) RUnlock() {
	if r.rc == nil {
		panic("Use of a zero RLockToken is invalid.")
	}
	if r.tok == -1 {
		panic("Use of a RLockToken after RUnlock is invalid.")
	}
	r.rc.release(int(r.tok))
	r.tok = -1
}
