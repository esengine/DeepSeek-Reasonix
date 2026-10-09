package workspacelease

import (
	"context"
	"slices"
	"sync"
)

// holdState is what the claim's lifetime depends on besides runs: calls that
// are executing under it, and sessions waiting on a person who took it off.
type holdState struct {
	inFlight  int
	yielders  int
	parked    []string
	parkedSet bool
}

// HoldPaths is AcquirePaths for a call about to write: until the returned func
// runs, the claim is not given back by Yield.
func (o *Owner) HoldPaths(ctx context.Context, paths []string) (func(), error) {
	return o.acquire(ctx, paths, true)
}

func (o *Owner) holdLocked(hold bool) func() {
	if !hold {
		return func() {}
	}
	o.holds.inFlight++
	var once sync.Once
	return func() {
		once.Do(func() {
			o.mu.Lock()
			o.holds.inFlight--
			o.mu.Unlock()
		})
	}
}

// Yield gives the claim back while the session waits on a person and returns
// the func that ends that wait. Waits nest: the claim is taken again, queueing
// behind any session that claimed it meanwhile, only when the last waiter
// resumes. Nothing is given back while a call is mid-write or an acquisition
// is in progress; the wait still counts.
func (o *Owner) Yield() func(context.Context) error {
	if o == nil {
		return noResume
	}
	o.mu.Lock()
	o.holds.yielders++
	var release func()
	if o.acquired && !o.acquiring && o.holds.inFlight == 0 {
		held := slices.Clone(o.scope.paths)
		if o.holds.parkedSet {
			held = mergePaths(o.holds.parked, held)
		}
		o.holds.parked, o.holds.parkedSet = held, true
		release = o.releaseLocked(false)
	}
	o.mu.Unlock()
	if release != nil {
		release()
	}
	var once sync.Once
	return func(ctx context.Context) error {
		var err error
		once.Do(func() { err = o.resume(ctx) })
		return err
	}
}

func (o *Owner) resume(ctx context.Context) error {
	o.mu.Lock()
	o.holds.yielders--
	if o.holds.yielders > 0 || !o.holds.parkedSet {
		o.mu.Unlock()
		return nil
	}
	parked := o.holds.parked
	o.holds.parked, o.holds.parkedSet = nil, false
	o.mu.Unlock()
	return o.AcquirePaths(ctx, parked)
}

func noResume(context.Context) error { return nil }
