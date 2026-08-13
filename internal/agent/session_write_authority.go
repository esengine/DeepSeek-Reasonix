package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrSessionWriteAuthorityMissing is returned when a save or ownership-
// sensitive rewrite runs without a write authority bound for the target path.
// Callers must re-acquire a lease and rebind rather than forking recovery.
var ErrSessionWriteAuthorityMissing = errors.New("session write authority missing")

// ErrSessionWriteAuthorityStale is returned when the bound authority no longer
// matches the live lease generation (rebind, release, or controller replace).
// The in-memory transcript is preserved; callers rebind or fall back to an
// emergency isolated copy only on shutdown.
var ErrSessionWriteAuthorityStale = errors.New("session write authority stale")

// sessionWriteGeneration is a process-wide counter used by controllers when
// they issue a new authority generation. Each bind/rebind/replace must use a
// fresh value so any previous Controller's authority becomes stale immediately.
var sessionWriteGeneration atomic.Uint64

// NextSessionWriteGeneration allocates a new generation token. Controllers call
// this on create, restore, path change, and re-issue.
func NextSessionWriteGeneration() uint64 {
	return sessionWriteGeneration.Add(1)
}

// SessionWriteAuthority is an unforgeable in-memory write permit for one
// session path. Only a live SessionLease can issue it; the token carries the
// lease owner id and a controller-bound generation that is invalidated when the
// controller is replaced or rebound. Tokens never leave process memory and
// cannot be reconstructed from disk metadata.
type SessionWriteAuthority struct {
	path       string
	ownerID    uint64
	generation uint64
	lease      *SessionLease
}

// IssueWriteAuthority mints a path-bound authority for generation. The lease
// must still be held; a released lease returns a stale error so callers cannot
// mint after release.
func (l *SessionLease) IssueWriteAuthority(generation uint64) (*SessionWriteAuthority, error) {
	if l == nil {
		return nil, ErrSessionWriteAuthorityMissing
	}
	if generation == 0 {
		return nil, fmt.Errorf("%w: zero generation", ErrSessionWriteAuthorityMissing)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released || l.leaseLock == nil {
		return nil, ErrSessionWriteAuthorityStale
	}
	l.writeGeneration = generation
	return &SessionWriteAuthority{
		path:       l.path,
		ownerID:    l.ownerID,
		generation: generation,
		lease:      l,
	}, nil
}

// Path is the canonical session path this authority covers.
func (a *SessionWriteAuthority) Path() string {
	if a == nil {
		return ""
	}
	return a.path
}

// Generation is the controller-bound generation this authority was issued for.
func (a *SessionWriteAuthority) Generation() uint64 {
	if a == nil {
		return 0
	}
	return a.generation
}

// Valid reports whether the authority still matches a live lease for its path
// and generation. A nil authority is never valid.
func (a *SessionWriteAuthority) Valid() bool {
	if a == nil || a.lease == nil || a.generation == 0 {
		return false
	}
	a.lease.mu.Lock()
	defer a.lease.mu.Unlock()
	if a.lease.released || a.lease.leaseLock == nil {
		return false
	}
	if a.lease.path != a.path || a.lease.ownerID != a.ownerID || a.lease.writeGeneration != a.generation {
		return false
	}
	// Active-owner registry must still name this generation's lease. A reclaim
	// that replaced the owner id makes prior authorities stale even if the
	// lease object has not been released yet.
	owner, ok := sessionLeaseActiveOwners.Load(a.path)
	if !ok {
		return false
	}
	id, ok := owner.(uint64)
	return ok && id == a.ownerID
}

// Covers reports whether a is valid for path (canonical comparison).
func (a *SessionWriteAuthority) Covers(path string) bool {
	if !a.Valid() {
		return false
	}
	return a.path == canonicalSessionSavePath(path)
}

// BeginSave marks an in-flight save against the issuing lease so Release waits
// for the write cycle to finish. The returned release must run exactly once.
// A missing or stale authority returns a typed error and does not enter the
// diverged/recovery path.
func (a *SessionWriteAuthority) BeginSave(path string) (func(), error) {
	if a == nil {
		return nil, ErrSessionWriteAuthorityMissing
	}
	if a.lease == nil || a.generation == 0 {
		return nil, ErrSessionWriteAuthorityMissing
	}
	a.lease.mu.Lock()
	defer a.lease.mu.Unlock()
	if a.path != canonicalSessionSavePath(path) ||
		a.lease.released || a.lease.leaseLock == nil ||
		a.lease.path != a.path || a.lease.ownerID != a.ownerID ||
		a.lease.writeGeneration != a.generation {
		return nil, ErrSessionWriteAuthorityStale
	}
	owner, ok := sessionLeaseActiveOwners.Load(a.path)
	id, okID := owner.(uint64)
	if !ok || !okID || id != a.ownerID {
		return nil, ErrSessionWriteAuthorityStale
	}
	a.lease.activeSaves++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.lease.mu.Lock()
			if a.lease.activeSaves > 0 {
				a.lease.activeSaves--
			}
			if a.lease.activeSaves == 0 && a.lease.releaseWait != nil {
				// Wake any Release waiters that parked for in-flight saves.
				close(a.lease.releaseWait)
				a.lease.releaseWait = nil
			}
			a.lease.mu.Unlock()
		})
	}, nil
}

// bindWriteAuthority stores auth on the session. A nil auth clears the binding.
// The session only consults authority for ownership-sensitive decisions when a
// non-nil auth has been bound at least once (authRequired).
func (s *Session) BindWriteAuthority(auth *SessionWriteAuthority) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeAuth = auth
	if auth != nil {
		s.authRequired = true
	}
}

// RequireWriteAuthority permanently puts this Session on the production
// fail-closed path. Controllers call it before attempting lease issuance so a
// failed or interrupted bind cannot leave a persisted session writable through
// the legacy unbound test path.
func (s *Session) RequireWriteAuthority() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.authRequired = true
	s.mu.Unlock()
}

// WriteAuthorityRequired reports whether production admission and saves must
// present a live path-bound authority.
func (s *Session) WriteAuthorityRequired() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authRequired
}

// WriteAuthority returns the currently bound authority, if any.
func (s *Session) WriteAuthority() *SessionWriteAuthority {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.writeAuth
}

// ClearWriteAuthority drops the bound authority without clearing authRequired,
// so subsequent saves fail closed until a fresh authority is bound.
func (s *Session) ClearWriteAuthority() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeAuth = nil
}

// requireWriteAuthorityForSave enforces the production write path. Low-level
// unit tests that never bind an authority keep the legacy unbound path so they
// can exercise pure CAS mechanics. Once a controller has bound an authority,
// every save must present a live one for the target path.
func (s *Session) requireWriteAuthorityForSave(path string) (func(), error) {
	if s == nil {
		return nil, ErrSessionWriteAuthorityMissing
	}
	s.mu.RLock()
	auth := s.writeAuth
	required := s.authRequired
	s.mu.RUnlock()
	if !required {
		return func() {}, nil
	}
	if auth == nil {
		return nil, ErrSessionWriteAuthorityMissing
	}
	return auth.BeginSave(path)
}

// hasValidWriteAuthority reports whether the session currently holds a live
// authority covering path. Used by conflict classification for owned rewrite.
func (s *Session) hasValidWriteAuthority(path string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	auth := s.writeAuth
	s.mu.RUnlock()
	return auth.Covers(path)
}

// authorityErrorForPath returns a typed authority error when the session has
// bound (or required) authority that no longer covers path. A never-bound
// session returns nil so low-level CAS tests keep their existing conflict path.
func (s *Session) authorityErrorForPath(path string) error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	auth := s.writeAuth
	required := s.authRequired
	s.mu.RUnlock()
	if !required {
		return nil
	}
	if auth == nil {
		return ErrSessionWriteAuthorityMissing
	}
	if auth.Covers(path) {
		return nil
	}
	if strings.TrimSpace(auth.path) == "" || auth.generation == 0 {
		return ErrSessionWriteAuthorityMissing
	}
	return ErrSessionWriteAuthorityStale
}
