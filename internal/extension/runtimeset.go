package extension

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// RuntimeSet tracks live resources bound to one RuntimeSnapshot generation.
// It is separate from the immutable snapshot and holds an EffectScope so
// activation, receipts, and typed dispose share one owner. CloseIfGeneration
// keeps stale cleanup from closing a newer runtime's resources.
type RuntimeSet struct {
	mu    sync.Mutex
	scope *LiveScope
	// nextCloser sequences auto-generated closer ids for Add.
	nextCloser int
}

// NewRuntimeSet returns an empty set bound to a snapshot generation.
func NewRuntimeSet(generation uint64) *RuntimeSet {
	return &RuntimeSet{scope: NewEffectScope(generation)}
}

// Scope returns the underlying EffectScope for typed effect registration.
func (s *RuntimeSet) Scope() EffectScope {
	if s == nil || s.scope == nil {
		return nil
	}
	return s.scope
}

// Generation returns the snapshot generation this set is bound to.
func (s *RuntimeSet) Generation() uint64 {
	if s == nil || s.scope == nil {
		return 0
	}
	return s.scope.Generation()
}

// Add registers closers as reversible effects. A closer added to an already
// closed set is closed immediately — the alternative (silently leaking it
// because the owner went away between activation and registration) is how
// sidecar processes outlive their session.
func (s *RuntimeSet) Add(closers ...io.Closer) {
	if s == nil {
		return
	}
	for _, c := range closers {
		if c == nil {
			continue
		}
		s.mu.Lock()
		s.nextCloser++
		id := fmt.Sprintf("closer-%d", s.nextCloser)
		s.mu.Unlock()
		_ = s.scope.TrackCloser(id, c)
	}
}

// Track registers a typed effect on the set's scope.
func (s *RuntimeSet) Track(e Effect) error {
	if s == nil || s.scope == nil {
		return fmt.Errorf("extension: nil RuntimeSet")
	}
	return s.scope.Track(e)
}

// Len returns the number of registered effects that have not yet been disposed.
func (s *RuntimeSet) Len() int {
	if s == nil || s.scope == nil {
		return 0
	}
	s.scope.mu.Lock()
	defer s.scope.mu.Unlock()
	return len(s.scope.effects)
}

// Close releases every registered resource, in reverse registration order
// (later resources may depend on earlier ones). It is idempotent: concurrent
// or repeated calls after the first return nil without re-closing, because
// most closers are not safe to invoke twice.
func (s *RuntimeSet) Close() error {
	if s == nil || s.scope == nil {
		return nil
	}
	return s.scope.Dispose(context.Background())
}

// CloseIfGeneration closes only when gen matches; wrong generations leave
// resources untouched. Errors collapse to the bool (fire-and-forget cleanup).
func (s *RuntimeSet) CloseIfGeneration(gen uint64) bool {
	if s == nil || s.scope == nil {
		return false
	}
	if gen != s.scope.Generation() {
		return false
	}
	_ = s.Close()
	return true
}

// Closed reports whether Close has run.
func (s *RuntimeSet) Closed() bool {
	if s == nil || s.scope == nil {
		return true
	}
	return s.scope.Closed()
}

// Receipts returns effect receipts recorded by this generation.
func (s *RuntimeSet) Receipts() []EffectReceipt {
	if s == nil || s.scope == nil {
		return nil
	}
	return s.scope.Receipts()
}
