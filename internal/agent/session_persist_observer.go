package agent

import (
	"os"

	"reasonix/internal/store"
)

// SessionPersistEvent is emitted only after the authoritative transcript,
// event log, metadata ledger, and derived display read model have completed
// their save boundary. Observers must only enqueue work and return immediately.
type SessionPersistEvent struct {
	Path          string
	Revision      int64
	ContentDigest string
	MessageCount  int
	AppendFrom    int
	Rewrite       bool
	Removed       bool
}

type SessionPersistObserver interface {
	EnqueueSessionPersist(SessionPersistEvent) bool
}

func (s *Session) SetPersistObserver(observer SessionPersistObserver) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.persistObserver = observer
	s.mu.Unlock()
}

func (s *Session) notifyPersisted(path string, rewrite bool, appendFrom int) {
	if s == nil {
		return
	}
	s.mu.RLock()
	observer := s.persistObserver
	messageCount := len(s.Messages)
	s.mu.RUnlock()
	if observer == nil {
		return
	}
	state, known := s.PersistedState(path)
	event := SessionPersistEvent{Path: path, MessageCount: messageCount, AppendFrom: appendFrom, Rewrite: rewrite}
	if known {
		event.Revision = state.Revision
		event.ContentDigest = state.DigestHex
	}
	observer.EnqueueSessionPersist(event)
}

// SaveIfAbsent persists a newly imported or copied session without replacing a
// destination another runtime created after the caller's scan.
func (s *Session) SaveIfAbsent(path string) error {
	err := s.withSessionSaveLocks(path, func() error {
		if sessionArtifactExists(path) {
			return os.ErrExist
		}
		return s.saveLocked(path, sessionSaveSnapshot)
	})
	if err == nil {
		s.notifyPersisted(path, false, -1)
	}
	return err
}

func (s *Session) saveObserved(path string, mode sessionSaveMode) error {
	appendFrom := -1
	if mode == sessionSaveSnapshot {
		if index, err := LoadSessionDisplayIndex(store.SessionDisplayIndex(path)); err == nil {
			appendFrom = index.MessageCount
		}
	}
	err := s.save(path, mode)
	if err == nil {
		s.notifyPersisted(path, mode != sessionSaveSnapshot, appendFrom)
	}
	return err
}
