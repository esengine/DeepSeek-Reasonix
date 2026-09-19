package cli

import (
	"fmt"

	"reasonix/internal/control"
)

func cliControllerHasActiveRuntimeWork(ctrl control.SessionAPI) bool {
	if ctrl == nil {
		return false
	}
	status := ctrl.RuntimeStatus()
	return status.Running || status.PendingPrompt || status.BackgroundJobs > 0
}

// sessionLeaseResumeRefusal is the startup-time refusal for `reasonix
// [--resume|--continue]` and `reasonix run --resume/--continue`: it names the
// holder and offers the two ways out (close the holder, or continue in a
// duplicated session via --copy).
func sessionLeaseResumeRefusal(err error) string {
	return control.SessionInUseMessage(err) +
		"; close the other Reasonix window or process, or rerun with --copy to continue in a duplicated session"
}

// sessionLeaseHeldNotice is the in-TUI refusal for /resume and /switch, where
// exiting to rerun with --copy is not the natural move.
func sessionLeaseHeldNotice(err error) string {
	return control.SessionInUseMessage(err) + "; " + control.SessionLeaseCloseHint
}

// rebindSessionLease moves the chat TUI's session lease to path before the
// controller binds it for writing. A nil keeper (tests, persistence disabled)
// gates nothing. On error the keeper still guards the previous session.
func (m *chatTUI) rebindSessionLease(path string) error {
	if m.leases == nil {
		return nil
	}
	handled := false
	var err error
	if m.takeover != nil {
		handled, err = m.takeover.RebindAway(path)
	}
	if err != nil {
		return err
	}
	if !handled {
		err = m.leases.Rebind(path)
	}
	if err != nil {
		return err
	}
	return bindChatTUIAuthority(m)
}

// restoreSessionLease re-points the lease at the controller's current session
// after a switch attempt moved it but the switch itself then failed.
// Best-effort: the old lease was released during the rebind, so in the
// (unlikely) case another runtime grabbed it in between this stays silent and
// the next write surfaces the conflict.
func (m *chatTUI) restoreSessionLease() {
	if m.leases == nil {
		return
	}
	_ = m.leases.Rebind(m.ctrl.SessionPath())
	_ = bindChatTUIAuthority(m)
}

// followSessionLease re-points the TUI's session lease at the controller's
// current session file after an operation that rotated it to a fresh path
// (/new, /clear, /branch, fork). A fresh path cannot be held by anyone else,
// so failure is theoretical — but never silent.
func (m *chatTUI) followSessionLease() {
	if m.leases == nil {
		return
	}
	if err := m.leases.Rebind(m.ctrl.SessionPath()); err != nil {
		m.notice(sessionLeaseHeldNotice(err))
		return
	}
	if err := bindChatTUIAuthority(m); err != nil {
		m.notice(fmt.Sprintf("session write authority: %v", err))
	}
}

// cliSessionRecoveredHandler moves the single-session CLI lease during the
// controller's recovery commit. The callback runs before Controller changes its
// session path, closing the unguarded interval that event-driven follow-up
// calls left after ordinary turn-end and mid-turn autosaves.
func cliSessionRecoveredHandler(leases *control.SessionLeaseKeeper) func(control.SessionRecoveryInfo) error {
	return func(info control.SessionRecoveryInfo) error {
		if err := leases.HandleSessionRecovered(info); err != nil {
			return err
		}
		// Controller pointer is not available here; TUI followSessionLease and
		// headless post-Rebind bind authority. Recovery commit rebinds the lease
		// first; the next Snapshot path match is ensured once Bind runs.
		return nil
	}
}

func rebindCLIControllerAuthority(leases *control.SessionLeaseKeeper, ctrl *control.Controller) error {
	if leases == nil || ctrl == nil {
		return nil
	}
	if err := leases.Rebind(ctrl.SessionPath()); err != nil {
		return err
	}
	return leases.BindControllerAuthority(ctrl)
}

func bindChatTUIAuthority(m *chatTUI) error {
	if m == nil || m.leases == nil {
		return nil
	}
	c, ok := m.ctrl.(*control.Controller)
	if !ok || c == nil {
		return nil
	}
	return m.leases.BindControllerAuthority(c)
}
