package acp

import (
	"context"

	"reasonix/internal/session/control"
)

func (s *service) bindControllerIdle(sess *acpSession) {
	if sess == nil {
		return
	}
	ctrl, ok := sess.currentCtrl().(*control.Controller)
	if !ok {
		return
	}
	ctrl.SetIdleObserver(func() {
		sess.mu.Lock()
		pending := !sess.deleted && sess.ctrl == ctrl && !sess.running && (len(sess.pendingConfig) > 0 || sess.pendingReload)
		sess.mu.Unlock()
		if pending {
			go s.drainControllerIdle(sess, ctrl)
		}
	})
}

func (s *service) drainControllerIdle(sess *acpSession, owner *control.Controller) {
	if s.session(sess.id) != sess || sess.currentCtrl() != owner {
		return
	}
	ctx := context.Background()
	s.emitModeDrift(sess)
	s.emitToolApprovalDrift(ctx, sess)
	s.reportPendingSessionConfigError(ctx, sess, s.applyPendingSessionConfig(ctx, sess), "after controller-owned turn")
	s.drainPendingReload(ctx, sess)
}
