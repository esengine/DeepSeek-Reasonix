package checkpoint

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// MutationBarrier provides exclusive workspace mutation access for rewind
// transactions. It is intentionally separate from App.mu / Controller locks so
// file I/O never runs under those mutexes.
//
// Writers call EnterWrite / ExitWrite around mutations.
// Rewind holds EnterExclusive for the whole prepare+commit critical section.
type MutationBarrier struct {
	mu        sync.Mutex
	cond      *sync.Cond
	writers   int
	exclusive bool
	// generation increments on every exclusive release so prepare tokens can
	// detect concurrent mutation without relying on wall-clock time.
	generation atomic.Uint64
	// closed rejects new enters after shutdown (optional).
	closed bool
}

// NewMutationBarrier returns a ready barrier.
func NewMutationBarrier() *MutationBarrier {
	b := &MutationBarrier{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// Generation returns the current exclusive-release generation.
func (b *MutationBarrier) Generation() uint64 {
	if b == nil {
		return 0
	}
	return b.generation.Load()
}

// EnterWrite blocks until exclusive access is free, then increments the writer count.
func (b *MutationBarrier) EnterWrite() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.exclusive || b.closed {
		if b.closed {
			return fmt.Errorf("mutation barrier closed")
		}
		b.cond.Wait()
	}
	b.writers++
	return nil
}

// TryEnterWrite is a non-blocking EnterWrite.
func (b *MutationBarrier) TryEnterWrite() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exclusive || b.closed {
		return false
	}
	b.writers++
	return true
}

// ExitWrite decrements the writer count and advances the workspace generation.
// Plans prepared before a completed writer can therefore never authorize a
// later commit without a fresh preview.
func (b *MutationBarrier) ExitWrite() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.writers > 0 {
		b.writers--
		b.generation.Add(1)
	}
	if b.writers == 0 {
		b.cond.Broadcast()
	}
}

// EnterExclusive waits until no writers hold the barrier, then takes exclusive.
func (b *MutationBarrier) EnterExclusive() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.exclusive || b.writers > 0 || b.closed {
		if b.closed {
			return fmt.Errorf("mutation barrier closed")
		}
		b.cond.Wait()
	}
	b.exclusive = true
	return nil
}

// TryEnterExclusive is a non-blocking EnterExclusive.
func (b *MutationBarrier) TryEnterExclusive() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exclusive || b.writers > 0 || b.closed {
		return false
	}
	b.exclusive = true
	return true
}

// ExitExclusive releases exclusive access and bumps generation.
func (b *MutationBarrier) ExitExclusive() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.exclusive = false
	b.generation.Add(1)
	b.cond.Broadcast()
}

// Busy reports whether exclusive is held or writers are active.
func (b *MutationBarrier) Busy() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exclusive || b.writers > 0
}

// Close rejects future enters (best-effort shutdown).
func (b *MutationBarrier) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.closed = true
	b.cond.Broadcast()
	b.mu.Unlock()
}
