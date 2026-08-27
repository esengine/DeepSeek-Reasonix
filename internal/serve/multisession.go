package serve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// sessionTagSink stamps every event from one controller with that
// controller's current session path. This lets one Serve process keep several
// turns alive without sending a background session's frames to the foreground
// browser.
type sessionTagSink struct {
	bc      *Broadcaster
	mu      sync.Mutex
	path    string
	active  bool
	pending []event.Event
}

func newSessionTagSink(bc *Broadcaster) *sessionTagSink {
	return &sessionTagSink{bc: bc}
}

// SessionTagSink is exported for the CLI, which builds Serve's initial
// controller before the Server exists.
type SessionTagSink = sessionTagSink

func NewSessionTagSink(bc *Broadcaster) *SessionTagSink {
	return newSessionTagSink(bc)
}

func (s *sessionTagSink) SetPath(path string) {
	s.mu.Lock()
	s.path = canonicalSessionPath(path)
	s.activateLocked()
	s.mu.Unlock()
}

// PrimePath assigns a replacement controller's route without publishing boot
// events. Activate is called only after the controller swap fully commits.
func (s *sessionTagSink) PrimePath(path string) {
	s.mu.Lock()
	s.path = canonicalSessionPath(path)
	s.mu.Unlock()
}

// BufferPath retags synchronous in-place Resume events but withholds them until
// Serve publishes the matching foreground route. Unlike PrimePath, it also
// pauses a sink that was already active for the previous session.
func (s *sessionTagSink) BufferPath(path string) {
	s.mu.Lock()
	s.path = canonicalSessionPath(path)
	s.active = false
	s.mu.Unlock()
}

func canonicalSessionPath(path string) string {
	if path != "" {
		return agent.CanonicalSessionPath(path)
	}
	return ""
}

func (s *sessionTagSink) Activate() {
	s.mu.Lock()
	s.activateLocked()
	s.mu.Unlock()
}

func (s *sessionTagSink) activateLocked() {
	if s.active {
		return
	}
	s.active = true
	for _, e := range s.pending {
		if s.path != "" {
			e.SessionPath = s.path
		}
		s.bc.Emit(e)
	}
	s.pending = nil
}

func (s *sessionTagSink) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

func (s *sessionTagSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		s.pending = append(s.pending, e)
		return
	}
	if s.path != "" {
		e.SessionPath = s.path
	}
	s.bc.Emit(e)
}

type detachedSession struct {
	path     string
	ctrl     control.SessionAPI
	keeper   *control.SessionLeaseKeeper
	tag      *sessionTagSink
	retiring bool // guarded by Server.detachedMu; blocks reattach during Close
	force    chan struct{}
	reattach chan struct{}
	done     chan struct{}
}

// RegisterSessionTag associates a controller built outside Server with its
// tagging sink. In-place /new and /resume operations can then advance the tag.
func (s *Server) RegisterSessionTag(ctrl *control.Controller, tag *sessionTagSink) {
	if ctrl == nil || tag == nil {
		return
	}
	s.tagsMu.Lock()
	if s.tags == nil {
		s.tags = map[*control.Controller]*sessionTagSink{}
	}
	s.tags[ctrl] = tag
	s.tagsMu.Unlock()
}

func (s *Server) tagFor(ctrl *control.Controller) *sessionTagSink {
	if ctrl == nil {
		return nil
	}
	s.tagsMu.Lock()
	defer s.tagsMu.Unlock()
	return s.tags[ctrl]
}

func (s *Server) forgetSessionTag(ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	s.tagsMu.Lock()
	delete(s.tags, ctrl)
	s.tagsMu.Unlock()
	s.setControllerLeaseOwner(ctrl, nil)
}

func (s *Server) closeTaggedController(ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	ctrl.Close()
	s.forgetSessionTag(ctrl)
}

func (s *Server) setControllerPath(ctrl *control.Controller, path string) {
	if path != "" {
		path = agent.CanonicalSessionPath(path)
	}
	if tag := s.tagFor(ctrl); tag != nil {
		tag.SetPath(path)
	}
	s.bc.SetCurrentSession(path)
}

// buildTagged creates a controller whose frames are session-tagged. The legacy
// two-argument test builder remains supported; tests that need to assert the
// complete boot contract can inject buildControllerWithOptions.
func (s *Server) buildTagged(ctx context.Context, ref string, inheritTemp bool) (*control.Controller, *sessionTagSink, error) {
	tag := newSessionTagSink(s.bc)
	opts := s.buildOptions
	opts.Model = ref
	opts.Sink = tag
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	opts.StatsSource = "serve"
	if cur, ok := s.ctl().(*control.Controller); ok && cur != nil {
		opts.SessionDir = cur.SessionDir()
		opts.WorkspaceRoot = cur.WorkspaceRoot()
		if inheritTemp {
			opts.SessionTemp = cur.SessionTemp()
		}
	}

	var (
		ctrl *control.Controller
		err  error
	)
	switch {
	case s.buildControllerWithOptions != nil:
		ctrl, err = s.buildControllerWithOptions(ctx, ref, opts)
	case s.buildController != nil:
		ctrl, err = s.buildController(ctx, ref)
	default:
		ctrl, err = boot.Build(ctx, opts)
	}
	if err != nil {
		return nil, nil, err
	}
	s.RegisterSessionTag(ctrl, tag)
	slog.Info("serve: controller built", "model", ref, "sessionDir", opts.SessionDir)
	return ctrl, tag, nil
}

func (s *Server) detachedBusy(path string) bool {
	path = agent.CanonicalSessionPath(path)
	s.detachedMu.Lock()
	defer s.detachedMu.Unlock()
	_, ok := s.detached[path]
	return ok
}

// takeDetached transfers ownership from the close-on-idle watcher back to the
// request goroutine. Waiting for done is essential: without the acknowledgement
// the watcher can close an idle controller just after it is re-attached.
func (s *Server) takeDetached(path string) *detachedSession {
	path = agent.CanonicalSessionPath(path)
	s.detachedMu.Lock()
	d := s.detached[path]
	if d != nil && !d.retiring {
		delete(s.detached, path)
	} else {
		d = nil
	}
	s.detachedMu.Unlock()
	if d == nil {
		return nil
	}
	close(d.reattach)
	<-d.done
	return d
}

func (s *Server) registerDetached(ctrl control.SessionAPI, keeper *control.SessionLeaseKeeper, tag *sessionTagSink) (*detachedSession, error) {
	if ctrl == nil {
		return nil, fmt.Errorf("cannot detach a nil controller")
	}
	if tag == nil {
		if concrete, ok := ctrl.(*control.Controller); ok {
			tag = s.tagFor(concrete)
		}
	}
	if tag == nil {
		return nil, errSessionTagUnavailable
	}
	if keeper != nil {
		if concrete, ok := ctrl.(*control.Controller); ok {
			concrete.SetOnSessionRecovered(s.sessionRecoveryHandler(concrete, keeper))
		}
	}
	if registerDetachedHookForTest != nil {
		registerDetachedHookForTest()
	}
	d := &detachedSession{
		ctrl: ctrl, keeper: keeper, tag: tag,
		force: make(chan struct{}), reattach: make(chan struct{}), done: make(chan struct{}),
	}
	s.detachedMu.Lock()
	path := agent.CanonicalSessionPath(ctrl.SessionPath())
	if path == "" {
		s.detachedMu.Unlock()
		return nil, fmt.Errorf("cannot detach a session without a path")
	}
	d.path = path
	if s.detached == nil {
		s.detached = map[string]*detachedSession{}
	}
	if _, exists := s.detached[path]; exists {
		s.detachedMu.Unlock()
		return nil, fmt.Errorf("session is already running in the background")
	}
	s.detached[path] = d
	s.detachedMu.Unlock()
	slog.Info("serve: session detached", "session", path, "running", controllerHasActiveRuntimeWork(ctrl))
	go s.watchDetached(d)
	return d, nil
}

func (s *Server) watchDetached(d *detachedSession) {
	interval := 200 * time.Millisecond
	forced := false
	for controllerHasActiveRuntimeWork(d.ctrl) && !forced {
		timer := time.NewTimer(interval)
		select {
		case <-d.reattach:
			if !timer.Stop() {
				<-timer.C
			}
			close(d.done)
			return
		case <-d.force:
			if !timer.Stop() {
				<-timer.C
			}
			forced = true
		case <-timer.C:
			if interval < 2*time.Second {
				interval *= 2
			}
		}
	}

	// Claim close ownership only while the registry still points at d. Keep the
	// retiring entry visible until Close and lease release finish so deletion
	// cannot race final controller writes. takeDetached refuses retiring entries.
	s.detachedMu.Lock()
	owns := s.detached[d.path] == d
	if owns {
		d.retiring = true
	}
	s.detachedMu.Unlock()
	if !owns {
		close(d.done)
		return
	}
	d.ctrl.Close()
	if d.keeper != nil {
		d.keeper.Release()
	}
	if concrete, ok := d.ctrl.(*control.Controller); ok {
		s.forgetSessionTag(concrete)
	}
	s.detachedMu.Lock()
	closedPath := d.path
	if s.detached[d.path] == d {
		delete(s.detached, d.path)
	}
	s.detachedMu.Unlock()
	slog.Info("serve: background session closed", "session", closedPath, "forced", forced)
	close(d.done)
}

func (s *Server) WaitForDetachedIdle() {
	s.detachedMu.Lock()
	detached := make([]*detachedSession, 0, len(s.detached))
	for _, d := range s.detached {
		detached = append(detached, d)
	}
	s.detachedMu.Unlock()
	for _, d := range detached {
		<-d.done
	}
}

func (s *Server) CloseBackground() {
	s.detachedMu.Lock()
	detached := make([]*detachedSession, 0, len(s.detached))
	for _, d := range s.detached {
		detached = append(detached, d)
	}
	s.detachedMu.Unlock()
	for _, d := range detached {
		select {
		case <-d.force:
		default:
			close(d.force)
		}
	}
	for _, d := range detached {
		<-d.done
	}
}

// Close stops every controller the server owns, including a foreground
// replacement created after the CLI's original controller was constructed.
func (s *Server) Close() {
	s.CloseBackground()
	cur := s.ctl()
	cur.Close()
	if concrete, ok := cur.(*control.Controller); ok {
		s.forgetSessionTag(concrete)
	}
}

// busyDetach publishes a fresh controller before demoting a busy controller.
// Every failure before publication restores the original lease ownership.
func (s *Server) busyDetach(ctx context.Context, cur *control.Controller, targetPath string, loadTarget func(*control.Controller) error) error {
	if s.tagFor(cur) == nil {
		return errSessionTagUnavailable
	}
	newCtrl, tag, err := s.buildTagged(ctx, currentModelRef(cur), false)
	if err != nil {
		return err
	}
	if targetPath == "" {
		newCtrl.EnsureSessionPath()
		targetPath = newCtrl.SessionPath()
	}
	targetPath = agent.CanonicalSessionPath(targetPath)
	if targetPath == "" {
		s.closeTaggedController(newCtrl)
		return fmt.Errorf("replacement session has no path")
	}

	demoted, err := s.leases.RebindDetaching(targetPath)
	if err != nil {
		s.closeTaggedController(newCtrl)
		return err
	}
	if loadTarget != nil {
		if err := loadTarget(newCtrl); err != nil {
			s.closeTaggedController(newCtrl)
			s.rollbackDetach(demoted, cur)
			return err
		}
	}
	tag.PrimePath(targetPath)
	newCtrl.EnableInteractiveApproval()
	newCtrl.SetOnSessionRecovered(s.sessionRecoveryHandler(newCtrl, s.leases))
	if s.leases != nil {
		if err := s.leases.BindControllerAuthority(newCtrl); err != nil {
			s.closeTaggedController(newCtrl)
			s.rollbackDetach(demoted, cur)
			return err
		}
	}

	if !s.publishControllerSwap(cur, newCtrl, targetPath) {
		s.closeTaggedController(newCtrl)
		s.rollbackDetach(demoted, cur)
		return errReplacedDuringBind
	}

	if _, err := s.registerDetached(cur, demoted, nil); err != nil {
		// bindMu prevents another foreground swap here. Roll publication back so
		// a registry failure cannot strand a running controller.
		_ = s.publishControllerSwap(newCtrl, cur, cur.SessionPath())
		s.closeTaggedController(newCtrl)
		s.rollbackDetach(demoted, cur)
		return err
	}
	tag.Activate()
	s.bc.ResetSessionPath(targetPath)
	return nil
}

func (s *Server) announceSessionChanged(path string, reset bool) {
	s.bc.Emit(event.Event{Kind: event.SessionChanged, SessionPath: path, SessionReset: reset})
}

var errReplacedDuringBind = &replacedDuringBindError{}
var errSessionTagUnavailable = errors.New("multi-session switching requires a session-tagged Serve controller")

type replacedDuringBindError struct{}

func (*replacedDuringBindError) Error() string { return "session changed during switch" }

func (s *Server) rollbackDetach(demoted *control.SessionLeaseKeeper, ctrl *control.Controller) {
	if demoted != nil && s.leases != nil {
		s.leases.Adopt(demoted)
	}
	if ctrl != nil {
		ctrl.SetOnSessionRecovered(s.sessionRecoveryHandler(ctrl, s.leases))
	}
}

func (s *Server) resumeActiveSession(w http.ResponseWriter, r *http.Request, cur control.SessionAPI, realPath string) bool {
	if agent.CanonicalSessionPath(cur.SessionPath()) == agent.CanonicalSessionPath(realPath) {
		s.bc.SetCurrentSession(realPath)
		s.announceSessionChanged(realPath, false)
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	if detached := s.takeDetached(realPath); detached != nil {
		if err := s.reattachDetached(cur, detached); err != nil {
			s.renderBindError(w, err)
			return true
		}
		s.announceSessionChanged(realPath, false)
		w.WriteHeader(http.StatusNoContent)
		s.replayPendingPromptsBroadcast()
		return true
	}
	if s.detachedBusy(realPath) {
		http.Error(w, "session is finishing background teardown; retry shortly", http.StatusConflict)
		return true
	}
	if !controllerHasActiveRuntimeWork(cur) {
		return false
	}
	curCtrl, ok := cur.(*control.Controller)
	if !ok {
		http.Error(w, "cannot switch session while active work or background jobs are running", http.StatusConflict)
		return true
	}
	err := s.busyDetach(r.Context(), curCtrl, realPath, func(next *control.Controller) error {
		loaded, err := agent.LoadSession(realPath)
		if err == nil {
			next.Resume(loaded, realPath)
		}
		return err
	})
	if err != nil {
		s.renderBindError(w, err)
		return true
	}
	s.announceSessionChanged(realPath, false)
	w.WriteHeader(http.StatusNoContent)
	s.replayPendingPromptsBroadcast()
	return true
}

// reattachDetached promotes a controller owned by the background registry.
// bindMu is held, so publication and lease ownership move as one transaction.
func (s *Server) reattachDetached(cur control.SessionAPI, detached *detachedSession) error {
	curCtrl, _ := cur.(*control.Controller)
	demoted := s.leases.Split()
	s.leases.Adopt(detached.keeper)
	detached.keeper = nil
	if detached.tag != nil {
		detached.tag.SetPath(detached.ctrl.SessionPath())
	}
	if concrete, ok := detached.ctrl.(*control.Controller); ok {
		concrete.SetOnSessionRecovered(s.sessionRecoveryHandler(concrete, s.leases))
	}
	if !s.publishControllerSwap(cur, detached.ctrl, detached.ctrl.SessionPath()) {
		detached.keeper = s.leases.Split()
		s.leases.Adopt(demoted)
		if curCtrl != nil {
			curCtrl.SetOnSessionRecovered(s.sessionRecoveryHandler(curCtrl, s.leases))
		}
		_, _ = s.registerDetached(detached.ctrl, detached.keeper, detached.tag)
		return errReplacedDuringBind
	}
	if controllerHasActiveRuntimeWork(cur) {
		if curCtrl == nil {
			s.restoreReattach(cur, detached, demoted)
			return fmt.Errorf("cannot switch session while active work or background jobs are running")
		}
		if _, err := s.registerDetached(curCtrl, demoted, nil); err != nil {
			s.restoreReattach(cur, detached, demoted)
			return err
		}
	} else {
		if err := cur.Snapshot(); err != nil {
			slog.Warn("serve: snapshot before background reattach", "err", err)
		}
		cur.Close()
		if curCtrl != nil {
			s.forgetSessionTag(curCtrl)
		}
		if demoted != nil {
			demoted.Release()
		}
	}
	slog.Info("serve: background session re-attached", "session", detached.path, "running", controllerHasActiveRuntimeWork(detached.ctrl))
	return nil
}

func (s *Server) restoreReattach(cur control.SessionAPI, detached *detachedSession, demoted *control.SessionLeaseKeeper) {
	_ = s.publishControllerSwap(detached.ctrl, cur, cur.SessionPath())
	detached.keeper = s.leases.Split()
	s.leases.Adopt(demoted)
	if concrete, ok := cur.(*control.Controller); ok {
		concrete.SetOnSessionRecovered(s.sessionRecoveryHandler(concrete, s.leases))
	}
	_, _ = s.registerDetached(detached.ctrl, detached.keeper, detached.tag)
}

// publishControllerSwap makes the command target and current-only SSE route
// visible as one generation. Readers cannot observe next while the broadcaster
// still filters against expect's session.
func (s *Server) publishControllerSwap(expect, next control.SessionAPI, path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctrl != expect {
		return false
	}
	s.ctrl = next
	s.bc.SetCurrentSession(path)
	return true
}

func (s *Server) replayPendingPromptsBroadcast() {
	cur := s.ctl()
	path := cur.SessionPath()
	cur.ReplayPendingPromptsWith(func() event.Sink {
		return event.FuncSink(func(e event.Event) {
			e.SessionPath = path
			s.bc.Emit(e)
		})
	})
}

func (s *Server) renderBindError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrSessionLeaseHeld):
		http.Error(w, sessionInUseError(err), http.StatusConflict)
	case errors.Is(err, errReplacedDuringBind), errors.Is(err, errSessionTagUnavailable):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, "switch session: "+err.Error(), http.StatusInternalServerError)
	}
}

// retireDetachedForProviderHeal is called with bindMu held. It makes every
// detached controller unreattachable and waits for its provider generation to
// close before credential reload is acknowledged.
func (s *Server) retireDetachedForProviderHeal() {
	s.detachedMu.Lock()
	detached := make([]*detachedSession, 0, len(s.detached))
	for _, d := range s.detached {
		detached = append(detached, d)
		select {
		case <-d.force:
		default:
			close(d.force)
		}
	}
	s.detachedMu.Unlock()
	for _, d := range detached {
		slog.Info("serve: provider heal retires background session", "session", d.path)
		<-d.done
	}
}
