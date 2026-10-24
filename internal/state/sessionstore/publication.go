package sessionstore

import "sync"

// SessionPublication permits attachment publication inside the session save lock.
// Its methods refuse use after the owning callback returns.
type SessionPublication struct {
	mu     sync.Mutex
	path   string
	active bool
}

func withLockedPublication(path string, fn func(*SessionPublication) error) error {
	p := &SessionPublication{path: canonicalSessionSavePath(path), active: true}
	defer func() { p.mu.Lock(); p.active = false; p.mu.Unlock() }()
	return fn(p)
}

func WithSessionPublication(path string, fn func(*SessionPublication) error) error {
	return new(Session).withSessionSaveLocks(path, func() error {
		return withLockedPublication(path, fn)
	})
}

func (p *SessionPublication) BindExecutionEnvironment(binding ExecutionEnvironmentBinding, authority *SessionWriteAuthority) error {
	if p == nil {
		return ErrSessionWriteAuthorityMissing
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active {
		return ErrSessionWriteAuthorityStale
	}
	end, err := authority.BeginSave(p.path)
	if err != nil {
		return err
	}
	defer end()
	if err := bindExecutionEnvironmentLocked(p.path, binding); err != nil {
		return err
	}
	authority.environmentReservation.Store(true)
	return nil
}
