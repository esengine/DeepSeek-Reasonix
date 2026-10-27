package checkpoint

import (
	"context"
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
	return b.EnterWriteContext(context.Background())
}

// EnterWriteContext admits a writer unless its wait is cancelled.
func (b *MutationBarrier) EnterWriteContext(ctx context.Context) error {
	return b.enter(ctx, false)
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
	return b.EnterExclusiveContext(context.Background())
}

// EnterExclusiveContext waits for exclusive access unless ctx is cancelled.
func (b *MutationBarrier) EnterExclusiveContext(ctx context.Context) error {
	return b.enter(ctx, true)
}

func (b *MutationBarrier) enter(ctx context.Context, exclusive bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// Register under mu so cancellation cannot be lost between the context
	// check and Wait. Stopping the callback never waits for it under mu.
	stop := context.AfterFunc(ctx, func() {
		b.mu.Lock()
		b.cond.Broadcast()
		b.mu.Unlock()
	})
	defer stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if b.closed {
			return fmt.Errorf("mutation barrier closed")
		}
		if !b.exclusive && (!exclusive || b.writers == 0) {
			if exclusive {
				b.exclusive = true
			} else {
				b.writers++
			}
			return nil
		}
		b.cond.Wait()
	}
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

// HasWriters reads writer activity without taking exclusive mutation access.
func (b *MutationBarrier) HasWriters() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writers > 0
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
