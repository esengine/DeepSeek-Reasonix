package control

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/agent"
)

// SessionLeaseKeeper owns at most one session lease on behalf of a frontend
// that binds session files for writing (the CLI chat/run commands, `reasonix
// serve`, one ACP session). Desktop tabs keep their own per-tab lease
// management; this keeper is the equivalent for the single-session surfaces:
// it follows the active session path across resumes, forks, and fresh-session
// rotations, holding exactly one lease at a time.
//
// The zero value is not ready for use; construct with NewSessionLeaseKeeper.
type SessionLeaseKeeper struct {
	mu    sync.Mutex
	lease *agent.SessionLease
}

func NewSessionLeaseKeeper() *SessionLeaseKeeper {
	return &SessionLeaseKeeper{}
}

// Rebind points the keeper at path: it acquires path's session lease and only
// then releases the previously held one, so the outgoing session stays
// protected until the new one is secured. Rebinding to the path already held
// is a no-op; an empty path (session persistence disabled) just releases.
// On failure the keeper is unchanged — the caller still holds its previous
// lease and must not bind path for writing. A held path surfaces as an error
// wrapping agent.ErrSessionLeaseHeld; format it with SessionInUseMessage.
func (k *SessionLeaseKeeper) Rebind(path string) error {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if strings.TrimSpace(path) == "" {
		k.releaseLocked()
		return nil
	}
	if k.lease != nil && k.lease.Path() == agent.CanonicalSessionPath(path) {
		return nil
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		return err
	}
	k.releaseLocked()
	k.lease = lease
	return nil
}

// Release drops the held lease, if any. Idempotent; call it on frontend
// teardown after the controller has finished its final writes.
func (k *SessionLeaseKeeper) Release() {
	if k == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.releaseLocked()
}

// HeldPath reports the canonical session path the keeper currently guards,
// or "" when it holds nothing.
func (k *SessionLeaseKeeper) HeldPath() string {
	if k == nil {
		return ""
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.lease == nil {
		return ""
	}
	return k.lease.Path()
}

func (k *SessionLeaseKeeper) releaseLocked() {
	if k.lease != nil {
		k.lease.Release()
		k.lease = nil
	}
}

// SessionLeaseCloseHint is the universal way out of a lease refusal, appended
// by surfaces that have no copy escape hatch (in-TUI switches, serve, ACP).
const SessionLeaseCloseHint = "close the other Reasonix window or process first"

// SessionInUseMessage renders a lease-acquisition failure as the shared
// operator-facing "who is holding this" line used by the CLI, serve, and ACP.
// It names the holder from the lease info when available and degrades to a
// generic line otherwise. The session file path is deliberately omitted — the
// caller already knows which session it asked for.
func SessionInUseMessage(err error) string {
	const fallback = "this session is in use by another Reasonix window or process"
	var leaseErr *agent.SessionLeaseError
	if !errors.As(err, &leaseErr) || leaseErr == nil || leaseErr.Info == nil || leaseErr.Info.PID <= 0 {
		return fallback
	}
	info := leaseErr.Info
	var b strings.Builder
	fmt.Fprintf(&b, "this session is in use by another Reasonix process (pid %d", info.PID)
	if host := strings.TrimSpace(info.Hostname); host != "" {
		b.WriteString(" on " + host)
	}
	if !info.AcquiredAt.IsZero() {
		b.WriteString(", since " + info.AcquiredAt.Local().Format("15:04"))
	}
	b.WriteString(")")
	return b.String()
}
