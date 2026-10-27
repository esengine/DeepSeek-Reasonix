package checkpoint

import (
	"context"
	"fmt"
	"time"
)

type writerRegistration struct {
	writer  ActiveWriter
	ready   chan struct{}
	cancel  context.CancelFunc
	release func()
	err     error
}

// RegisterWriter preserves background-context admission for existing callers.
func (o *MutationObserver) RegisterWriter(id, kind string, turn int) error {
	_, err := o.RegisterWriterContext(context.Background(), id, kind, turn)
	return err
}

// RegisterWriterContext admits one identified background writer. Cancellation
// bounds preparation, not the lifetime of an already registered writer.
func (o *MutationObserver) RegisterWriterContext(ctx context.Context, id, kind string, turn int) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o == nil || id == "" || o.reg == nil {
		return func() {}, nil
	}
	o.reg.mu.Lock()
	if previous := o.reg.writers[id]; previous != nil {
		o.reg.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-previous.ready:
			if err := o.registrationResult(ctx, id, previous); err != nil {
				return nil, err
			}
			return func() { o.unregisterWriter(id, previous) }, nil
		}
	}
	prepare, cancel := context.WithCancel(ctx)
	defer cancel()
	r := &writerRegistration{writer: ActiveWriter{ID: id, Kind: kind, Turn: turn, StartedAt: time.Now()},
		ready: make(chan struct{}), cancel: cancel}
	o.reg.writers[id] = r
	o.reg.mu.Unlock()
	defer func() {
		if p := recover(); p != nil {
			o.unregisterWriter(id, r)
			o.finishRegistration(r, fmt.Errorf("writer preparation panicked: %v", p))
			panic(p)
		}
	}()
	release, err := o.BeginWriteContext(prepare)
	if err == nil && release == nil {
		release = func() {}
	}
	o.reg.mu.Lock()
	if err == nil {
		err = prepare.Err()
	}
	if err == nil && o.reg.writers[id] != r {
		err = context.Canceled
	}
	if err == nil {
		r.release = release
	} else if o.reg.writers[id] == r {
		delete(o.reg.writers, id)
	}
	o.reg.mu.Unlock()
	if err != nil && release != nil {
		release()
	}
	o.publishWriters()
	if err == nil {
		err = o.registrationResult(ctx, id, r)
		if err != nil {
			o.unregisterWriter(id, r)
		}
	}
	o.finishRegistration(r, err)
	if err != nil {
		return nil, fmt.Errorf("register background writer: %w", err)
	}
	return func() { o.unregisterWriter(id, r) }, nil
}

func (o *MutationObserver) finishRegistration(r *writerRegistration, err error) {
	o.reg.mu.Lock()
	r.err = err
	close(r.ready)
	o.reg.mu.Unlock()
}

func (o *MutationObserver) registrationResult(ctx context.Context, id string, r *writerRegistration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o.reg.mu.Lock()
	defer o.reg.mu.Unlock()
	if o.reg.writers[id] != r {
		if r.err != nil {
			return r.err
		}
		return context.Canceled
	}
	return r.err
}

// UnregisterWriter retires the current registration and only its own permit.
func (o *MutationObserver) UnregisterWriter(id string) {
	if o == nil || id == "" || o.reg == nil {
		return
	}
	o.unregisterWriter(id, nil)
}

func (o *MutationObserver) unregisterWriter(id string, expected *writerRegistration) {
	o.reg.mu.Lock()
	r := o.reg.writers[id]
	if r == nil || expected != nil && expected != r {
		o.reg.mu.Unlock()
		return
	}
	delete(o.reg.writers, id)
	release := r.release
	o.reg.mu.Unlock()
	r.cancel()
	if release != nil {
		release()
		o.publishWriters()
	}
}

func (o *MutationObserver) publishWriters() {
	if o.store == nil {
		return
	}
	o.reg.publish.Lock()
	defer o.reg.publish.Unlock()
	o.reg.mu.Lock()
	snapshot := o.snapshotWritersLocked()
	o.reg.mu.Unlock()
	o.store.SetActiveWriters(snapshot)
}
