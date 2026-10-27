package workspacelease

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

type leaseExtent struct {
	paths []string
	set   bool
}

func (e *leaseExtent) add(paths []string) {
	if !e.set {
		e.paths, e.set = slices.Clone(paths), true
		return
	}
	if coversPaths(e.paths, paths) {
		return
	}
	if len(paths) == 0 {
		e.paths = nil
		return
	}
	for _, path := range paths {
		if coversPaths(e.paths, []string{path}) {
			continue
		}
		var kept []string
		for _, old := range e.paths {
			if !pathWithin(path, old) {
				kept = append(kept, old)
			}
		}
		e.paths = append(kept, path)
	}
}

// PreparedCall holds a provisional extent until mutation entry or cleanup.
// Retain must precede any potentially writing hook or concrete execution.
type PreparedCall struct {
	hold      *callHold
	closeOnce sync.Once
	closeErr  error
}

// PreparePaths acquires a call's complete extent without retaining new paths
// for the run. Close removes only this call's contribution unless Retain ran.
// The returned context permits the call itself to Yield before execution.
func (o *Owner) PreparePaths(ctx context.Context, paths []string) (context.Context, *PreparedCall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o == nil {
		return ctx, nil, nil
	}
	h := o.newCall(false)
	if err := o.acquire(ctx, paths, h); err != nil {
		return ctx, nil, err
	}
	return context.WithValue(ctx, callKey{}, h), &PreparedCall{hold: h}, nil
}

func (o *Owner) newCall(retained bool) *callHold {
	o.mu.Lock()
	defer o.mu.Unlock()
	return &callHold{owner: o, retained: retained, generation: o.holds.generation}
}

func (o *Owner) registerCallLocked(h *callHold, paths []string) {
	if h != nil && h.done {
		return
	}
	if h == nil || h.retained {
		o.holds.retained.add(paths)
	}
	if h == nil {
		return
	}
	if o.holds.calls == nil {
		o.holds.calls = make(map[*callHold]struct{})
	}
	o.holds.calls[h] = struct{}{}
	h.paths = slices.Clone(paths)
	if !h.live {
		h.live = true
		o.holds.inFlight++
	}
}

// Retain preserves this extent through the final participating run, including
// failed or partially executed writes. It is idempotent after cleanup.
func (p *PreparedCall) Retain() {
	if p == nil || p.hold == nil {
		return
	}
	h := p.hold
	o := h.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, present := o.holds.calls[h]; present && !h.done && !h.retained && h.generation == o.holds.generation {
		h.retained = true
		if h.live {
			o.holds.retained.add(h.paths)
		} else {
			o.holds.parkedRetained.add(h.paths)
		}
	}
}

func (o *Owner) callExtentLocked() leaseExtent {
	extent := o.holds.retained
	for h := range o.holds.calls {
		if h.live {
			extent.add(h.paths)
		}
	}
	return extent
}

func (o *Owner) restoreExtentLocked() leaseExtent {
	extent := o.holds.retained
	if o.holds.parkedRetained.set {
		extent.add(o.holds.parkedRetained.paths)
	}
	for h := range o.holds.calls {
		if !h.done && h.generation == o.holds.generation {
			extent.add(h.paths)
		}
	}
	return extent
}

func (o *Owner) resetCallsLocked() {
	o.holds.retained = leaseExtent{}
	for h := range o.holds.calls {
		h.done = true
	}
	clear(o.holds.calls)
	o.holds.yielders = 0
	o.holds.parked, o.holds.parkedSet = nil, false
	o.holds.parkedRetained = leaseExtent{}
}

// Close removes an unretained extent through the existing claim registrar.
// If publication fails, the old extent stays protected until the run ends and
// the error is returned. Cleanup is idempotent and never steals another claim.
func (p *PreparedCall) Close(ctx context.Context) error {
	if p == nil || p.hold == nil {
		return nil
	}
	p.closeOnce.Do(func() { p.closeErr = p.close(ctx) })
	return p.closeErr
}

func (p *PreparedCall) close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	h := p.hold
	o := h.owner
	for {
		o.mu.Lock()
		if h.done {
			o.mu.Unlock()
			return nil
		}
		if o.acquiring {
			done := o.acquireDone
			o.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				p.Retain()
				h.end()
				return ctx.Err()
			}
		}
		h.done = true
		delete(o.holds.calls, h)
		if h.live {
			h.live = false
			o.holds.inFlight--
		}
		extent := o.callExtentLocked()
		if !o.acquired {
			o.mu.Unlock()
			return nil
		}
		if !extent.set || o.activeRuns == 0 && o.holds.inFlight == 0 {
			release := o.releaseLocked(o.activeRuns == 0 || !o.holds.parkedSet)
			o.mu.Unlock()
			release()
			return nil
		}
		if coversPaths(extent.paths, o.scope.paths) {
			release := o.releaseIfIdleLocked()
			o.mu.Unlock()
			if release != nil {
				release()
			}
			return nil
		}
		o.acquiring, o.acquireDone = true, make(chan struct{})
		done := o.acquireDone
		o.mu.Unlock()
		_, err := o.acquirePaths(ctx, extent.paths, true)
		o.mu.Lock()
		if err != nil {
			o.holds.retained.add(o.scope.paths)
		} else {
			o.scope.paths = extent.paths
		}
		o.acquiring = false
		o.waiting = false
		close(done)
		release := o.releaseIfIdleLocked()
		o.mu.Unlock()
		if release != nil {
			release()
		}
		if err != nil {
			return fmt.Errorf("publish workspace lease cleanup: %w", err)
		}
		return nil
	}
}
