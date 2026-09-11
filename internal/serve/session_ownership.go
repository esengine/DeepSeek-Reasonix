package serve

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// Session ownership handoff: the single-writer protocol behind local takeover.
//
// A session has exactly one writer at a time — the runtime holding its lease.
// When the machine hosting Serve is also where the user now sits (the remote
// desktop came "home"), the local Reasonix window can take a session over:
// Serve releases the lease and the local window acquires it. Serve keeps no
// controller authority for a mirrored session, but stays the rendezvous: the
// remote tab's SSE stream keeps rendering because the local writer pushes its
// frames through POST /external/frames, and the remote side drops to read-only
// until it reclaims speaking rights via POST /reclaim.
//
// Every transition is cooperative — nothing ever steals the OS-level lease
// file lock. Handoff releases what Serve holds; reclaim waits for the local
// writer to release what it holds (a dead writer releases implicitly when the
// kernel drops its file lock).

// handoffMode selects how a takeover deals with a turn still running on the
// side that is losing the session.
type handoffMode string

const (
	handoffModeWait      handoffMode = "wait"      // drain: wait for the active turn to finish
	handoffModeInterrupt handoffMode = "interrupt" // cancel the active turn, then hand off
)

const (
	// handoffDefaultTimeout bounds a drain-mode takeover and a reclaim's wait
	// for the local writer to yield.
	handoffDefaultTimeout = 60 * time.Second
	handoffPollInterval   = 200 * time.Millisecond
	// mirrorStaleAfter is how long a mirrored session goes without writer
	// contact (frames or heartbeats) before Serve is willing to probe whether
	// the writer is gone and auto-reclaim. Generous against laptop sleeps and
	// GC pauses; the lease probe is the real authority.
	mirrorStaleAfter       = 30 * time.Second
	externalFramesMaxBody  = 8 << 20
	externalFramesMaxCount = 512
)

// leaseHeldByForeignRuntime probes whether a runtime other than this Serve
// process holds a session's lease. It is a variable only so tests can model
// the local writer as a separate process: the real probe answers false for
// leases held by the calling process, and tests hold the writer's lease
// in-process.
var leaseHeldByForeignRuntime = agent.SessionLeaseHeldByOtherRuntime

// errSessionTakenOver is the stable refusal every mutating endpoint returns
// while the foreground session is mirrored to a local writer. Clients match
// the leading sentence to surface the read-only state.
const errSessionTakenOver = "session is taken over by a local Reasonix window and is read-only here; use POST /reclaim to take it back"

// mirroredSession is Serve's bookkeeping for a session whose lease a local
// runtime now holds. Serve answers reads from the transcript file and mirrors
// the writer's frames to subscribers, but must not mutate the session.
type mirroredSession struct {
	path             string
	mirrorID         string
	handoffID        string
	returnHandoffID  string
	sourceWriterID   string
	targetWriterID   string
	phase            mirrorPhase
	since            time.Time
	lastContact      time.Time
	reclaimRequested bool
	reclaimMode      handoffMode
}

type mirrorPhase string

const (
	mirrorPhasePending          mirrorPhase = "pending"
	mirrorPhaseExternal         mirrorPhase = "external"
	mirrorPhaseReclaimRequested mirrorPhase = "reclaim_requested"
	mirrorPhaseRecovering       mirrorPhase = "recovering"
)

type mirrorGrant struct {
	SessionPath     string `json:"sessionPath"`
	MirrorID        string `json:"mirrorId"`
	HandoffID       string `json:"handoffId,omitempty"`
	ReturnHandoffID string `json:"returnHandoffId"`
	SourceWriterID  string `json:"sourceWriterId"`
	TargetWriterID  string `json:"targetWriterId"`
	Status          string `json:"status"`
}

func newMirrorGeneration() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func newMirroredSession(path, sourceWriterID, targetWriterID string, phase mirrorPhase) (mirroredSession, error) {
	mirrorID, err := newMirrorGeneration()
	if err != nil {
		return mirroredSession{}, err
	}
	handoffID, err := newMirrorGeneration()
	if err != nil {
		return mirroredSession{}, err
	}
	returnHandoffID, err := newMirrorGeneration()
	if err != nil {
		return mirroredSession{}, err
	}
	now := time.Now()
	return mirroredSession{
		path: path, mirrorID: mirrorID, handoffID: handoffID,
		returnHandoffID: returnHandoffID, sourceWriterID: sourceWriterID,
		targetWriterID: targetWriterID, phase: phase, since: now, lastContact: now,
	}, nil
}

func (m mirroredSession) grant(status string) mirrorGrant {
	return mirrorGrant{
		SessionPath: m.path, MirrorID: m.mirrorID, HandoffID: m.handoffID,
		ReturnHandoffID: m.returnHandoffID, SourceWriterID: m.sourceWriterID,
		TargetWriterID: m.targetWriterID, Status: status,
	}
}

func (s *Server) markMirrored(m mirroredSession) {
	path := agent.CanonicalSessionPath(m.path)
	if path == "" {
		return
	}
	m.path = path
	s.mirrorMu.Lock()
	if s.mirrored == nil {
		s.mirrored = map[string]mirroredSession{}
	}
	s.mirrored[path] = m
	s.mirrorMu.Unlock()
}

func (s *Server) clearMirrored(path, mirrorID string) (mirroredSession, bool) {
	path = agent.CanonicalSessionPath(path)
	s.mirrorMu.Lock()
	m, ok := s.mirrored[path]
	if !ok || (mirrorID != "" && m.mirrorID != mirrorID) {
		s.mirrorMu.Unlock()
		return mirroredSession{}, false
	}
	delete(s.mirrored, path)
	s.mirrorMu.Unlock()
	return m, true
}

func (s *Server) mirroredEntry(path string) (mirroredSession, bool) {
	path = agent.CanonicalSessionPath(path)
	s.mirrorMu.Lock()
	defer s.mirrorMu.Unlock()
	m, ok := s.mirrored[path]
	return m, ok
}

func (s *Server) sessionMirrored(path string) bool {
	_, ok := s.mirroredEntry(path)
	return ok
}

// foregroundMirroredLocked reports whether the current foreground session has
// been handed to a local writer. Callers hold bindMu.
func (s *Server) foregroundMirroredLocked() bool {
	cur := s.ctl()
	if cur == nil {
		return false
	}
	return s.sessionMirrored(cur.SessionPath())
}

func (s *Server) touchMirrored(path, mirrorID string, phase mirrorPhase) (mirroredSession, bool) {
	s.mirrorMu.Lock()
	canonical := agent.CanonicalSessionPath(path)
	m, ok := s.mirrored[canonical]
	if ok && m.mirrorID == mirrorID {
		m.lastContact = time.Now()
		if phase != "" {
			m.phase = phase
		}
		s.mirrored[canonical] = m
	}
	s.mirrorMu.Unlock()
	return m, ok && m.mirrorID == mirrorID
}

// rejectMirroredForegroundLocked answers 409 for foreground mutations while
// the session is mirrored. Returns true when the response was written.
// Callers hold bindMu.
func (s *Server) rejectMirroredForegroundLocked(w http.ResponseWriter) bool {
	if !s.foregroundMirroredLocked() {
		return false
	}
	http.Error(w, errSessionTakenOver, http.StatusConflict)
	return true
}

// snapshotForeground persists the foreground session before a switch, unless a
// local writer owns it — a save attempt there fails closed (no write
// authority) and the conflict path could fork a recovery branch into a file
// the writer now owns. Callers hold bindMu.
func (s *Server) snapshotForeground(cur control.SessionAPI) {
	if s.foregroundMirroredLocked() {
		return
	}
	if err := cur.Snapshot(); err != nil {
		slog.Warn("serve: snapshot before switch", "err", err)
	}
}

// resolveSessionPath validates a client-supplied session path against the
// foreground session dir the same way POST /resume does: absolute, a real
// transcript file, inside the session dir, and not pending cleanup. The
// returned path is symlink-resolved.
func (s *Server) resolveSessionPath(raw string) (string, error) {
	dir := s.ctl().SessionDir()
	if dir == "" {
		return "", errors.New("sessions disabled")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", errors.New("invalid session dir")
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", errors.New("invalid session dir")
	}
	absPath, err := filepath.Abs(strings.TrimSpace(raw))
	if err != nil || !store.IsSessionTranscriptName(filepath.Base(absPath)) {
		return "", errors.New("invalid session path")
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", errors.New("invalid session path")
	}
	if realPath == realDir || !strings.HasPrefix(realPath, realDir+string(os.PathSeparator)) {
		return "", errors.New("path outside session dir")
	}
	if agent.IsCleanupPending(realPath) {
		return "", errors.New("session is pending cleanup")
	}
	return realPath, nil
}

type ownershipView struct {
	SessionPath      string `json:"sessionPath"`
	Holder           string `json:"holder"` // serve | external | other | free
	RemoteAttached   bool   `json:"remoteAttached"`
	Running          bool   `json:"running"`
	Mirrored         bool   `json:"mirrored"`
	ReclaimRequested bool   `json:"reclaimRequested"`
	TakenOver        bool   `json:"takenOver"`
	HolderPID        int    `json:"holderPid,omitempty"`
	HolderHost       string `json:"holderHost,omitempty"`
}

// ownership reports who currently writes a session, whether a remote SSE
// client is attached, and whether a turn is running — the inputs a local
// takeover prompt needs. remoteAttached counts every SSE subscriber; Serve
// cannot distinguish the desktop pump from a browser tab, so it is an
// over-approximation of "the remote side is watching".
func (s *Server) ownership(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("session")
	realPath, err := s.resolveSessionPath(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	view := ownershipView{SessionPath: agent.CanonicalSessionPath(realPath), RemoteAttached: s.bc.Subscribers() > 0}
	if m, ok := s.mirroredEntry(realPath); ok {
		view.Holder = "external"
		view.Mirrored = true
		view.TakenOver = true
		view.ReclaimRequested = m.reclaimRequested
		s.appendServeIdentity(&view)
		writeJSON(w, view)
		return
	}
	cur := s.ctl()
	foreground := cur != nil && agent.CanonicalSessionPath(cur.SessionPath()) == agent.CanonicalSessionPath(realPath)
	if foreground {
		view.Holder = "serve"
		view.Running = controllerHasActiveRuntimeWork(cur)
		s.appendServeIdentity(&view)
		writeJSON(w, view)
		return
	}
	if s.detachedBusy(realPath) {
		view.Holder = "serve"
		view.Running = s.detachedHasActiveWork(realPath)
		s.appendServeIdentity(&view)
		writeJSON(w, view)
		return
	}
	if leaseHeldByForeignRuntime(realPath) {
		view.Holder = "other"
	}
	if view.Holder == "" {
		view.Holder = "free"
	}
	writeJSON(w, view)
}

func (s *Server) appendServeIdentity(view *ownershipView) {
	host, _ := os.Hostname()
	view.HolderPID = os.Getpid()
	view.HolderHost = strings.TrimSpace(host)
}

func (s *Server) detachedHasActiveWork(path string) bool {
	path = agent.CanonicalSessionPath(path)
	s.detachedMu.Lock()
	defer s.detachedMu.Unlock()
	d := s.detached[path]
	return d != nil && controllerHasActiveRuntimeWork(d.ctrl)
}

type handoffRequest struct {
	SessionPath    string `json:"sessionPath"`
	TargetWriterID string `json:"targetWriterId"`
	Force          bool   `json:"force"`
	Mode           string `json:"mode"`
	TimeoutMs      int    `json:"timeoutMs"`
}

// handoff releases a session Serve holds so a local runtime on this machine
// can take it over. With force unset it refuses while a remote client is
// attached — the caller is expected to have confirmed the takeover with its
// user via GET /ownership. wait drains a running turn; interrupt cancels it.
func (s *Server) handoff(w http.ResponseWriter, r *http.Request) {
	var body handoffRequest
	if err := decodeTakeoverJSON(w, r, &body); err != nil || strings.TrimSpace(body.SessionPath) == "" || strings.TrimSpace(body.TargetWriterID) == "" {
		if err == nil {
			http.Error(w, "missing sessionPath or targetWriterId", http.StatusBadRequest)
		}
		return
	}
	mode := parseHandoffMode(body.Mode)
	realPath, err := s.resolveSessionPath(body.SessionPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if existing, ok := s.mirroredEntry(realPath); ok {
		if existing.targetWriterID != strings.TrimSpace(body.TargetWriterID) {
			http.Error(w, "session is already handed off to another writer", http.StatusConflict)
			return
		}
		writeJSON(w, existing.grant("already_handed_off"))
		return
	}
	if !body.Force && s.bc.Subscribers() > 0 {
		http.Error(w, "session is attached to a remote client; retry with force after confirming the takeover", http.StatusConflict)
		return
	}
	timeout := handoffTimeout(body.TimeoutMs)

	// Drain or cancel outside bindMu: waiting inside would freeze every other
	// command for up to the whole timeout.
	if err := s.quietSessionForHandoff(realPath, mode, timeout); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	s.bindMu.Lock()
	m, err := s.handoffLocked(realPath, strings.TrimSpace(body.TargetWriterID))
	s.bindMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), statusForHandoffError(err))
		return
	}
	writeJSON(w, m.grant("handed_off"))
}

func parseHandoffMode(raw string) handoffMode {
	if handoffMode(raw) == handoffModeInterrupt {
		return handoffModeInterrupt
	}
	return handoffModeWait
}

func handoffTimeout(ms int) time.Duration {
	if ms <= 0 {
		return handoffDefaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func statusForHandoffError(err error) int {
	switch {
	case errors.Is(err, errSessionNotHeld), errors.Is(err, errHandoffBusyAgain):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

var (
	errSessionNotHeld   = errors.New("session is not held by this serve process")
	errHandoffBusyAgain = errors.New("session became busy again during handoff; retry")
)

// quietSessionForHandoff waits for (or cancels toward) an idle session before
// the binding transaction runs. It re-checks under bindMu afterwards: turn
// admission holds bindMu, so once the caller holds it and the session is
// idle, no new turn can start on it.
func (s *Server) quietSessionForHandoff(realPath string, mode handoffMode, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		cur := s.ctl()
		foreground := cur != nil && agent.CanonicalSessionPath(cur.SessionPath()) == agent.CanonicalSessionPath(realPath)
		var busy bool
		switch {
		case foreground:
			busy = controllerHasActiveRuntimeWork(cur)
			if busy && mode == handoffModeInterrupt {
				cur.Cancel()
			}
		case s.detachedBusy(realPath):
			s.detachedMu.Lock()
			d := s.detached[agent.CanonicalSessionPath(realPath)]
			ctrl := control.SessionAPI(nil)
			if d != nil {
				ctrl = d.ctrl
			}
			s.detachedMu.Unlock()
			busy = ctrl != nil && controllerHasActiveRuntimeWork(ctrl)
			if busy && mode == handoffModeInterrupt {
				ctrl.Cancel()
			}
		default:
			return errSessionNotHeld
		}
		if !busy {
			return nil
		}
		if time.Now().After(deadline) {
			if mode == handoffModeInterrupt {
				return fmt.Errorf("session did not stop within %s; retry", timeout)
			}
			return fmt.Errorf("session is still running after %s; retry with mode=interrupt to cancel it", timeout)
		}
		time.Sleep(handoffPollInterval)
	}
}

// handoffLocked performs the release transaction. Callers hold bindMu and
// have already quieted the session.
func (s *Server) handoffLocked(realPath, targetWriterID string) (mirroredSession, error) {
	cur := s.ctl()
	canonical := agent.CanonicalSessionPath(realPath)
	info, err := agent.LoadSessionLeaseInfo(realPath)
	if err != nil || info == nil || strings.TrimSpace(info.WriterID) == "" {
		return mirroredSession{}, fmt.Errorf("handoff: current lease identity unavailable")
	}
	m, err := newMirroredSession(canonical, info.WriterID, targetWriterID, mirrorPhasePending)
	if err != nil {
		return mirroredSession{}, fmt.Errorf("handoff: create generation: %w", err)
	}
	switch {
	case cur != nil && agent.CanonicalSessionPath(cur.SessionPath()) == canonical:
		if controllerHasActiveRuntimeWork(cur) {
			return mirroredSession{}, errHandoffBusyAgain
		}
		// Flush the in-memory transcript while this process still owns the
		// file, then hand the lease over. Rebind("") also unbinds the
		// controller's write authority, so any later save fails closed
		// instead of racing the new writer.
		if err := cur.Snapshot(); err != nil {
			return mirroredSession{}, fmt.Errorf("handoff: snapshot session: %w", err)
		}
		if s.leases == nil {
			return mirroredSession{}, fmt.Errorf("handoff: lease keeper unavailable")
		}
		if err := s.leases.ReleaseForHandoff(targetWriterID, m.handoffID); err != nil {
			return mirroredSession{}, fmt.Errorf("handoff: release session lease: %w", err)
		}
	case s.detachedBusy(realPath):
		detached := s.takeDetached(realPath)
		if detached == nil {
			return mirroredSession{}, errHandoffBusyAgain
		}
		if controllerHasActiveRuntimeWork(detached.ctrl) {
			_, _ = s.registerDetached(detached.ctrl, detached.keeper, detached.tag)
			return mirroredSession{}, errHandoffBusyAgain
		}
		if err := detached.ctrl.Snapshot(); err != nil {
			_, _ = s.registerDetached(detached.ctrl, detached.keeper, detached.tag)
			return mirroredSession{}, fmt.Errorf("handoff: snapshot detached session: %w", err)
		}
		if detached.keeper == nil {
			_, _ = s.registerDetached(detached.ctrl, detached.keeper, detached.tag)
			return mirroredSession{}, fmt.Errorf("handoff: detached lease keeper unavailable")
		}
		if err := detached.keeper.ReleaseForHandoff(targetWriterID, m.handoffID); err != nil {
			_, _ = s.registerDetached(detached.ctrl, detached.keeper, detached.tag)
			return mirroredSession{}, fmt.Errorf("handoff: release detached session lease: %w", err)
		}
		detached.ctrl.Close()
		if concrete, ok := detached.ctrl.(*control.Controller); ok {
			s.forgetSessionTag(concrete)
		}
	default:
		return mirroredSession{}, errSessionNotHeld
	}
	s.markMirrored(m)
	slog.Info("serve: session handed off to local runtime", "session", canonical)
	s.bc.Emit(event.Event{
		Kind:        event.Notice,
		Level:       event.LevelWarn,
		Code:        event.NoticeCodeSessionTakenOver,
		Text:        "This session was taken over by a local Reasonix window and is read-only here.",
		Detail:      "A Reasonix window on this machine took over the conversation. It keeps streaming here; use \"take back\" to reclaim it.",
		SessionPath: canonical,
	})
	return m, nil
}

type externalFramesRequest struct {
	SessionPath string            `json:"sessionPath"`
	MirrorID    string            `json:"mirrorId"`
	Frames      []eventwire.Event `json:"frames"`
}

type externalFramesResponse struct {
	ReclaimRequested bool        `json:"reclaimRequested"`
	ReclaimMode      handoffMode `json:"reclaimMode,omitempty"`
	ReturnHandoffID  string      `json:"returnHandoffId,omitempty"`
	SourceWriterID   string      `json:"sourceWriterId,omitempty"`
}

// externalFrames mirrors the local writer's frames to every subscriber. An
// empty frame list is a heartbeat: the response tells the writer when the
// remote side asked for the session back, so an idle writer learns about a
// reclaim without pushing anything.
func (s *Server) externalFrames(w http.ResponseWriter, r *http.Request) {
	var body externalFramesRequest
	if err := decodeTakeoverJSON(w, r, &body); err != nil || strings.TrimSpace(body.SessionPath) == "" || strings.TrimSpace(body.MirrorID) == "" {
		if err == nil {
			http.Error(w, "missing sessionPath or mirrorId", http.StatusBadRequest)
		}
		return
	}
	if len(body.Frames) > externalFramesMaxCount {
		http.Error(w, "too many frames", http.StatusRequestEntityTooLarge)
		return
	}
	realPath, err := s.resolveSessionPath(body.SessionPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	canonical := agent.CanonicalSessionPath(realPath)
	mirrorID := strings.TrimSpace(body.MirrorID)
	// Validate, publish and advance contact under one mirror generation lock.
	// Re-adopt rotates the token under the same lock, so an old request cannot
	// pass validation, lose its generation, and still emit frames before the
	// post-publication check notices.
	s.mirrorMu.Lock()
	m, ok := s.mirrored[canonical]
	if !ok || m.mirrorID != mirrorID {
		s.mirrorMu.Unlock()
		http.Error(w, "session is not mirrored by this serve process", http.StatusConflict)
		return
	}
	m.lastContact = time.Now()
	m.phase = mirrorPhaseExternal
	s.mirrored[canonical] = m
	for i := range body.Frames {
		frame := body.Frames[i]
		frame.SessionPath = canonical
		s.bc.EmitWire(frame)
	}
	s.mirrorMu.Unlock()
	writeJSON(w, externalFramesResponse{
		ReclaimRequested: m.reclaimRequested, ReclaimMode: m.reclaimMode,
		ReturnHandoffID: m.returnHandoffID, SourceWriterID: m.sourceWriterID,
	})
}

func decodeTakeoverJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, externalFramesMaxBody))
	if err := decoder.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid request body", http.StatusBadRequest)
		}
		return err
	}
	return nil
}

// reclaim is the remote side's way back: it asks the local writer to yield
// the session, waits for the lease to come free, then re-owns the session.
// The local side demotes passively — it sees reclaimRequested on its next
// frame push or heartbeat — so exactly one side speaks at any moment.
func (s *Server) reclaim(w http.ResponseWriter, r *http.Request) {
	var body handoffRequest
	if err := decodeTakeoverJSON(w, r, &body); err != nil || strings.TrimSpace(body.SessionPath) == "" {
		if err == nil {
			http.Error(w, "missing sessionPath", http.StatusBadRequest)
		}
		return
	}
	mode := parseHandoffMode(body.Mode)
	timeout := handoffTimeout(body.TimeoutMs)
	realPath, err := s.resolveSessionPath(body.SessionPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	canonical := agent.CanonicalSessionPath(realPath)

	s.mirrorMu.Lock()
	m, ok := s.mirrored[canonical]
	if !ok {
		s.mirrorMu.Unlock()
		if s.serveHoldsSession(realPath) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// A session held by a local process that was never adopted (the adopt
		// can fail silently) has no mirror forwarder to signal. The reclaim
		// can only wait for the lease to free — cap it short so the caller
		// gets actionable feedback instead of a two-minute hang.
		if leaseHeldByForeignRuntime(realPath) {
			slog.Info("serve: reclaim on un-mirrored foreign-held session (adopter absent)",
				"session", canonical)
			deadline := time.Now().Add(10 * time.Second)
			for leaseHeldByForeignRuntime(realPath) {
				if time.Now().After(deadline) {
					http.Error(w, "session is held by a local Reasonix window that never registered a mirror; close that window or retry after it exits", http.StatusConflict)
					return
				}
				time.Sleep(handoffPollInterval)
			}
			s.bindMu.Lock()
			defer s.bindMu.Unlock()
			s.resumeSession(w, r, realPath)
			return
		}
		http.Error(w, "session is not held by any known runtime", http.StatusConflict)
		return
	}
	m.reclaimRequested = true
	m.reclaimMode = mode
	m.phase = mirrorPhaseReclaimRequested
	s.mirrored[canonical] = m
	s.mirrorMu.Unlock()
	s.bc.Emit(event.Event{
		Kind:        event.Notice,
		Code:        event.NoticeCodeSessionReclaimRequested,
		Text:        "The remote side asked to take this session back.",
		SessionPath: canonical,
	})
	slog.Info("serve: reclaim requested", "session", canonical, "mode", string(mode))

	deadline := time.Now().Add(timeout)
	for leaseHeldByForeignRuntime(realPath) {
		if time.Now().After(deadline) {
			http.Error(w, "local writer did not yield the session; retry", http.StatusConflict)
			return
		}
		time.Sleep(handoffPollInterval)
	}

	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	current, ok := s.mirroredEntry(realPath)
	if !ok || current.mirrorID != m.mirrorID {
		if s.serveHoldsSession(realPath) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "mirror generation changed during reclaim", http.StatusConflict)
		return
	}
	s.reclaimMirroredLocked(w, realPath, current)
}

func (s *Server) serveHoldsSession(realPath string) bool {
	cur := s.ctl()
	if cur != nil && agent.CanonicalSessionPath(cur.SessionPath()) == agent.CanonicalSessionPath(realPath) {
		return true
	}
	return s.detachedBusy(realPath)
}

// reclaimMirroredLocked acquires the returning writer's reservation, reloads
// and binds the controller, and only then clears the matching mirror epoch.
// Callers hold bindMu.
func (s *Server) reclaimMirroredLocked(w http.ResponseWriter, realPath string, mirror mirroredSession) {
	current, ok := s.mirroredEntry(realPath)
	if !ok || current.mirrorID != mirror.mirrorID {
		http.Error(w, "mirror generation changed", http.StatusConflict)
		return
	}
	s.touchMirrored(realPath, mirror.mirrorID, mirrorPhaseRecovering)
	cur := s.ctl()
	if cur == nil || s.leases == nil {
		http.Error(w, "session runtime unavailable", http.StatusInternalServerError)
		return
	}
	canonical := agent.CanonicalSessionPath(realPath)
	if agent.CanonicalSessionPath(cur.SessionPath()) != canonical && !s.foregroundMirroredLocked() {
		if err := cur.Snapshot(); err != nil {
			http.Error(w, "snapshot current session: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	previous, err := s.acquireReturningLease(realPath, mirror)
	if err != nil {
		if errors.Is(err, agent.ErrSessionLeaseHeld) {
			http.Error(w, sessionInUseError(err), http.StatusConflict)
		} else {
			http.Error(w, "session lease: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	committed := false
	defer func() {
		if committed {
			if previous != nil {
				previous.RetireDetached()
			}
			return
		}
		s.rollbackReclaimLease(cur, previous)
	}()
	loaded, err := agent.LoadSession(realPath)
	if err != nil {
		http.Error(w, "load session: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !s.commitLoadedResume(w, cur, loaded, realPath) {
		return
	}
	if _, ok := s.clearMirrored(realPath, mirror.mirrorID); !ok {
		http.Error(w, "mirror generation changed", http.StatusConflict)
		return
	}
	committed = true
	s.bc.ResetSessionPath(realPath)
	s.announceSessionChanged(realPath, false)
	s.broadcastReclaimed(realPath)
	w.WriteHeader(http.StatusNoContent)
	s.replayPendingPromptsBroadcast()
}

// rollbackReclaimLease restores the controller and keeper that were detached
// while a mirrored target was acquired. commitLoadedResume can reject after
// Resume (for example when a test hook rotates the current controller), so the
// source transcript is reloaded and re-authorized before the failed target
// lease is retired.
func (s *Server) rollbackReclaimLease(cur control.SessionAPI, previous *control.SessionLeaseKeeper) {
	failed := s.leases.Split()
	if previous == nil {
		if failed != nil {
			failed.Release()
		}
		return
	}
	previousPath := previous.HeldPath()
	loaded, err := agent.LoadSession(previousPath)
	if err == nil {
		err = previous.BindSessionAuthority(loaded)
	}
	if err == nil {
		cur.Resume(loaded, previousPath)
	} else {
		slog.Error("serve: restore source after failed reclaim", "err", err)
	}
	s.leases.Adopt(previous)
	if ctrl, ok := cur.(*control.Controller); ok && err == nil {
		if bindErr := s.leases.BindControllerAuthority(ctrl); bindErr != nil {
			slog.Error("serve: restore source authority after failed reclaim", "err", bindErr)
		}
	}
	if failed != nil {
		// The same controller may already be restored through s.leases. Retire
		// only the failed target lease without clearing that shared authority.
		failed.RetireDetached()
	}
}

func (s *Server) acquireReturningLease(realPath string, mirror mirroredSession) (*control.SessionLeaseKeeper, error) {
	info, err := agent.LoadSessionLeaseInfo(realPath)
	if err == nil && info != nil && info.HandoffTo == agent.SessionWriterID() &&
		info.HandoffID == mirror.returnHandoffID && info.WriterID == mirror.targetWriterID {
		return s.leases.RebindDetachingWithHandoff(realPath, mirror.targetWriterID, mirror.returnHandoffID)
	}
	return s.leases.RebindDetaching(realPath)
}

func (s *Server) broadcastReclaimed(realPath string) {
	s.bc.Emit(event.Event{
		Kind:        event.Notice,
		Code:        event.NoticeCodeSessionReclaimed,
		Text:        "This session is driven remotely again.",
		SessionPath: agent.CanonicalSessionPath(realPath),
	})
}

// adopt registers a session the local runtime already owns as mirrored, so
// the remote side can watch it read-only and reclaim it. This is the local
// desktop's announcement when it opens a session directly (no takeover — there
// was nothing to hand off): Serve must know about the writer to mediate
// reclaim and mirror the frames. Sessions Serve itself holds are refused —
// those go through /handoff instead.
func (s *Server) adopt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionPath string `json:"sessionPath"`
		WriterID    string `json:"writerId"`
	}
	if err := decodeTakeoverJSON(w, r, &body); err != nil || strings.TrimSpace(body.SessionPath) == "" || strings.TrimSpace(body.WriterID) == "" {
		if err == nil {
			http.Error(w, "missing sessionPath or writerId", http.StatusBadRequest)
		}
		return
	}
	realPath, err := s.resolveSessionPath(body.SessionPath)
	if err != nil {
		http.Error(w, err.Error(), resolveSessionPathStatus(err))
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if s.serveHoldsSession(realPath) {
		http.Error(w, "session is held by this serve; use POST /handoff to take it over", http.StatusConflict)
		return
	}
	info, held, inspectErr := agent.InspectSessionLease(realPath)
	if inspectErr != nil || !held || info == nil || info.WriterID != strings.TrimSpace(body.WriterID) {
		http.Error(w, "session is not held by the claimed writer", http.StatusConflict)
		return
	}
	if existing, ok := s.mirroredEntry(realPath); ok && existing.targetWriterID != info.WriterID {
		http.Error(w, "session is mirrored by another writer", http.StatusConflict)
		return
	}
	m, err := newMirroredSession(agent.CanonicalSessionPath(realPath), agent.SessionWriterID(), info.WriterID, mirrorPhaseExternal)
	if err != nil {
		http.Error(w, "create mirror generation", http.StatusInternalServerError)
		return
	}
	m.handoffID = ""
	s.markMirrored(m)
	slog.Info("serve: session adopted by local runtime", "session", agent.CanonicalSessionPath(realPath))
	s.bc.Emit(event.Event{
		Kind:        event.Notice,
		Level:       event.LevelWarn,
		Code:        event.NoticeCodeSessionTakenOver,
		Text:        "This session was taken over by a local Reasonix window and is read-only here.",
		Detail:      "A Reasonix window on this machine opened this session; it keeps streaming here. Use \"take back\" to reclaim it.",
		SessionPath: agent.CanonicalSessionPath(realPath),
	})
	writeJSON(w, m.grant("adopted"))
}

// mirroredReadView reports whether session is mirrored, and if so builds the
// file-backed read view for read-only endpoints that select a specific
// session.
func (s *Server) mirroredReadView(session string) (string, []provider.Message, bool) {
	realPath, err := s.resolveSessionPath(session)
	if err != nil || !s.sessionMirrored(realPath) {
		return "", nil, false
	}
	msgs, ok := s.mirroredHistory(realPath)
	if !ok {
		return "", nil, false
	}
	return agent.CanonicalSessionPath(realPath), msgs, true
}

// externalReadView serves the file-backed read view for any session a local
// runtime owns — mirrored via /adopt or /handoff, or merely held by another
// process on this machine. A spectator client (remote tab) needs the local
// writer's transcript, not Serve's foreground.
func (s *Server) externalReadView(session string) (string, []provider.Message, bool) {
	if s.sessionMirrored(session) {
		return s.mirroredReadView(session)
	}
	realPath, err := s.resolveSessionPath(session)
	if err != nil || !leaseHeldByForeignRuntime(realPath) {
		return "", nil, false
	}
	msgs, ok := s.mirroredHistory(realPath)
	if !ok {
		return "", nil, false
	}
	return agent.CanonicalSessionPath(realPath), msgs, true
}

// statusViewForPath renders the per-session status payload for the ?session=
// selector. Local-owned sessions (mirrored or foreign-held) report takenOver;
// Serve-owned sessions report takenOver=false so a spectator client clears its
// read-only pin after reclaim or when the session returns to the foreground.
func (s *Server) statusViewForPath(path string, held bool) map[string]any {
	if held {
		return s.externalStatusView(path)
	}
	running := false
	cur := s.ctl()
	if cur != nil && agent.CanonicalSessionPath(cur.SessionPath()) == agent.CanonicalSessionPath(path) {
		running = controllerHasActiveRuntimeWork(cur)
	}
	return map[string]any{
		"label":            s.ctl().Label(),
		"running":          running,
		"plan":             false,
		"autoApproveTools": false,
		"bypass":           false,
		"toolApprovalMode": "ask",
		"cwd":              s.ctl().SessionDir(),
		"pendingPrompt":    false,
		"backgroundJobs":   0,
		"takenOver":        false,
		"sessionName":      strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		"sessionPath":      agent.CanonicalSessionPath(path),
	}
}

// externalStatusView renders the status payload for a session a local runtime
// owns (mirrored or foreign-held): nothing here can run, ownership is external,
// and the surface must render read-only.
func (s *Server) externalStatusView(path string) map[string]any {
	if _, ok := s.mirroredEntry(path); ok {
		return s.mirrorStatusView(path)
	}
	sess := map[string]any{
		"label":            s.ctl().Label(),
		"running":          false,
		"plan":             false,
		"autoApproveTools": false,
		"bypass":           false,
		"toolApprovalMode": "ask",
		"cwd":              s.ctl().SessionDir(),
		"pendingPrompt":    false,
		"backgroundJobs":   0,
		"takenOver":        true,
		"sessionName":      strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		"sessionPath":      agent.CanonicalSessionPath(path),
	}
	return sess
}

// mirrorStatusView renders the status payload for a mirrored session selected
// via ?session=: nothing can run here, ownership is external, and the surface
// must render read-only.
func (s *Server) mirrorStatusView(path string) map[string]any {
	m, ok := s.mirroredEntry(path)
	sess := map[string]any{
		"label":            s.ctl().Label(),
		"running":          false,
		"plan":             false,
		"autoApproveTools": false,
		"bypass":           false,
		"toolApprovalMode": "ask",
		"cwd":              s.ctl().SessionDir(),
		"pendingPrompt":    false,
		"backgroundJobs":   0,
		"takenOver":        true,
		"sessionName":      strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		"sessionPath":      agent.CanonicalSessionPath(path),
	}
	if ok {
		sess["reclaimRequested"] = m.reclaimRequested
	}
	return sess
}

// mirrorEnd is the local writer's farewell: it has closed its tab and dropped
// the lease, so the remote side can speak again without an explicit reclaim
// round-trip. A writer that still holds the lease is told to release first —
// ending the mirror under a live writer would leave the foreground writable
// in name only (its write authority is gone) and render stale history.
func (s *Server) mirrorEnd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionPath string `json:"sessionPath"`
		MirrorID    string `json:"mirrorId"`
	}
	if err := decodeTakeoverJSON(w, r, &body); err != nil || strings.TrimSpace(body.SessionPath) == "" || strings.TrimSpace(body.MirrorID) == "" {
		if err == nil {
			http.Error(w, "missing sessionPath or mirrorId", http.StatusBadRequest)
		}
		return
	}
	realPath, err := s.resolveSessionPath(body.SessionPath)
	if err != nil {
		http.Error(w, err.Error(), resolveSessionPathStatus(err))
		return
	}
	m, ok := s.mirroredEntry(realPath)
	if !ok || m.mirrorID != strings.TrimSpace(body.MirrorID) {
		http.Error(w, "mirror generation changed", http.StatusConflict)
		return
	}
	if leaseHeldByForeignRuntime(realPath) {
		http.Error(w, "local writer still holds the session; release it before ending the mirror", http.StatusConflict)
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	current, ok := s.mirroredEntry(realPath)
	if !ok || current.mirrorID != m.mirrorID {
		http.Error(w, "mirror generation changed", http.StatusConflict)
		return
	}
	s.reclaimMirroredLocked(w, realPath, current)
}

// maybeAutoReclaimMirrored recovers a mirror whose writer vanished without
// calling /mirror-end (killed window, laptop died). The OS releases the lease
// with the process; once the entry is stale and the lease is free, hand the
// session back to the remote side.
func (s *Server) maybeAutoReclaimMirrored(path string) {
	m, ok := s.mirroredEntry(path)
	if !ok {
		return
	}
	if time.Since(m.lastContact) < mirrorStaleAfter {
		return
	}
	if leaseHeldByForeignRuntime(path) {
		// The writer is alive but quiet (or another runtime took the file).
		// Push the staleness window so a chatty-but-healthy writer never
		// gets reclaimed under itself.
		s.touchMirrored(path, m.mirrorID, "")
		return
	}
	if m.reclaimRequested {
		// The writer vanished AFTER a reclaim was requested: its OS lock died
		// with it, so the outstanding reclaim can finally complete. Skipping
		// here (as this function used to) left the entry mirrored with the
		// flag set forever — the remote tab stayed a read-only spectator with
		// every retry 409ing after the wait timeout.
		slog.Info("serve: completing outstanding reclaim for vanished writer",
			"session", agent.CanonicalSessionPath(path))
	}
	go func() {
		s.bindMu.Lock()
		defer s.bindMu.Unlock()
		current, ok := s.mirroredEntry(path)
		if !ok || current.mirrorID != m.mirrorID {
			return
		}
		recorder := &statusRecorder{header: http.Header{}}
		s.reclaimMirroredLocked(recorder, path, current)
		if recorder.status >= http.StatusBadRequest {
			slog.Warn("serve: auto-reclaim of stale mirror failed", "session", path, "status", recorder.status)
			return
		}
		slog.Info("serve: stale mirror auto-reclaimed", "session", path)
	}()
}

type statusRecorder struct {
	header http.Header
	status int
}

func (w *statusRecorder) Header() http.Header { return w.header }
func (w *statusRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(p), nil
}
func (w *statusRecorder) WriteHeader(status int) { w.status = status }

// mirroredHistory reads the transcript file for a mirrored session so
// hydrating and reconciling clients see the local writer's turns, not Serve's
// frozen in-memory copy. Returns false when the file cannot be read; callers
// fall back to the stale in-memory history.
func (s *Server) mirroredHistory(realPath string) ([]provider.Message, bool) {
	loaded, err := agent.LoadSession(realPath)
	if err != nil || loaded == nil {
		return nil, false
	}
	return loaded.Messages, true
}
