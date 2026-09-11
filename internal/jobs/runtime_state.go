package jobs

import "sync"

// RuntimeState counts operational work, including cancelled processes which
// have not exited. Revisions order updates from concurrent jobs in a manager.
type RuntimeState struct {
	SessionID string
	JobID     string
	Revision  uint64
	Running   int
}

type runtimeObservers struct {
	mu        sync.Mutex
	revision  uint64
	next      uint64
	listeners map[uint64]*runtimeSubscription
}

type runtimeSubscription struct {
	mu       sync.Mutex
	session  string
	callback func(RuntimeState)
	pending  *RuntimeState
	draining bool
	closed   bool
}

func (s *runtimeSubscription) enqueue(state RuntimeState) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.pending = &state
	if s.draining {
		s.mu.Unlock()
		return
	}
	s.draining = true
	s.mu.Unlock()
	go func() {
		for {
			s.mu.Lock()
			if s.closed || s.pending == nil {
				s.draining = false
				s.mu.Unlock()
				return
			}
			state := *s.pending
			s.pending = nil
			s.mu.Unlock()
			s.callback(state)
		}
	}()
}

// SubscribeRuntime registers before reading, so a completion cannot disappear
// in the subscribe/read gap. Callbacks run off-lock with one pending snapshot.
func (m *Manager) SubscribeRuntime(session string, callback func(RuntimeState)) (RuntimeState, func()) {
	m.runtimeObservers.mu.Lock()
	defer m.runtimeObservers.mu.Unlock()
	m.runtimeObservers.next++
	id := m.runtimeObservers.next
	if m.runtimeObservers.listeners == nil {
		m.runtimeObservers.listeners = map[uint64]*runtimeSubscription{}
	}
	sub := &runtimeSubscription{session: session, callback: callback}
	m.runtimeObservers.listeners[id] = sub
	initial := RuntimeState{SessionID: session, Revision: m.runtimeObservers.revision, Running: len(m.RunningForSession(session))}
	return initial, func() {
		m.runtimeObservers.mu.Lock()
		delete(m.runtimeObservers.listeners, id)
		m.runtimeObservers.mu.Unlock()
		sub.mu.Lock()
		sub.closed = true
		sub.pending = nil
		sub.mu.Unlock()
	}
}

// notifyRuntime runs only after committing a start or closing done. Notices
// still precede done to protect DrainCompletedNote; they are not state signals.
func (m *Manager) notifyRuntime(session, jobID string) {
	m.runtimeObservers.mu.Lock()
	defer m.runtimeObservers.mu.Unlock()
	m.runtimeObservers.revision++
	for _, sub := range m.runtimeObservers.listeners {
		if sub.session != "" && session != "" && sub.session != session {
			continue
		}
		sub.enqueue(RuntimeState{SessionID: sub.session, JobID: jobID, Revision: m.runtimeObservers.revision, Running: len(m.RunningForSession(sub.session))})
	}
}
