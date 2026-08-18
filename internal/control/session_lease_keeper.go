package control

import (
	"errors"
	"fmt"
	"log/slog"
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
	mu         sync.Mutex
	lease      *agent.SessionLease
	controller *Controller
	retired    []<-chan struct{}
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

// HandleSessionRecovered moves the single-session frontend lease before a
// controller commits to a recovery branch. It is suitable for
// Options.OnSessionRecovered in CLI chat/run/serve surfaces. Rebind acquires the
// recovery path before releasing the original lease, so a failed handoff keeps
// the previous session protected.
func (k *SessionLeaseKeeper) HandleSessionRecovered(info SessionRecoveryInfo) error {
	recoveryPath := strings.TrimSpace(info.RecoveryPath)
	if k == nil || recoveryPath == "" {
		return nil
	}
	k.mu.Lock()
	if k.lease != nil && k.lease.Path() == agent.CanonicalSessionPath(recoveryPath) {
		k.mu.Unlock()
		return nil
	}
	lease, err := agent.TryAcquireSessionLease(recoveryPath)
	if err == nil && k.controller != nil {
		err = k.controller.BindSessionWriteAuthority(lease)
	}
	if err != nil {
		if lease != nil {
			lease.Release()
		}
		k.mu.Unlock()
		if errors.Is(err, agent.ErrSessionLeaseHeld) {
			return fmt.Errorf("bind recovery session: %s; %s",
				SessionInUseMessage(err), SessionLeaseCloseHint)
		}
		// The detailed error can contain a machine-local path. Keep it in
		// diagnostics and return path-free text to every frontend.
		slog.Error("control: bind recovery session lease", "err", err)
		return fmt.Errorf("bind recovery session: unable to secure recovered transcript")
	}
	old := k.lease
	k.lease = lease
	var retired chan struct{}
	if old != nil {
		retired = make(chan struct{})
		k.retired = append(k.retired, retired)
	}
	k.mu.Unlock()
	// Recovery callbacks run inside the authority-guarded save that still owns
	// old. Releasing synchronously here would wait on that same save forever.
	// Retirement is bounded to one goroutine per committed path handoff.
	if old != nil {
		go func() {
			old.Release()
			close(retired)
		}()
	}
	return nil
}

// HandleSessionTransition acquires and binds an intentional path-change target
// before the controller swaps Sessions. Acquisition is failure-atomic: the old
// lease remains held unless the target lease and candidate authority are ready.
func (k *SessionLeaseKeeper) HandleSessionTransition(info SessionTransitionInfo) error {
	targetPath := strings.TrimSpace(info.TargetPath)
	if k == nil || targetPath == "" {
		return nil
	}
	k.mu.Lock()
	canonical := agent.CanonicalSessionPath(targetPath)
	if k.lease != nil && k.lease.Path() == canonical {
		err := info.BindWriteAuthority(k.lease)
		k.mu.Unlock()
		return err
	}
	lease, err := agent.TryAcquireSessionLease(targetPath)
	if err == nil {
		err = info.BindWriteAuthority(lease)
	}
	if err != nil {
		if lease != nil {
			lease.Release()
		}
		k.mu.Unlock()
		if errors.Is(err, agent.ErrSessionLeaseHeld) {
			return fmt.Errorf("bind target session: %s; %s",
				SessionInUseMessage(err), SessionLeaseCloseHint)
		}
		slog.Error("control: bind target session lease", "reason", info.Reason, "err", err)
		return fmt.Errorf("bind target session: unable to secure transcript")
	}
	old := k.lease
	k.lease = lease
	k.mu.Unlock()
	if old != nil {
		old.Release()
	}
	return nil
}

// Release drops the held lease, if any. Idempotent; call it on frontend
// teardown after the controller has finished its final writes.
func (k *SessionLeaseKeeper) Release() {
	if k == nil {
		return
	}
	k.mu.Lock()
	k.releaseLocked()
	retired := append([]<-chan struct{}(nil), k.retired...)
	k.mu.Unlock()
	for _, done := range retired {
		<-done
	}
}

// WaitForRetiredLeases waits until the most recent recovery handoff has
// released its outgoing lease. Runtime paths do not need to call it; tests and
// shutdown use it when they require deterministic cleanup observation.
func (k *SessionLeaseKeeper) WaitForRetiredLeases() {
	if k == nil {
		return
	}
	k.mu.Lock()
	retired := append([]<-chan struct{}(nil), k.retired...)
	k.mu.Unlock()
	for _, done := range retired {
		<-done
	}
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

// Lease returns the held lease for authority issuance. Callers must not
// Release it; use Release/Rebind on the keeper instead.
func (k *SessionLeaseKeeper) Lease() *agent.SessionLease {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.lease
}

// BindControllerAuthority issues a fresh write authority from the held lease
// onto c. Safe no-op when the keeper holds nothing.
func (k *SessionLeaseKeeper) BindControllerAuthority(c *Controller) error {
	if k == nil || c == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := c.BindSessionWriteAuthority(k.lease); err != nil {
		return err
	}
	k.controller = c
	c.SetOnSessionTransition(k.HandleSessionTransition)
	return nil
}

func (k *SessionLeaseKeeper) releaseLocked() {
	if k.lease != nil {
		k.lease.Release()
		k.lease = nil
	}
	if k.controller != nil {
		k.controller.SetOnSessionTransition(nil)
		_ = k.controller.BindSessionWriteAuthority(nil)
		k.controller = nil
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
