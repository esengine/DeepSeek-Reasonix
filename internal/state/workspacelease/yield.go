package workspacelease

import (
	"context"
	"errors"
	"slices"
	"sync"
)

// holdState is what the claim's lifetime depends on besides runs: calls that
// are executing under it, and sessions waiting on a person who took it off.
type holdState struct {
	generation uint64
	retained   leaseExtent
	calls      map[*callHold]struct{}
	inFlight   int
	yielders   int
	parked     []string
	parkedSet  bool
	// parkedMark is the claim record when the parked extent was given up.
	parkedMark claimMark
}

// callHold is one writing call's share of inFlight. It stays reachable from the
// call's context so a question that call asks can suspend it.
type callHold struct {
	owner    *Owner
	paths    []string
	live     bool
	done     bool
	retained bool
}

type callKey struct{}

// HoldPaths is AcquirePaths for a call about to write: until the returned func
// runs, the claim is not given back by Yield, except by a Yield made with the
// returned context, which is the call itself asking before it writes.
func (o *Owner) HoldPaths(ctx context.Context, paths []string) (context.Context, func(), error) {
	if o == nil {
		return ctx, func() {}, nil
	}
	h := &callHold{owner: o, retained: true}
	err := o.acquire(ctx, paths, h)
	if err != nil {
		return ctx, func() {}, err
	}
	return context.WithValue(ctx, callKey{}, h), h.end, nil
}

func (h *callHold) end() {
	o := h.owner
	o.mu.Lock()
	h.done = true
	if h.live {
		h.live = false
		o.holds.inFlight--
	}
	delete(o.holds.calls, h)
	release := o.releaseIfIdleLocked()
	o.mu.Unlock()
	if release != nil {
		release()
	}
}

// Resume ends a yield. It takes the claim again, queueing behind any session
// that claimed it meanwhile, and names the sessions that did.
type Resume func(context.Context) (Contention, error)

// ErrHoldEnded refuses restoration after the call or run's lifetime ended.
var ErrHoldEnded = errors.New("workspace write hold has ended")

// Yield gives the claim back while the session waits on a person and returns
// the func that ends that wait. Waits nest: the claim is taken again only when
// the last waiter resumes. Nothing is given back while another call is
// mid-write or an acquisition is in progress; the wait still counts. A ctx from
// HoldPaths suspends that call's own hold, since it is the one asking.
func (o *Owner) Yield(ctx context.Context) Resume {
	if o == nil {
		return noResume
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mark := o.claimMark()
	o.mu.Lock()
	generation := o.holds.generation
	o.holds.yielders++
	var suspended *callHold
	if h, _ := ctx.Value(callKey{}).(*callHold); h != nil && h.owner == o && h.live {
		h.live = false
		o.holds.inFlight--
		suspended = h
	}
	var release func()
	if o.acquired && !o.acquiring && o.holds.inFlight == 0 {
		held := slices.Clone(o.scope.paths)
		if o.holds.parkedSet {
			held = mergePaths(o.holds.parked, held)
		} else {
			o.holds.parkedMark = mark
		}
		o.holds.parked, o.holds.parkedSet = held, true
		release = o.releaseLocked(false)
	}
	o.mu.Unlock()
	if release != nil {
		release()
	}
	var once sync.Once
	var contention Contention
	var resumeErr error
	return func(ctx context.Context) (Contention, error) {
		once.Do(func() { contention, resumeErr = o.resume(ctx, suspended, generation) })
		return contention, resumeErr
	}
}

func (o *Owner) resume(ctx context.Context, suspended *callHold, generation uint64) (Contention, error) {
	o.mu.Lock()
	if generation != o.holds.generation {
		o.mu.Unlock()
		return Contention{}, ErrHoldEnded
	}
	o.holds.yielders--
	last := o.holds.yielders == 0 && o.holds.parkedSet
	parked, mark := o.holds.parked, o.holds.parkedMark
	if last {
		o.holds.parked, o.holds.parkedSet = nil, false
	}
	o.mu.Unlock()
	var contention Contention
	if last {
		o.mu.Lock()
		extent := o.callExtentLocked()
		o.mu.Unlock()
		if extent.set {
			restore := &callHold{owner: o}
			if err := o.acquire(ctx, extent.paths, restore); err != nil {
				return Contention{}, err
			}
			if err := (&PreparedCall{hold: restore}).Close(ctx); err != nil {
				return Contention{}, err
			}
		}
		contention = o.contendersSince(mark, parked)
	}
	if suspended != nil {
		if err := o.rehold(ctx, suspended); err != nil {
			return contention, err
		}
	}
	return contention, nil
}

// rehold puts a suspended call back under the claim it gave up, waiting for it
// if another session has it.
func (o *Owner) rehold(ctx context.Context, h *callHold) error {
	return o.acquire(ctx, h.paths, h)
}

func noResume(context.Context) (Contention, error) { return Contention{}, nil }
