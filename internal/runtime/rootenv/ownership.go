package rootenv

import (
	"maps"
	"slices"
	"sync"
)

// Environment is one runtime's reference to an execution ownership generation.
// Rebuilds retain independent references before retiring the old runtime.
type Environment struct {
	mu     sync.Mutex
	closed bool
	owner  *ownership
}

type ownership struct {
	mu      sync.Mutex
	store   *Store
	record  Record
	release func()
	refs    int
}

func newEnvironment(store *Store, record Record, release func()) *Environment {
	return &Environment{owner: &ownership{store: store, record: record, release: release, refs: 1}}
}

func (e *Environment) Record() Record {
	e.owner.mu.Lock()
	defer e.owner.mu.Unlock()
	r := e.owner.record
	r.CheckContract = r.CheckContract.clone()
	r.Attachments = maps.Clone(r.Attachments)
	if r.Publication != nil {
		r.Publication = r.Publication.clone()
	}
	if r.Materialization != nil {
		copy := *r.Materialization
		r.Materialization = &copy
	}
	if r.Environment != nil {
		copy := *r.Environment
		copy.Paths = slices.Clone(copy.Paths)
		r.Environment = &copy
	}
	return r
}

func (e *Environment) Retain() (*Environment, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrStale
	}
	e.owner.mu.Lock()
	defer e.owner.mu.Unlock()
	if e.owner.release == nil {
		return nil, ErrStale
	}
	e.owner.refs++
	return &Environment{owner: e.owner}, nil
}

// Close releases only this runtime's reference. The last reference ends ownership.
// Every close retains the persistent worktree, baseline anchor and session binding.
func (e *Environment) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	e.owner.mu.Lock()
	defer e.owner.mu.Unlock()
	e.owner.refs--
	if e.owner.refs == 0 && e.owner.release != nil {
		e.owner.release()
		e.owner.release = nil
	}
	return nil
}
