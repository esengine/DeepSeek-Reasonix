package extension

import (
	"errors"
	"io"
	"slices"
	"sync"
)

// RuntimeSet tracks the closable resources bound to one RuntimeSnapshot —
// MCP sidecar processes, watchers, temp files. It exists separately from the
// snapshot because a snapshot is immutable value state while its resources
// have a lifecycle: when a newer snapshot activates, the old RuntimeSet is
// closed exactly once, and CloseIfGeneration makes sure a stale cleanup path
// can never close resources that now belong to a newer runtime.
type RuntimeSet struct {
	mu         sync.Mutex
	generation uint64
	closers    []io.Closer
	closed     bool
}

// NewRuntimeSet returns an empty set bound to a snapshot generation.
func NewRuntimeSet(generation uint64) *RuntimeSet {
	return &RuntimeSet{generation: generation}
}

// Generation returns the snapshot generation this set is bound to.
func (s *RuntimeSet) Generation() uint64 { return s.generation }

// Add registers closers. A closer added to an already-closed set is closed
// immediately — the alternative (silently leaking it because the owner went
// away between activation and registration) is how sidecar processes outlive
// their session.
func (s *RuntimeSet) Add(closers ...io.Closer) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		for _, c := range closers {
			if c != nil {
				_ = c.Close()
			}
		}
		return
	}
	for _, c := range closers {
		if c != nil {
			s.closers = append(s.closers, c)
		}
	}
	s.mu.Unlock()
}

// Len returns the number of registered closers.
func (s *RuntimeSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.closers)
}

// Close releases every registered resource, in reverse registration order
// (later resources may depend on earlier ones). It is idempotent: concurrent
// or repeated calls after the first return nil without re-closing, because
// most closers are not safe to invoke twice.
func (s *RuntimeSet) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	closers := s.closers
	s.closers = nil
	s.mu.Unlock()

	var errs []error
	for _, v := range slices.Backward(closers) {
		if err := v.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CloseIfGeneration closes the set only when gen matches the generation it
// was built for, and reports whether the close ran. A wrong generation means
// the caller's snapshot is stale and these resources already belong to a
// newer runtime, so they are left untouched. Close errors are intentionally
// collapsed into the bool: this is the fire-and-forget cleanup path — callers
// that need error detail call Close themselves.
func (s *RuntimeSet) CloseIfGeneration(gen uint64) bool {
	s.mu.Lock()
	matched := gen == s.generation
	s.mu.Unlock()
	if !matched {
		return false
	}
	_ = s.Close()
	return true
}

// Closed reports whether Close has run.
func (s *RuntimeSet) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
