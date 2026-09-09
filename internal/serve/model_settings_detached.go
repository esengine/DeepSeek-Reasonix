package serve

import (
	"context"
	"fmt"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/sessioninbox"
)

// Background dispatch holds the same binding gate as foreground dispatch, then
// the owner's admission gate. The close watcher only takes the latter.
func (s *Server) beforeDetachedInboxDispatch(ctrl *control.Controller) (func(), error) {
	s.bindMu.Lock()
	s.detachedMu.Lock()
	var owner *detachedSession
	for _, d := range s.detached {
		if d.ctrl == ctrl && !d.retiring {
			owner = d
			break
		}
	}
	s.detachedMu.Unlock()
	if owner == nil {
		s.bindMu.Unlock()
		return nil, control.ErrInboxRuntimeUnpublished
	}
	owner.admissionMu.Lock()
	release := func() {
		owner.admissionMu.Unlock()
		s.bindMu.Unlock()
	}
	s.detachedMu.Lock()
	valid := s.detached[owner.path] == owner && !owner.retiring && owner.ctrl == ctrl
	s.detachedMu.Unlock()
	if !valid {
		release()
		return nil, control.ErrInboxRuntimeUnpublished
	}
	if controllerHasActiveRuntimeWork(ctrl) {
		release()
		return nil, control.ErrTurnRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := s.refreshModelSettingsOwnerLocked(ctx, modelSettingsRuntimeOwner{
		current:  func() control.SessionAPI { return owner.ctrl },
		settings: &owner.modelSettings, offerID: &owner.modelSettingsOfferID,
		apply: func(ctx context.Context, ref string) error { return s.rebuildDetachedModelSettings(ctx, owner, ref) },
	})
	cancel()
	if err != nil {
		release()
		return nil, err
	}
	if owner.ctrl != ctrl {
		replacement, _ := owner.ctrl.(*control.Controller)
		release()
		if replacement != nil {
			replacement.NotifyInboxRuntimeReady()
		}
		return nil, control.ErrInboxRuntimeUnpublished
	}
	return release, nil
}

// Keep a background owner alive through the gap between completion and FIFO
// admission. Paused/blocked work remains durable and may close normally.
func (s *Server) detachedHasPendingWork(d *detachedSession) bool {
	d.admissionMu.Lock()
	defer d.admissionMu.Unlock()
	if controllerHasActiveRuntimeWork(d.ctrl) {
		return true
	}
	if inbox, ok := d.ctrl.(control.Inbox); ok {
		snapshot := inbox.InboxSnapshot()
		if !snapshot.Paused {
			for _, item := range snapshot.Items {
				if item.State == sessioninbox.StateQueued {
					return true
				}
			}
		}
	}
	s.detachedMu.Lock()
	if s.detached[d.path] == d {
		d.retiring = true
	}
	s.detachedMu.Unlock()
	return false
}

func (s *Server) rebuildDetachedModelSettings(ctx context.Context, owner *detachedSession, ref string) error {
	old, ok := owner.ctrl.(*control.Controller)
	if !ok || controllerHasActiveRuntimeWork(old) {
		return fmt.Errorf("background runtime cannot apply model settings yet")
	}
	if err := old.Snapshot(); err != nil {
		return err
	}
	tag := newSessionTagSink(s.bc)
	tag.PrimePath(old.SessionPath())
	opts := owner.buildOptions
	opts.Model, opts.ModelSettings, opts.Sink = ref, owner.modelSettings, tag
	opts.SessionDir, opts.WorkspaceRoot = old.SessionDir(), old.WorkspaceRoot()
	opts.BeforeInboxDispatch = s.beforeInboxDispatch
	next, err := s.rebuildWithOptions(ctx, old, ref, opts, tag)
	if err != nil {
		return err
	}
	next.EnableInteractiveApproval()
	next.SetOnSessionRecovered(s.sessionRecoveryHandler(next, owner.keeper))
	if err := owner.keeper.BindControllerAuthority(next); err != nil {
		s.closeTaggedController(next)
		return err
	}
	if err := next.Snapshot(); err != nil {
		_ = owner.keeper.BindControllerAuthority(old)
		s.closeTaggedController(next)
		return err
	}
	s.detachedMu.Lock()
	owner.ctrl, owner.tag = next, tag
	s.detachedMu.Unlock()
	tag.Activate()
	old.Close()
	s.forgetSessionTag(old)
	return nil
}
