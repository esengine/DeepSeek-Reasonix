package acp

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

// bindACPWriteAuthority issues a generation-bound write authority when ctrl is
// a concrete *control.Controller. Test fakes without the method stay unbound.
func bindACPWriteAuthority(ctrl acpController, lease *agent.SessionLease) error {
	c, ok := ctrl.(*control.Controller)
	if !ok || c == nil {
		return nil
	}
	return c.BindSessionWriteAuthority(lease)
}

func bindACPWriteAuthorityOrClose(ctrl acpController, lease *agent.SessionLease) error {
	if err := bindACPWriteAuthority(ctrl, lease); err != nil {
		if lease != nil {
			lease.Release()
		}
		ctrl.Close()
		return err
	}
	return nil
}

func (s *service) bindSessionPathHandlers(id string, params *SessionParams) {
	params.OnSessionRecovered = s.sessionRecoveredHandler(id)
	params.OnSessionTransition = s.sessionTransitionHandler(id)
}

func resumeACPControllerForWrite(ctrl acpController, loaded *agent.Session, path string, lease *agent.SessionLease) error {
	ctrl.Resume(loaded, path)
	return bindACPWriteAuthorityOrClose(ctrl, lease)
}

func snapshotACPController(sess *acpSession, ctrl acpController) error {
	err := ctrl.Snapshot()
	sess.waitForRetiredSessionLeases()
	return err
}

func (s *service) prepareACPReplacementAuthority(sess *acpSession, next *control.Controller, current acpController, path, snapshotAction string) error {
	next.SetOnSessionRecovered(s.sessionRecoveredHandlerFor(sess.id, next))
	next.SetOnSessionTransition(s.sessionTransitionHandler(sess.id))
	sess.mu.Lock()
	lease := sess.lease
	sess.mu.Unlock()
	if lease != nil {
		if err := bindACPWriteAuthority(next, lease); err != nil {
			return fmt.Errorf("bind replacement session authority")
		}
	}
	if path == "" {
		return nil
	}
	if err := snapshotACPController(sess, next); err != nil {
		_ = bindACPWriteAuthority(current, lease)
		return fmt.Errorf("%s: %w", snapshotAction, err)
	}
	return nil
}

// sessionTransitionHandler binds an unpublished branch/switch Session to its
// target lease before the controller publishes it. ACP metadata changes only
// after both acquisition and authority binding succeed.
func (s *service) sessionTransitionHandler(id string) func(control.SessionTransitionInfo) error {
	return func(info control.SessionTransitionInfo) error {
		targetPath := strings.TrimSpace(info.TargetPath)
		if targetPath == "" {
			return nil
		}
		sess := s.session(id)
		if sess == nil {
			return fmt.Errorf("bind target session: session is unavailable")
		}
		lease, err := agent.TryAcquireSessionLease(targetPath)
		if err != nil {
			if errors.Is(err, agent.ErrSessionLeaseHeld) {
				return fmt.Errorf("bind target session: %s; %s",
					control.SessionInUseMessage(err), control.SessionLeaseCloseHint)
			}
			return fmt.Errorf("bind target session: %w", err)
		}
		sess.mu.Lock()
		if sess.deleted {
			sess.mu.Unlock()
			lease.Release()
			return fmt.Errorf("bind target session: session is deleted")
		}
		if err := info.BindWriteAuthority(lease); err != nil {
			sess.mu.Unlock()
			lease.Release()
			return fmt.Errorf("bind target session authority: %w", err)
		}
		old := sess.lease
		sess.lease = lease
		sess.transcript = targetPath
		meta := sess.metaLocked()
		sess.mu.Unlock()
		sess.retireSessionLease(old)
		_ = saveACPMeta(targetPath, meta)
		s.persistACPTranscriptRedirect(id, targetPath, meta)
		return nil
	}
}

// persistACPTranscriptRedirect keeps restart-time id lookup attached to the
// transcript an intentional transition or conflict recovery selected. The
// active transcript owns the full metadata; the id-keyed sidecar is only a
// single-hop redirect when the paths differ.
func (s *service) persistACPTranscriptRedirect(id, activePath string, meta acpSessionMeta) {
	dir := s.sessionDir()
	if dir == "" {
		return
	}
	idPath := transcriptPath(dir, id)
	if idPath == activePath {
		return
	}
	idMeta, _, err := loadACPMeta(idPath)
	if err != nil {
		slog.Warn("acp: load id-keyed meta for transcript redirect", "err", err)
		idMeta = acpSessionMeta{}
	}
	if idMeta.SessionID == "" {
		idMeta.SessionID = id
	}
	if idMeta.Cwd == "" {
		idMeta.Cwd = meta.Cwd
	}
	if idMeta.CreatedAt.IsZero() {
		idMeta.CreatedAt = meta.CreatedAt
	}
	idMeta.ActiveTranscript = filepath.Base(activePath)
	if err := saveACPMeta(idPath, idMeta); err != nil {
		slog.Warn("acp: save transcript redirect", "err", err)
	}
}

// sessionRecoveredHandler follows a conflict recovery at commit time so ACP
// metadata, transcript lookup, and the write lease all point at one file.
func (s *service) sessionRecoveredHandler(id string) func(control.SessionRecoveryInfo) error {
	return s.sessionRecoveredHandlerFor(id, nil)
}

func (s *service) sessionRecoveredHandlerFor(id string, owner acpController) func(control.SessionRecoveryInfo) error {
	return func(info control.SessionRecoveryInfo) error {
		recoveryPath := strings.TrimSpace(info.RecoveryPath)
		if recoveryPath == "" {
			return nil
		}
		sess := s.session(id)
		if sess == nil {
			return nil
		}
		lease, err := agent.TryAcquireSessionLease(recoveryPath)
		if err != nil {
			if errors.Is(err, agent.ErrSessionLeaseHeld) {
				return fmt.Errorf("bind recovery session: %s; %s",
					control.SessionInUseMessage(err), control.SessionLeaseCloseHint)
			}
			return fmt.Errorf("bind recovery session: %w", err)
		}
		sess.mu.Lock()
		if sess.deleted {
			sess.mu.Unlock()
			lease.Release()
			return fmt.Errorf("bind recovery session: session is deleted")
		}
		old := sess.lease
		ctrl := owner
		if ctrl == nil {
			ctrl = sess.ctrl
		}
		if err := bindACPWriteAuthority(ctrl, lease); err != nil {
			if old != nil {
				_ = bindACPWriteAuthority(ctrl, old)
			}
			sess.mu.Unlock()
			lease.Release()
			return fmt.Errorf("bind recovery session: unable to bind recovered transcript authority")
		}
		sess.lease = lease
		sess.transcript = recoveryPath
		meta := sess.metaLocked()
		sess.mu.Unlock()
		sess.retireSessionLease(old)
		_ = saveACPMeta(recoveryPath, meta)
		s.persistACPTranscriptRedirect(id, recoveryPath, meta)
		return nil
	}
}
