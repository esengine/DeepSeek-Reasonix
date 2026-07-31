package safety

import (
	"sync"
	"time"
)

// ProcessingLock is a short-TTL in-memory lock to prevent concurrent processing
// of the same event.
type ProcessingLock struct {
	mu     sync.Mutex
	locks  map[string]int64 // id -> expireAt (Unix ms)
	ttlMs  int64
	ticker *time.Ticker
	stopCh chan struct{}
}

// NewProcessingLock creates a new ProcessingLock.
func NewProcessingLock(ttl time.Duration, sweepInterval time.Duration) *ProcessingLock {
	pl := &ProcessingLock{
		locks:  make(map[string]int64),
		ttlMs:  ttl.Milliseconds(),
		ticker: time.NewTicker(sweepInterval),
		stopCh: make(chan struct{}),
	}
	go pl.sweepLoop()
	return pl
}

// Acquire returns true if the lock is successfully acquired, false if already held.
func (pl *ProcessingLock) Acquire(id string) bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	now := time.Now().UnixMilli()
	exp, exists := pl.locks[id]
	if exists && exp > now {
		return false
	}
	pl.locks[id] = now + pl.ttlMs
	return true
}

// Release releases the lock for the given id.
func (pl *ProcessingLock) Release(id string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	delete(pl.locks, id)
}

func (pl *ProcessingLock) sweepLoop() {
	for {
		select {
		case <-pl.ticker.C:
			pl.sweep()
		case <-pl.stopCh:
			pl.ticker.Stop()
			return
		}
	}
}

func (pl *ProcessingLock) sweep() {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	now := time.Now().UnixMilli()
	for id, exp := range pl.locks {
		if exp <= now {
			delete(pl.locks, id)
		}
	}
}

// Dispose stops the sweeper and clears the locks.
func (pl *ProcessingLock) Dispose() {
	close(pl.stopCh)
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.locks = make(map[string]int64)
}
