package edge

import (
	"sync"
	"time"
)

// reasonixRecoveryState binds one renderer failure to the navigation started by
// its native Reload call. Navigation events may overlap, so completion is only
// accepted after the matching navigation ID has been observed.
type reasonixRecoveryState[T any] struct {
	mu              sync.Mutex
	pending         *T
	navigationID    uint64
	navigationBound bool
	last            time.Time
	timer           *time.Timer
}

func (s *reasonixRecoveryState[T]) begin(
	value T,
	now time.Time,
	cooldown time.Duration,
	timeout time.Duration,
	onTimeout func(T),
) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil || (!s.last.IsZero() && now.Sub(s.last) < cooldown) {
		return false
	}
	s.last = now
	s.pending = &value
	s.navigationID = 0
	s.navigationBound = false
	if timeout > 0 && onTimeout != nil {
		s.timer = time.AfterFunc(timeout, func() {
			if timedOut, ok := s.finish(); ok {
				onTimeout(timedOut)
			}
		})
	}
	return true
}

func (s *reasonixRecoveryState[T]) hasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending != nil
}

func (s *reasonixRecoveryState[T]) bindNavigation(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil || s.navigationBound {
		return false
	}
	s.navigationID = id
	s.navigationBound = true
	return true
}

func (s *reasonixRecoveryState[T]) completeNavigation(id uint64) (T, bool) {
	s.mu.Lock()
	if s.pending == nil || !s.navigationBound || s.navigationID != id {
		var zero T
		s.mu.Unlock()
		return zero, false
	}
	return s.finishLocked()
}

func (s *reasonixRecoveryState[T]) finish() (T, bool) {
	s.mu.Lock()
	if s.pending == nil {
		var zero T
		s.mu.Unlock()
		return zero, false
	}
	return s.finishLocked()
}

func (s *reasonixRecoveryState[T]) finishLocked() (T, bool) {
	value := *s.pending
	s.pending = nil
	s.navigationID = 0
	s.navigationBound = false
	timer := s.timer
	s.timer = nil
	s.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	return value, true
}
