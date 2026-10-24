package control

import "reasonix/internal/state/sessionstore"

func (k *SessionLeaseKeeper) adoptSessionLease(owner *Controller, lease *sessionstore.SessionLease) error {
	k.mu.Lock()
	if k.controller != owner || lease == nil {
		k.mu.Unlock()
		return sessionstore.ErrSessionWriteAuthorityStale
	}
	old := k.lease
	k.lease = lease
	var retired chan struct{}
	if old != nil {
		retired = make(chan struct{})
		k.retired = append(k.retired, retired)
	}
	k.mu.Unlock()
	// A recovery transition may run inside an old authority-guarded save.
	if old != nil {
		go func() { old.Release(); close(retired) }()
	}
	return nil
}
