package mutation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"reasonix/internal/base/filelock"
)

const waitSnapshotLimit = 128

type waitSnapshot struct {
	ready   chan struct{}
	created time.Time
	expires time.Time
	records []record
	err     error
}

type waitSnapshotPool struct {
	mu      sync.Mutex
	entries map[string]*waitSnapshot
}

var waitingSnapshots waitSnapshotPool

func (p *waitSnapshotPool) get(ctx context.Context, key string, read func(context.Context) ([]record, error)) ([]record, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		snapshot, load := p.entry(key)
		if snapshot == nil {
			return readWaitSnapshot(ctx, read)
		}
		if load {
			snapshot.records, snapshot.err = readWaitSnapshot(ctx, read)
			snapshot.expires = time.Now().Add(retryInterval)
			close(snapshot.ready)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-snapshot.ready:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if errors.Is(snapshot.err, context.Canceled) || errors.Is(snapshot.err, context.DeadlineExceeded) {
				p.forget(key, snapshot)
				if ctx.Err() == nil {
					continue
				}
			}
			return snapshot.records, snapshot.err
		}
	}
}

func readWaitSnapshot(ctx context.Context, read func(context.Context) ([]record, error)) (records []record, err error) {
	defer func() {
		if p := recover(); p != nil {
			records, err = nil, fmt.Errorf("%w: wait snapshot reader panicked: %v", ErrCallback, p)
		}
	}()
	return read(ctx)
}

func (p *waitSnapshotPool) entry(key string) (*waitSnapshot, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = make(map[string]*waitSnapshot)
	}
	if snapshot := p.entries[key]; snapshot != nil {
		select {
		case <-snapshot.ready:
			if time.Now().Before(snapshot.expires) {
				return snapshot, false
			}
			delete(p.entries, key)
		default:
			return snapshot, false
		}
	}
	if len(p.entries) >= waitSnapshotLimit && !p.evict() {
		return nil, false
	}
	snapshot := &waitSnapshot{ready: make(chan struct{}), created: time.Now()}
	p.entries[key] = snapshot
	return snapshot, true
}

// Only completed observations are evicted; active readers retain their rendezvous.
func (p *waitSnapshotPool) evict() bool {
	var oldest *waitSnapshot
	var victim string
	for key, snapshot := range p.entries {
		select {
		case <-snapshot.ready:
			if oldest == nil || snapshot.created.Before(oldest.created) {
				victim, oldest = key, snapshot
			}
		default:
		}
	}
	if oldest == nil {
		return false
	}
	delete(p.entries, victim)
	return true
}

func (p *waitSnapshotPool) forget(key string, snapshot *waitSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries[key] == snapshot {
		delete(p.entries, key)
	}
}

// A shared observation can defer a retry, never authorize admission. The grant
// path still rereads the registry and compatibility claims under their OS locks.
func (c *Coordinator) retryGrant(ctx context.Context, lease *Lease, previous *blocker) (*blocker, error) {
	if previous != nil && previous.kind == NamedPaths && previous.ID != "" {
		records, err := waitingSnapshots.get(ctx, c.dir, c.readWaitingRecords)
		if err != nil {
			return nil, err
		}
		if held := pathBlocker(lease.record, records); held != nil {
			return &blocker{record: *held, kind: NamedPaths}, nil
		}
	}
	return c.tryGrant(ctx, lease)
}

func (c *Coordinator) readWaitingRecords(ctx context.Context) ([]record, error) {
	release, err := filelock.Acquire(ctx, c.guardPath())
	if err != nil {
		return nil, err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.records()
}
