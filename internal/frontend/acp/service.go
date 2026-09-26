package acp

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension/uihub"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/session/control"
	"reasonix/internal/state/store"
	"reasonix/internal/tools/builtin"
	"reasonix/internal/tools/jobs"
)

// SessionParams is everything a Factory needs to assemble one ACP session's
// controller. Sink is owned by this package (an updateSink bound to the session
// id) and must be wired into the controller's event sink; the controller's
// interactive approval (see control.Controller.EnableInteractiveApproval) then
// routes "ask" decisions back through that sink as ApprovalRequest events, which
// the sink forwards to the client over session/request_permission.
//
// Cwd roots the session's file tools and bash (built via builtin.Workspace).
// Model, EffortOverride, and RuntimeProfile are optional session-local selectors
// from ACP config options. MCPServers are the MCP servers the client asked the
// agent to connect for this session. OnSessionRecovered is the service's
// bookkeeping hook for automatic transcript recovery branches (see
// sessionRecoveredHandler); factories must wire it into the controller they build.
type SessionParams struct {
	Cwd                string
	MCPServers         []plugin.Spec
	Sink               event.Sink
	Model              string
	EffortOverride     *string
	RuntimeProfile     string
	OnSessionRecovered func(control.SessionRecoveryInfo) error
	// FileOverlay and Terminal are non-nil when the client advertised the
	// matching capability at initialize: file tools then see unsaved editor
	// buffers, and foreground bash can run in a client-owned terminal.
	// Factories thread them into the controller's tool assembly.
	FileOverlay builtin.FileOverlay
	Terminal    builtin.TerminalRunner
}

// Factory builds the per-session controller. The composition root (the cli's
// `reasonix acp` command) implements it by reusing setup()'s assembly: a
// Provider for Model, a tool Registry rooted at Cwd via builtin.Workspace, a
// per-session MCP host from MCPServers, the event Sink, all wired into a
// control.Controller. The returned controller owns its own cleanup (Close stops
// MCP subprocesses), so the service calls ctrl.Close() on teardown.
type Factory interface {
	NewSession(ctx context.Context, p SessionParams) (*control.Controller, error)
}

// SessionConfigStateParams asks the Factory for normalized session config
// selectors. Empty Model and RuntimeProfile use configured defaults. Nil
// EffortOverride means provider config wins; a non-nil empty string means
// provider default for this session.
type SessionConfigStateParams struct {
	Cwd            string
	Model          string
	EffortOverride *string
	RuntimeProfile string
}

// SessionConfigState is the complete ACP-visible config state for a session.
type SessionConfigState struct {
	Model          string
	EffortOverride *string
	RuntimeProfile string
	Models         *SessionModelState
	ConfigOptions  []SessionConfigOption
}

// SessionConfigStateProvider lets a Factory expose model, effort, and work-mode
// selectors without making the ACP transport depend on a concrete config backend.
type SessionConfigStateProvider interface {
	SessionConfigState(ctx context.Context, p SessionConfigStateParams) (SessionConfigState, error)
}

// SessionDirProvider lets a Factory expose the persistent session directory
// without forcing session/list to build a controller first.
type SessionDirProvider interface {
	SessionDir() string
}

// SessionRebuilder lets a Factory rebuild a session's controller via
// boot.Rebuild: the replacement is built with the same boot.Options NewSession
// would use, and the session state (history, approval grants, goal/recovery,
// lifecycle) migrates off old inside the boot layer. The caller keeps the
// swap/close ordering. Factories that do not implement it leave
// _reasonix.io/session/reloadExtensions reporting unavailable.
type SessionRebuilder interface {
	RebuildSession(ctx context.Context, p SessionParams, old *control.Controller) (*control.Controller, error)
}

// AgentInfo identifies this agent to clients in the initialize reply.
type AgentInfo struct {
	Name    string
	Version string
}

// Serve runs an ACP agent on r/w (stdin/stdout in production) until the input
// ends or ctx is cancelled. It owns the JSON-RPC connection and the session
// registry; the Factory supplies the kernel wiring. This is the single entry
// point the `reasonix acp` command calls.
//
// stdout is the JSON-RPC channel: callers must keep all other output (logs,
// diagnostics) off w and on stderr, or the wire corrupts.
func Serve(ctx context.Context, r io.Reader, w io.Writer, factory Factory, info AgentInfo) error {
	conn := NewConn(r, w)
	svc := &service{
		conn:     conn,
		factory:  factory,
		info:     info,
		sessions: make(map[string]*acpSession),
	}
	conn.Handle("initialize", svc.initialize)
	conn.Handle("authenticate", svc.authenticate)
	conn.Handle("session/new", svc.sessionNew)
	conn.Handle("session/load", svc.sessionLoad)
	conn.Handle("session/resume", svc.sessionResume)
	conn.Handle("session/prompt", svc.sessionPrompt)
	conn.Handle(sessionSteerMethod, svc.sessionSteer)
	conn.Handle(sessionInboxEnqueueMethod, svc.sessionInboxEnqueue)
	conn.Handle(sessionInboxListMethod, svc.sessionInboxList)
	conn.Handle(sessionInboxGetMethod, svc.sessionInboxGet)
	conn.Handle(sessionInboxUpdateMethod, svc.sessionInboxUpdate)
	conn.Handle(sessionInboxDeleteMethod, svc.sessionInboxDelete)
	conn.Handle(sessionInboxMoveMethod, svc.sessionInboxMove)
	conn.Handle(sessionInboxPauseMethod, svc.sessionInboxSetPaused)
	conn.Handle(sessionInboxRetryMethod, svc.sessionInboxRetry)
	conn.Handle(sessionInboxRefreshMethod, svc.sessionInboxRefresh)
	conn.Handle(sessionReloadExtensionsMethod, svc.sessionReloadExtensions)
	registerGoalMethods(conn, svc)
	conn.Handle(sessionStatusMethod, svc.sessionStatus)
	conn.Handle("session/set_config_option", svc.sessionSetConfigOption)
	conn.Handle("session/set_model", svc.sessionSetModel)
	conn.Handle("session/set_mode", svc.sessionSetMode)
	conn.Handle("session/close", svc.sessionClose)
	conn.Handle("session/list", svc.sessionList)
	conn.Handle("session/delete", svc.sessionDelete)
	conn.HandleNotify("session/cancel", svc.sessionCancel)
	defer svc.closeAll()
	return conn.Serve(ctx)
}

// service holds the connection-wide ACP state: the factory, agent identity, and
// the live session registry.
type service struct {
	conn    *Conn
	factory Factory
	info    AgentInfo

	mu       sync.Mutex
	sessions map[string]*acpSession
	// clientCaps is what the client offered at initialize (fs proxy, host
	// terminals). Zero until initialize arrives; sessions opened later bind a
	// clientIO built from it.
	clientCaps ClientCapabilities
}

// afterResponse wraps a result with work that must run after the transport has
// successfully written that result. Session-opening notifications use this so a
// client can register the returned session before receiving its first update.
type afterResponse struct {
	result any
	after  func()
}

func (r afterResponse) Response() any { return r.result }

func (r afterResponse) AfterResponse() {
	if r.after != nil {
		r.after()
	}
}

func (s *service) setClientCapabilities(caps ClientCapabilities) {
	s.mu.Lock()
	s.clientCaps = caps
	s.mu.Unlock()
}

func (s *service) clientCapabilities() ClientCapabilities {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientCaps
}

// extensionSurfaceSupported reports whether the connected client advertised
// reasonix.extensionSurface support in its initialize handshake.
func (s *service) extensionSurfaceSupported() bool {
	return clientExtensionSurfaceSupported(s.clientCapabilities())
}

// clientExtensionSurfaceSupported tolerantly parses the client's vendor
// capability block: _meta["reasonix.io"]["extensionSurface"]["supported"] must
// be an explicit true. Absent keys, wrong shapes, or a malformed block all
// mean unsupported — the sink then sends only the text fallback.
func clientExtensionSurfaceSupported(caps ClientCapabilities) bool {
	vendor, ok := caps.Meta["reasonix.io"].(map[string]any)
	if !ok {
		return false
	}
	capability, ok := vendor["extensionSurface"].(map[string]any)
	if !ok {
		return false
	}
	supported, _ := capability["supported"].(bool)
	return supported
}

// bindClientIO fills SessionParams' overlay/terminal fields from the client's
// declared capabilities. The nil checks keep absent capabilities as nil
// interface fields (a typed-nil *clientIO must never reach the interface).
func (s *service) bindClientIO(p *SessionParams, sessionID string) {
	io := newClientIO(s.conn, sessionID, s.clientCapabilities())
	if !io.hasAny() {
		return
	}
	if fo := io.fileOverlay(); fo != nil {
		p.FileOverlay = fo
	}
	if tr := io.terminalRunner(); tr != nil {
		p.Terminal = tr
	}
}

// acpController is the slice of the driving port ACP drives: lifecycle,
// persistence, turns, approval, and the editor-facing capability surface. It
// names no checkpoint, memory, skill-authoring or settings port.
type acpController interface {
	control.Lifecycle
	control.TurnControl
	TrySteer(text string) bool
	control.Approvals
	control.SlashDispatch
	control.MCPControl
	control.Extensions
	control.SessionPersistence
	// Goals backs ACP's normal/plan/goal collaboration-mode surface.
	control.Goals
}

// acpSession is one open session: its controller, the on-disk transcript path
// (empty when persistence is off), and the cancel func of the in-flight turn
// (nil when idle) so session/cancel can abort it.
type acpSession struct {
	id         string
	ctrl       acpController
	sink       *updateSink
	transcript string
	cwd        string
	mcpServers []plugin.Spec
	model      string
	// nil means use config; non-nil empty string means provider default.
	effortOverride   *string
	runtimeProfile   string
	toolApprovalMode string
	// runtimeState is the effective planner/sandbox posture captured after CLI
	// hard overrides. status snapshots never reconstruct it from user config.
	runtimeState SessionRuntimeState
	status       *statusTelemetry
	// modeID is the ACP collaboration mode last reported to the client (normal |
	// plan | goal). Goal draft mode turns the next user prompt into the goal.
	// Both are guarded by mu; controller-side completion/plan exit is reconciled
	// after each turn through current_mode_update.
	modeID        string
	goalDraftMode bool
	// pendingConfig queues config deltas requested while a turn or rebuild is
	// in flight, holding at most one entry per axis: a later request replaces
	// only its own axis (last-write-wins per axis), so a model change and a
	// work-mode change queued back to back during one turn both survive to the
	// drain instead of the second overwriting the first.
	pendingConfig []sessionConfigDelta
	// pendingReload coalesces _reasonix.io/session/reloadExtensions requests
	// made while a turn or a rebuild is in flight; the finishTurn /
	// post-maintenance drains run it once the session is idle.
	pendingReload bool
	title         string
	createdAt     time.Time
	updatedAt     time.Time

	mu sync.Mutex
	// stateChangeMu serializes controller rebuilds with collaboration/approval
	// changes so a swap cannot overwrite a newer user selection.
	stateChangeMu sync.Mutex
	cancel        context.CancelFunc
	done          chan struct{}
	running       bool
	deleted       bool
	// lease is the session lease guarding transcript against other runtimes
	// (a desktop window, the CLI) for the life of this session. Held from
	// session/new / session/load and released on close/delete/teardown.
	// Config rebuilds keep the same transcript; when a snapshot conflict
	// retargets the controller to a recovery branch, sessionRecoveredHandler
	// moves transcript and this lease to the recovery file at commit time.
	lease *sessionstore.SessionLease
	// retiredLeases tracks outgoing leases whose Release must run after the
	// authority-guarded save that triggered a recovery callback returns. Any
	// ACP operation that exposes a completed Snapshot waits for these channels,
	// so callers never observe the old transcript as still owned after the
	// handoff has completed.
	retiredLeases []<-chan struct{}
	// maintenanceDone is non-nil while session-owned maintenance, such as an
	// idle config rebuild, is in flight outside mu.
	maintenanceDone chan struct{}
}

func (s *acpSession) begin(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	runCtx, cancel := context.WithCancel(ctx)
	// Prompt admission and config-axis changes share this lock. TryLock keeps
	// ACP admission non-blocking while closing the idle-check/use window in an
	// in-place role switch.
	if !s.stateChangeMu.TryLock() {
		cancel()
		return nil, nil, false
	}
	defer s.stateChangeMu.Unlock()
	s.mu.Lock()
	// A queued pendingConfig blocks new turns so a prompt never runs on the
	// outgoing config. The turn or maintenance that queued it applies it from
	// its defer, so no new turn is needed to drain the queue.
	if s.running || s.deleted || s.maintenanceDone != nil || len(s.pendingConfig) > 0 {
		s.mu.Unlock()
		cancel()
		return nil, nil, false
	}
	s.running = true
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()
	return runCtx, cancel, true
}

func (s *acpSession) finish() {
	s.mu.Lock()
	done := s.done
	s.running = false
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()
	if done != nil {
		close(done)
	}
}

func (s *acpSession) abort() {
	s.mu.Lock()
	c := s.cancel
	s.mu.Unlock()
	if c != nil {
		c()
	}
}

func (s *acpSession) abortAndWait() {
	s.mu.Lock()
	c := s.cancel
	done := s.done
	maintenanceDone := s.maintenanceDone
	s.mu.Unlock()
	if c != nil {
		c()
	}
	if done != nil {
		<-done
	}
	if maintenanceDone != nil {
		<-maintenanceDone
	}
}

func (s *acpSession) deleteAndWait() {
	s.mu.Lock()
	s.deleted = true
	c := s.cancel
	done := s.done
	maintenanceDone := s.maintenanceDone
	s.mu.Unlock()
	if c != nil {
		c()
	}
	if done != nil {
		<-done
	}
	if maintenanceDone != nil {
		<-maintenanceDone
	}
}

func (s *acpSession) finishMaintenance(done chan struct{}) {
	if done == nil {
		return
	}
	closeDone := false
	s.mu.Lock()
	if s.maintenanceDone == done {
		s.maintenanceDone = nil
		closeDone = true
	}
	s.mu.Unlock()
	if closeDone {
		close(done)
	}
}

// currentCtrl returns the session's controller under mu. rebuildSession swaps
// ctrl while holding mu, so any read of the field outside mu races with a
// concurrent config rebuild; always go through this accessor unless mu is
// already held.
func (s *acpSession) currentCtrl() acpController {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctrl
}

// releaseSessionLease drops the session's transcript lease, if any. Idempotent.
func (s *acpSession) releaseSessionLease() {
	s.mu.Lock()
	lease := s.lease
	s.lease = nil
	s.mu.Unlock()
	if lease != nil {
		lease.Release()
	}
	s.waitForRetiredSessionLeases()
}

// retireSessionLease defers Release until the authority-guarded save that
// invoked a recovery callback can return. Releasing synchronously inside that
// callback would wait on the very save executing the callback and deadlock.
func (s *acpSession) retireSessionLease(lease *sessionstore.SessionLease) {
	if lease == nil {
		return
	}
	done := make(chan struct{})
	s.mu.Lock()
	s.retiredLeases = append(s.retiredLeases, done)
	s.mu.Unlock()
	go func() {
		lease.Release()
		close(done)
	}()
}

func (s *acpSession) waitForRetiredSessionLeases() {
	s.mu.Lock()
	retired := append([]<-chan struct{}(nil), s.retiredLeases...)
	s.mu.Unlock()
	for _, done := range retired {
		<-done
	}
	s.mu.Lock()
	pending := s.retiredLeases[:0]
	for _, done := range s.retiredLeases {
		select {
		case <-done:
		default:
			pending = append(pending, done)
		}
	}
	s.retiredLeases = pending
	s.mu.Unlock()
}

// Session modes exposed over ACP describe how the agent advances the task.
// Tool approval and runtime profile are independent config options. The legacy
// default/auto ids remain accepted for clients that used the old mixed axis.
const (
	sessionModeNormal        = "normal"
	sessionModePlan          = "plan"
	sessionModeGoal          = "goal"
	sessionModeLegacyDefault = "default"
	sessionModeLegacyAuto    = "auto"
)

// emitModeDrift reports controller-side mode flips (plan mode auto-exits when
// a plan is approved, a config rebuild resets switches) as current_mode_update
// so the client's mode picker stays truthful.
func (s *service) emitModeDrift(sess *acpSession) {
	// Hold stateChangeMu across the controller read and the session-state swap:
	// a session/set_mode completing between them (it holds this lock) would
	// otherwise be read back as drift, roll the session's modeID and metadata
	// back to the pre-selection value, and make the next rebuild re-apply that
	// stale mode to the replacement controller.
	sess.stateChangeMu.Lock()
	defer sess.stateChangeMu.Unlock()
	ctrl := sess.currentCtrl()
	current := sessionModeNormal
	switch {
	case ctrl.PlanMode():
		current = sessionModePlan
	case ctrl.GoalStatus() == control.GoalStatusRunning || sess.isGoalDraftMode():
		current = sessionModeGoal
	}
	if sess.swapModeID(current) != current {
		sess.sink.send(currentModeUpdate{SessionUpdate: "current_mode_update", CurrentModeID: current})
		sess.saveMetaIfPresent()
	}
}

func (s *service) emitToolApprovalDrift(ctx context.Context, sess *acpSession) {
	// Same contract as emitModeDrift: serialize with switchSessionToolApproval
	// and rebuilds so a user selection landing between the controller read and
	// the swap below is never reverted.
	sess.stateChangeMu.Lock()
	defer sess.stateChangeMu.Unlock()
	current := normalizeACPToolApprovalMode(sess.currentCtrl().ToolApprovalMode())
	if sess.swapToolApprovalMode(current) == current {
		return
	}
	if cfgState, err := s.configStateForSession(ctx, sess); err == nil {
		sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
	}
	sess.saveMetaIfPresent()
}

func (s *service) openExistingSession(ctx context.Context, method, id, cwdParam string, servers []MCPServerSpec, replay bool) (SessionConfigState, error) {
	if err := validateSessionID(method, id); err != nil {
		return SessionConfigState{}, err
	}
	cwd, err := s.resolveSessionCwd(cwdParam, id)
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": " + err.Error()}
	}
	mcpServers, err := mcpSpecs(servers, cwd)
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": " + err.Error()}
	}

	if sess := s.session(id); sess != nil {
		if sessionstore.IsCleanupPending(sess.transcript) {
			return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": unknown session " + id}
		}
		if replay {
			ctrl := sess.currentCtrl()
			replaySink := newUpdateSink(s.conn, id)
			replaySink.bindCwd(sess.cwd)
			replaySink.replay(ctrl.History())
		}
		cfgState, err := s.configStateForSession(ctx, sess)
		if err != nil {
			return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
		}
		return cfgState, nil
	}

	var saved acpSessionMeta
	persistedPath := ""
	if dir := s.sessionDir(); dir != "" {
		persistedPath = resolveTranscriptPath(dir, id)
		if sessionstore.IsCleanupPending(persistedPath) {
			return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": unknown session " + id}
		}
		meta, _, metaErr := loadACPMeta(persistedPath)
		if metaErr != nil {
			return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + metaErr.Error()}
		}
		saved = meta
	}
	cfgParams := SessionConfigStateParams{
		Cwd:            cwd,
		Model:          saved.Model,
		EffortOverride: cloneStringPtr(saved.EffortOverride),
		RuntimeProfile: saved.RuntimeProfile,
	}
	cfgState, err := s.sessionConfigState(ctx, cfgParams)
	if err != nil && (strings.TrimSpace(saved.Model) != "" || saved.EffortOverride != nil || strings.TrimSpace(saved.RuntimeProfile) != "") {
		cfgState, err = s.sessionConfigState(ctx, SessionConfigStateParams{Cwd: cwd})
	}
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
	}
	runtimeState, err := s.sessionRuntimeState(ctx, SessionRuntimeStateParams{
		Cwd: cwd, Model: cfgState.Model, RuntimeProfile: cfgState.RuntimeProfile,
	})
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
	}

	sink := newUpdateSink(s.conn, id)
	sink.bindCwd(cwd)
	sink.bindExtensionSurface(s.extensionSurfaceSupported())
	sessionParams := SessionParams{
		Cwd:                cwd,
		MCPServers:         mcpServers,
		Sink:               sink,
		Model:              cfgState.Model,
		EffortOverride:     cloneStringPtr(cfgState.EffortOverride),
		RuntimeProfile:     cfgState.RuntimeProfile,
		OnSessionRecovered: s.sessionRecoveredHandler(id),
	}
	s.bindClientIO(&sessionParams, id)
	ctrl, err := s.factory.NewSession(ctx, sessionParams)
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
	}
	ctrl.EnableInteractiveApproval()
	sink.bindApprove(ctrl.Approve)
	sink.bindAnswer(ctrl.AnswerQuestion)

	dir := ctrl.SessionDir()
	if dir == "" {
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": persistence is disabled"}
	}
	path := resolveTranscriptPath(dir, id)
	if path != persistedPath && sessionstore.IsCleanupPending(path) {
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": unknown session " + id}
	}
	// Bind the transcript for writing only if no other runtime (a desktop
	// window, the CLI) holds it; the editor should not silently double-write a
	// session that is open elsewhere.
	lease, leaseErr := sessionstore.TryAcquireSessionLease(path)
	if leaseErr != nil {
		ctrl.Close()
		return SessionConfigState{}, sessionLeaseBindError(method, leaseErr)
	}
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		lease.Release()
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": unknown session " + id}
	}
	if err := resumeACPControllerForWrite(ctrl, loaded, path, lease); err != nil {
		return SessionConfigState{}, sessionLeaseBindError(method, err)
	}
	toolApprovalMode := normalizeACPToolApprovalMode(saved.ToolApprovalMode)
	ctrl.SetToolApprovalMode(toolApprovalMode)
	modeID := normalizeACPCollaborationMode(saved.CollaborationMode)
	goalDraftMode := false
	switch modeID {
	case sessionModePlan:
		ctrl.SetPlanMode(true)
	case sessionModeGoal:
		ctrl.SetPlanMode(false)
		goalDraftMode = ctrl.GoalStatus() != control.GoalStatusRunning
	default:
		if ctrl.GoalStatus() == control.GoalStatusRunning {
			modeID = sessionModeGoal
		} else {
			modeID = sessionModeNormal
			ctrl.SetPlanMode(false)
		}
	}

	meta := metadataForLoadedSession(path, id, cwd, ctrl.History())
	meta.Model = cfgState.Model
	meta.EffortOverride = cloneStringPtr(cfgState.EffortOverride)
	meta.RuntimeProfile = cfgState.RuntimeProfile
	meta.ToolApprovalMode = toolApprovalMode
	meta.CollaborationMode = modeID
	cfgState = withToolApprovalConfig(cfgState, toolApprovalMode)
	sess := &acpSession{
		id:               id,
		ctrl:             ctrl,
		sink:             sink,
		transcript:       path,
		cwd:              meta.Cwd,
		mcpServers:       clonePluginSpecs(mcpServers),
		model:            cfgState.Model,
		effortOverride:   cloneStringPtr(cfgState.EffortOverride),
		runtimeProfile:   cfgState.RuntimeProfile,
		toolApprovalMode: toolApprovalMode,
		runtimeState:     runtimeState,
		status:           restoreStatusTelemetry(saved.Status),
		modeID:           modeID,
		goalDraftMode:    goalDraftMode,
		title:            meta.Title,
		createdAt:        meta.CreatedAt,
		updatedAt:        meta.UpdatedAt,
		lease:            lease,
	}
	s.bindStatusEvents(sess)
	if err := saveACPMeta(path, sess.meta()); err != nil {
		sess.releaseSessionLease()
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	if replay {
		sink.replay(ctrl.History())
	}
	return enrichStateWithExtensionModels(cfgState, ctrl.ProviderCatalog()), nil
}

// transcriptPath is where a session's transcript lives — keyed by id so
// session/load can recover it. Distinct from the cli's timestamp-labelled
// chat/run session files (those are addressed by a picker, not by id).
func transcriptPath(dir, id string) string {
	return filepath.Join(dir, id+".jsonl")
}

// resolveTranscriptPath returns the transcript file session id currently
// lives in. That is the id-keyed path by default; after a snapshot recovery
// moved the live session onto a recovery branch, the id-keyed sidecar carries
// an ActiveTranscript redirect (written by sessionRecoveredHandler) that
// load/resume/delete/meta lookups must follow, or a restart silently reopens
// the pre-recovery transcript. The redirect is a basename, must stay inside
// dir, and its target must exist and claim the same session id; anything else
// falls back to the id-keyed path.
func resolveTranscriptPath(dir, id string) string {
	path := transcriptPath(dir, id)
	meta, ok, err := loadACPMeta(path)
	if err != nil || !ok {
		return path
	}
	active := strings.TrimSpace(meta.ActiveTranscript)
	if active == "" || active == filepath.Base(path) {
		return path
	}
	if filepath.Base(active) != active {
		return path
	}
	resolved := filepath.Join(dir, active)
	if !sessionFileExists(resolved) {
		return path
	}
	targetMeta, ok, err := loadACPMeta(resolved)
	if err != nil || !ok || targetMeta.SessionID != id {
		return path
	}
	return resolved
}

func (s *service) reloadSessionExtensions(ctx context.Context, sess *acpSession) (any, error) {
	rebuilder, ok := s.factory.(SessionRebuilder)
	if !ok {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: sessionReloadExtensionsMethod + ": runtime reload is unavailable in this session"}
	}
	if !sess.stateChangeMu.TryLock() {
		// A config switch or reload is in maintenance: coalesce one reload
		// behind it; the maintenance owner's post-maintenance drain runs it
		// (mirrors the pendingConfig queue contract in rebuildSession).
		sess.mu.Lock()
		if sess.maintenanceDone != nil && !sess.deleted {
			sess.pendingReload = true
			sess.mu.Unlock()
			return SessionReloadExtensionsResult{Queued: true}, nil
		}
		sess.mu.Unlock()
		sess.stateChangeMu.Lock()
	}
	didMaintenance := false
	res, err := s.reloadSessionExtensionsLocked(ctx, sess, rebuilder, &didMaintenance)
	sess.stateChangeMu.Unlock()
	if didMaintenance {
		s.reportPendingSessionConfigError(ctx, sess, s.applyPendingSessionConfig(ctx, sess), "after maintenance")
		s.drainPendingReload(ctx, sess)
	}
	return res, err
}

// reloadSessionExtensionsLocked is reloadSessionExtensions' body; callers hold
// stateChangeMu. The busy/queue checks and the publish/close ordering mirror
// rebuildSessionLocked, but the build itself goes through the factory's
// boot.Rebuild path instead of NewSession + manual migration.
func (s *service) reloadSessionExtensionsLocked(ctx context.Context, sess *acpSession, rebuilder SessionRebuilder, didMaintenance *bool) (any, error) {
	sess.mu.Lock()
	if sess.deleted {
		sess.mu.Unlock()
		return nil, &RPCError{Code: ErrInvalidRequest, Message: sessionReloadExtensionsMethod + ": session is deleted"}
	}
	status := sess.ctrl.RuntimeStatus()
	if status.PendingPrompt {
		sess.mu.Unlock()
		return nil, sessionConfigActiveWorkError("answer pending prompts before reloading the runtime")
	}
	if !sess.running && !status.Running && status.BackgroundJobs > 0 {
		sess.mu.Unlock()
		return nil, sessionConfigActiveWorkError("stop background jobs before reloading the runtime")
	}
	if sess.running || status.Running || sess.maintenanceDone != nil {
		// Busy: coalesce exactly one reload; finishTurn (or the maintenance
		// owner's post-maintenance drain) runs it once the session is idle.
		sess.pendingReload = true
		sess.mu.Unlock()
		return SessionReloadExtensionsResult{Queued: true}, nil
	}
	// Claim the queued reload and raise maintenance in the same critical
	// section (mirrors rebuildSessionLocked): begin must never observe an
	// idle session between the two.
	sess.pendingReload = false
	cur := sess.ctrl
	sink := sess.sink
	mcpServers := clonePluginSpecs(sess.mcpServers)
	cwd := sess.cwd
	model := sess.model
	effortOverride := cloneStringPtr(sess.effortOverride)
	runtimeProfile := sess.runtimeProfile
	maintenanceDone := make(chan struct{})
	sess.maintenanceDone = maintenanceDone
	*didMaintenance = true
	sess.mu.Unlock()
	defer func() {
		sess.finishMaintenance(maintenanceDone)
	}()

	if err := snapshotACPController(sess, cur); err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: sessionReloadExtensionsMethod + ": snapshot before reload: " + err.Error()}
	}
	// Read the path only after Snapshot: a conflict can retarget cur to a
	// recovery branch, and boot.Rebuild binds the replacement to whatever
	// cur reports now (see rebuildSessionLocked). SessionPath is
	// controller-locked, so reading it off sess.mu is safe.
	prevPath := cur.SessionPath()
	old, ok := cur.(*control.Controller)
	if !ok {
		return nil, &RPCError{Code: ErrInternal, Message: sessionReloadExtensionsMethod + ": session controller does not support rebuild"}
	}
	rebuildParams := SessionParams{
		Cwd:                cwd,
		MCPServers:         mcpServers,
		Sink:               sink,
		Model:              model,
		EffortOverride:     effortOverride,
		RuntimeProfile:     runtimeProfile,
		OnSessionRecovered: s.sessionRecoveredHandler(sess.id),
	}
	// The rebuilt controller must keep the client-capability wiring (fs
	// overlay, host terminal) — mirrors rebuildSessionLocked.
	s.bindClientIO(&rebuildParams, sess.id)
	newCtrl, err := rebuilder.RebuildSession(ctx, rebuildParams, old)
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: sessionReloadExtensionsMethod + ": " + err.Error()}
	}
	newCtrl.EnableInteractiveApproval()
	// Config on disk may have changed the effective planner/sandbox posture;
	// recompute the status snapshot from the same resolved inputs.
	runtimeState, err := s.sessionRuntimeState(ctx, SessionRuntimeStateParams{
		Cwd: cwd, Model: model, RuntimeProfile: runtimeProfile,
	})
	if err != nil {
		newCtrl.ReleaseResources()
		return nil, &RPCError{Code: ErrInternal, Message: sessionReloadExtensionsMethod + ": runtime state: " + err.Error()}
	}
	// Persist before publishing the replacement. If this fails, the outgoing
	// controller and transcript still agree and remain fully usable (mirrors
	// the config switch).
	if err := s.prepareACPReplacementAuthority(sess, newCtrl, cur, prevPath, "snapshot after reload"); err != nil {
		newCtrl.ReleaseResources()
		return nil, &RPCError{Code: ErrInternal, Message: sessionReloadExtensionsMethod + ": " + err.Error()}
	}

	sess.mu.Lock()
	if sess.deleted {
		sess.mu.Unlock()
		newCtrl.ReleaseResources()
		return nil, &RPCError{Code: ErrInvalidRequest, Message: sessionReloadExtensionsMethod + ": session is deleted"}
	}
	if sess.ctrl != cur {
		sess.mu.Unlock()
		newCtrl.ReleaseResources()
		return nil, sessionConfigActiveWorkError("session changed while reloading; retry")
	}
	sess.ctrl = newCtrl
	sess.runtimeState = runtimeState
	if sess.transcript != "" && sessionFileExists(sess.transcript) {
		_ = saveACPMeta(sess.transcript, sess.metaLocked())
	}
	sess.mu.Unlock()
	sink.bindApprove(newCtrl.Approve)
	sink.bindAnswer(newCtrl.AnswerQuestion)

	// Release the outgoing controller only after the swap published the
	// replacement. ReleaseResources (not Close): the session logically
	// continues, so SessionEnd hooks must not fire — mirrors the config
	// switch.
	cur.ReleaseResources()
	// Clients see refreshed plugin commands without waiting for the next turn.
	s.sendAvailableCommands(sess)
	return SessionReloadExtensionsResult{}, nil
}

// drainPendingReload runs the coalesced reloadExtensions request once the
// session is idle. Called from finishTurn and after a config switch's or a
// reload's own maintenance completes; callers must NOT hold stateChangeMu
// (the reload re-acquires it).
func (s *service) drainPendingReload(ctx context.Context, sess *acpSession) {
	if _, ok := s.factory.(SessionRebuilder); !ok {
		return
	}
	sess.mu.Lock()
	if !sess.pendingReload || sess.deleted || sess.running || sess.maintenanceDone != nil || len(sess.pendingConfig) > 0 {
		sess.mu.Unlock()
		return
	}
	sess.mu.Unlock()
	if _, err := s.reloadSessionExtensions(ctx, sess); err != nil {
		s.reportPendingSessionConfigError(ctx, sess, err, "after queued reload")
	}
}

// finishTurn reconciles controller-side drift and drains any config switch
// queued during the turn. Drift must be reconciled before finish() exposes
// the session as idle: a concurrent config switch races on sess.running, and
// if it wins that race while modeID/toolApprovalMode are still stale (a
// slash command or plan/goal completion changed them inside the turn), it
// rebuilds the replacement controller from the outgoing state instead of the
// one this turn actually ended in.
func (s *service) finishTurn(ctx context.Context, sess *acpSession) {
	s.emitModeDrift(sess)
	s.emitToolApprovalDrift(ctx, sess)
	sess.finish()
	s.reportPendingSessionConfigError(ctx, sess, s.applyPendingSessionConfig(ctx, sess), "after turn")
	// A reloadExtensions request queued during the turn runs now that the
	// session may be idle; the drain re-checks busy state.
	s.drainPendingReload(ctx, sess)
	// Re-check after a rebuild in case the replacement normalized state.
	s.emitModeDrift(sess)
	s.emitToolApprovalDrift(ctx, sess)
}

// sessionConfigDelta names exactly one config axis a caller asked to change
// (tool approval never rebuilds the controller, so it has no delta here).
// rebuildSession queues these — instead of a fully resolved SessionConfigState
// — while a turn or rebuild is in flight, one queue entry per axis, and
// applyPendingSessionConfig re-resolves the queued set against the session's
// live baseline once the session is idle. That way a queued change to one axis
// can never restore a stale value on another axis that changed in the
// meantime, whether that axis rebuilt already or is queued alongside.
type sessionConfigDelta struct {
	axis           string
	model          string
	effortOverride *string
	runtimeProfile string
}

func (d sessionConfigDelta) clone() sessionConfigDelta {
	d.effortOverride = cloneStringPtr(d.effortOverride)
	return d
}

func (s *service) rebuildSession(ctx context.Context, sess *acpSession, cfgState SessionConfigState, deltas []sessionConfigDelta) error {
	if !sess.stateChangeMu.TryLock() {
		// Preserve the existing queue contract: a config change arriving during
		// a controller build returns immediately and is applied after that
		// build. The queue keeps one delta per axis (last-write-wins within an
		// axis), so changes queued for different axes never clobber each other.
		// Collaboration/approval changes do not use this queue; they wait for
		// the swap and then update the replacement controller.
		sess.mu.Lock()
		if sess.maintenanceDone != nil && !sess.deleted {
			for _, delta := range deltas {
				sess.pendingConfig = mergePendingConfig(sess.pendingConfig, delta)
			}
			sess.mu.Unlock()
			sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
			return nil
		}
		sess.mu.Unlock()
		sess.stateChangeMu.Lock()
	}
	didMaintenance := false
	err := s.rebuildSessionLocked(ctx, sess, cfgState, deltas, &didMaintenance)
	sess.stateChangeMu.Unlock()
	if didMaintenance {
		pendingErr := s.applyPendingSessionConfig(ctx, sess)
		s.reportPendingSessionConfigError(ctx, sess, pendingErr, "after maintenance")
		// A reloadExtensions request queued behind this maintenance runs next.
		s.drainPendingReload(ctx, sess)
	}
	return err
}

func (s *service) rebuildSessionLocked(ctx context.Context, sess *acpSession, cfgState SessionConfigState, deltas []sessionConfigDelta, didMaintenance *bool) (retErr error) {
	sess.mu.Lock()
	if sess.deleted {
		sess.mu.Unlock()
		return &RPCError{Code: ErrInvalidRequest, Message: "session config: session is deleted"}
	}
	status := sess.ctrl.RuntimeStatus()
	if status.PendingPrompt {
		sess.mu.Unlock()
		return sessionConfigActiveWorkError("answer pending prompts before switching config")
	}
	if !sess.running && !status.Running && status.BackgroundJobs > 0 {
		sess.mu.Unlock()
		return sessionConfigActiveWorkError("stop background jobs before switching config")
	}
	if sess.running || status.Running || sess.maintenanceDone != nil {
		for _, delta := range deltas {
			sess.pendingConfig = mergePendingConfig(sess.pendingConfig, delta)
		}
		sess.mu.Unlock()
		sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
		return nil
	}
	// Claim this rebuild's axes from the queue in the same critical section
	// that raises maintenanceDone below: begin must never observe an idle
	// session between the two. Axes queued by other requests stay queued and
	// are drained by the post-maintenance apply.
	sess.pendingConfig = removePendingAxes(sess.pendingConfig, deltas)

	cur := sess.ctrl
	sink := sess.sink
	mcpServers := clonePluginSpecs(sess.mcpServers)
	cwd := sess.cwd
	modeID := normalizeACPCollaborationMode(sess.modeID)
	goalDraftMode := sess.goalDraftMode
	toolApprovalMode := normalizeACPToolApprovalMode(sess.toolApprovalMode)
	if strings.TrimSpace(cfgState.RuntimeProfile) == "" {
		cfgState.RuntimeProfile = sess.runtimeProfile
	}
	maintenanceDone := make(chan struct{})
	sess.maintenanceDone = maintenanceDone
	*didMaintenance = true
	sess.mu.Unlock()
	defer func() {
		sess.finishMaintenance(maintenanceDone)
	}()

	if err := snapshotACPController(sess, cur); err != nil {
		return &RPCError{Code: ErrInternal, Message: "session config: snapshot before switch: " + err.Error()}
	}
	// Capture the adopt path and history only after Snapshot: a snapshot
	// conflict can retarget cur to a recovery branch (or adopt the newer disk
	// transcript), and a pre-snapshot capture would bind the rebuilt controller
	// back to the original file, re-conflicting on every later save. When that
	// recovery fired, sessionRecoveredHandler already moved sess.transcript
	// and the session lease to the recovery file, so prevPath, the session
	// bookkeeping, and the controller agree on one path here.
	// SessionPath is controller-locked, so reading it off sess.mu is safe.
	prevPath := cur.SessionPath()
	carried := cur.History()
	carriedGoal := ""
	if cur.GoalStatus() == control.GoalStatusRunning {
		carriedGoal = cur.Goal()
	}

	rebuildParams := SessionParams{
		Cwd:                cwd,
		MCPServers:         mcpServers,
		Sink:               sink,
		Model:              cfgState.Model,
		EffortOverride:     cloneStringPtr(cfgState.EffortOverride),
		RuntimeProfile:     cfgState.RuntimeProfile,
		OnSessionRecovered: s.sessionRecoveredHandler(sess.id),
	}
	// The rebuilt controller must keep the client-capability wiring (fs
	// overlay, host terminal) a model/effort switch would otherwise drop.
	s.bindClientIO(&rebuildParams, sess.id)
	newCtrl, err := s.factory.NewSession(ctx, rebuildParams)
	if err != nil {
		return &RPCError{Code: ErrInternal, Message: "session config: " + err.Error()}
	}
	newCtrl.EnableInteractiveApproval()
	runtimeState, err := s.sessionRuntimeState(ctx, SessionRuntimeStateParams{
		Cwd: cwd, Model: cfgState.Model, RuntimeProfile: cfgState.RuntimeProfile,
	})
	if err != nil {
		newCtrl.ReleaseResources()
		return &RPCError{Code: ErrInternal, Message: "session config: runtime state: " + err.Error()}
	}
	// The freshly built controller's own leading system message carries the
	// target profile's contract (see boot/token_profile.go); AdoptHistory below
	// replaces the whole history with carried, so splice that message in first
	// or the model keeps seeing the outgoing profile's contract after every
	// switch.
	if fresh := newCtrl.History(); len(fresh) > 0 && fresh[0].Role == provider.RoleSystem {
		if len(carried) > 0 && carried[0].Role == provider.RoleSystem {
			carried[0] = fresh[0]
		} else {
			carried = append([]provider.Message{fresh[0]}, carried...)
		}
	}
	newCtrl.AdoptHistory(carried, prevPath)
	// Re-apply all three independent session axes. A controller rebuild must not
	// turn Plan into tool approval, drop a running Goal, or reset Ask/Auto/Yolo.
	newCtrl.SetToolApprovalMode(toolApprovalMode)
	switch modeID {
	case sessionModePlan:
		newCtrl.SetPlanMode(true)
	case sessionModeGoal:
		newCtrl.SetPlanMode(false)
		if carriedGoal != "" {
			newCtrl.SetGoal(carriedGoal)
		}
	default:
		newCtrl.SetPlanMode(false)
	}
	// InheritLifecycleFrom wires two concrete controllers' turn/hook state; it's a
	// construction concern, not part of the driving port. cur is always the
	// *control.Controller the factory built for this session, so this is safe.
	if prev, ok := cur.(*control.Controller); ok {
		newCtrl.InheritLifecycleFrom(prev)
		// A rebuild must not force the user to re-approve tools already granted
		// for this session, or re-trust Plan-mode read-only commands already
		// trusted this session.
		newCtrl.RestoreSessionAuthorizations(prev.SessionAuthorizations())
	}
	// Persist before publishing the replacement. If this fails, the outgoing
	// controller and transcript still agree and remain fully usable; publishing
	// first would report a successful switch whose refreshed profile contract
	// disappears on restart. AdoptHistory preserves the loaded CAS baseline, so
	// this compatible leading-system rewrite is safe to snapshot here.
	if err := s.prepareACPReplacementAuthority(sess, newCtrl, cur, prevPath, "snapshot after switch"); err != nil {
		newCtrl.ReleaseResources()
		return &RPCError{Code: ErrInternal, Message: "session config: " + err.Error()}
	}

	sess.mu.Lock()
	if sess.deleted {
		sess.mu.Unlock()
		newCtrl.ReleaseResources()
		return &RPCError{Code: ErrInvalidRequest, Message: "session config: session is deleted"}
	}
	if sess.ctrl != cur {
		sess.mu.Unlock()
		newCtrl.ReleaseResources()
		return sessionConfigActiveWorkError("session changed while switching config; retry")
	}
	sess.ctrl = newCtrl
	sess.model = cfgState.Model
	sess.effortOverride = cloneStringPtr(cfgState.EffortOverride)
	sess.runtimeProfile = cfgState.RuntimeProfile
	sess.toolApprovalMode = toolApprovalMode
	sess.runtimeState = runtimeState
	sess.modeID = modeID
	sess.goalDraftMode = goalDraftMode
	if sess.transcript != "" && sessionFileExists(sess.transcript) {
		_ = saveACPMeta(sess.transcript, sess.metaLocked())
	}
	sess.mu.Unlock()
	sink.bindApprove(newCtrl.Approve)
	sink.bindAnswer(newCtrl.AnswerQuestion)

	cur.ReleaseResources()
	s.sendAvailableCommands(sess)
	sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
	return nil
}

type activeSessionConfigWorkError struct {
	*RPCError
}

func (e *activeSessionConfigWorkError) Unwrap() error {
	return e.RPCError
}

func (s *service) takeSession(id string) *acpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	delete(s.sessions, id)
	return sess
}

func (s *service) liveSessions() []*acpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*acpSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func clonePluginSpecs(in []plugin.Spec) []plugin.Spec {
	if len(in) == 0 {
		return nil
	}
	out := make([]plugin.Spec, len(in))
	copy(out, in)
	return out
}

func (s *service) resolveSessionCwd(cwd, sessionID string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		if !filepath.IsAbs(cwd) {
			return "", fmt.Errorf("cwd must be an absolute path")
		}
		return filepath.Clean(cwd), nil
	}
	if sessionID != "" {
		if meta, ok := s.loadMeta(sessionID); ok && meta.Cwd != "" {
			if !filepath.IsAbs(meta.Cwd) {
				return "", fmt.Errorf("stored cwd must be an absolute path")
			}
			return filepath.Clean(meta.Cwd), nil
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	return wd, nil
}

// closeAll tears down every open session (aborting any in-flight turn and
// stopping its MCP subprocesses) when the connection ends.
func (s *service) closeAll() {
	s.mu.Lock()
	sessions := s.sessions
	s.sessions = make(map[string]*acpSession)
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.abortAndWait()
		sess.currentCtrl().Close()
		sess.releaseSessionLease()
	}
}

func (s *acpSession) persistAfterTurn(prompt string) {
	s.mu.Lock()
	if s.deleted {
		s.mu.Unlock()
		return
	}
	ctrl := s.ctrl
	s.mu.Unlock()

	_ = snapshotACPController(s, ctrl)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted || s.ctrl != ctrl {
		return
	}
	if s.title == "" {
		s.title = previewTitle(prompt)
	}
	s.updatedAt = time.Now().UTC()
	if s.createdAt.IsZero() {
		s.createdAt = s.updatedAt
	}
	if s.transcript != "" && sessionFileExists(s.transcript) {
		_ = saveACPMeta(s.transcript, s.metaLocked())
	}
}

func (s *acpSession) info() SessionInfo {
	meta := s.meta()
	ctrl := s.currentCtrl()
	extra := map[string]any{}
	if n := len(ctrl.History()); n > 0 {
		extra["messageCount"] = n
	}
	if len(extra) == 0 {
		extra = nil
	}
	return meta.info(extra)
}

func (s *service) sendAvailableCommands(sess *acpSession) {
	if sess == nil {
		return
	}
	ctrl := sess.currentCtrl()
	if ctrl == nil {
		return
	}
	cmds := availableCommandsFor(ctrl)
	if len(cmds) == 0 {
		return
	}
	sess.sink.send(availableCommandsUpdate{
		SessionUpdate:     "available_commands_update",
		AvailableCommands: cmds,
	})
}

func availableCommandsFor(ctrl acpController) []AvailableCommand {
	if ctrl == nil {
		return nil
	}
	byName := map[string]AvailableCommand{}
	for _, cmd := range ctrl.Commands() {
		if cmd.Hidden {
			continue
		}
		name := strings.TrimSpace(cmd.Name)
		if name == "" {
			continue
		}
		desc := strings.TrimSpace(cmd.Description)
		if desc == "" {
			desc = "Run the " + name + " command"
		}
		ac := AvailableCommand{Name: name, Description: desc}
		if hint := strings.TrimSpace(cmd.ArgHint); hint != "" {
			ac.Input = &AvailableCommandInput{Hint: hint}
		}
		byName[name] = ac
	}
	for _, sk := range ctrl.SlashSkills() {
		name := strings.TrimSpace(sk.SlashName())
		if name == "" {
			continue
		}
		if _, exists := byName[name]; exists {
			continue
		}
		desc := strings.TrimSpace(sk.Description)
		if desc == "" {
			desc = "Run the " + name + " skill"
		}
		byName[name] = AvailableCommand{
			Name:        name,
			Description: desc,
			Input:       &AvailableCommandInput{Hint: "instructions"},
		}
	}
	if host := ctrl.Host(); host != nil {
		for _, prompt := range host.Prompts() {
			name := strings.TrimSpace(prompt.Name)
			if name == "" {
				continue
			}
			desc := strings.TrimSpace(prompt.Description)
			if desc == "" {
				desc = "Run the " + name + " MCP prompt"
			}
			ac := AvailableCommand{Name: name, Description: desc}
			if len(prompt.Args) > 0 {
				ac.Input = &AvailableCommandInput{Hint: "arguments"}
			}
			byName[name] = ac
		}
	}
	// Extension actions surface as "<plugin>:<action>" commands so ACP clients
	// can discover them in the slash menu alongside commands/skills/prompts.
	for _, action := range ctrl.ExtensionActions() {
		name := strings.TrimPrefix(strings.TrimSpace(action.Slash), "/")
		if name == "" {
			continue
		}
		if _, exists := byName[name]; exists {
			continue
		}
		desc := strings.TrimSpace(action.Label)
		if desc == "" {
			desc = "Run the " + name + " extension action"
		}
		byName[name] = AvailableCommand{
			Name:        name,
			Description: desc,
			Input:       &AvailableCommandInput{Hint: "arguments"},
		}
	}
	out := make([]AvailableCommand, 0, len(byName))
	for _, cmd := range byName {
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *service) resolveSlashPrompt(ctx context.Context, sess *acpSession, text string) string {
	line := strings.TrimSpace(text)
	if sess == nil || !strings.HasPrefix(line, "/") {
		return text
	}
	ctrl := sess.currentCtrl()
	if ctrl == nil {
		return text
	}
	if sent, ok := ctrl.CustomCommand(line); ok {
		return sent
	}
	if sent, ok := ctrl.RunSkill(line); ok {
		return sent
	}
	if sent, ok, err := ctrl.MCPPrompt(ctx, line); err == nil && ok {
		return sent
	}
	if sent, ok := invokeExtensionAction(ctx, ctrl, line); ok {
		return sent
	}
	return text
}

// invokeExtensionAction resolves a "/<plugin>:<action> args…" line against the
// handshake-declared extension actions and invokes it — the last resolution
// step in resolveSlashPrompt, after custom commands, skills, and MCP prompts.
// The extension's result message becomes the prompt text. A parse miss, an
// undeclared action, an invocation error, or an empty result all leave the
// line untouched (ok=false), matching how unknown slash commands fall through.
func invokeExtensionAction(ctx context.Context, ctrl acpController, line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	pluginID, actionID, ok := uihub.ParseSlashName(fields[0])
	if !ok {
		return "", false
	}
	declared := false
	for _, action := range ctrl.ExtensionActions() {
		if action.PluginID == pluginID && action.ActionID == actionID {
			declared = true
			break
		}
	}
	if !declared {
		return "", false
	}
	message, err := ctrl.InvokeExtensionAction(ctx, fields[0], control.ParseExtensionActionArgs(fields[1:]))
	if err != nil || strings.TrimSpace(message) == "" {
		return "", false
	}
	return message, true
}

type acpSessionMeta struct {
	SessionID         string                    `json:"sessionId"`
	Cwd               string                    `json:"cwd"`
	Model             string                    `json:"model,omitempty"`
	EffortOverride    *string                   `json:"effortOverride,omitempty"`
	RuntimeProfile    string                    `json:"runtimeProfile,omitempty"`
	ToolApprovalMode  string                    `json:"toolApprovalMode,omitempty"`
	CollaborationMode string                    `json:"collaborationMode,omitempty"`
	Title             string                    `json:"title,omitempty"`
	CreatedAt         time.Time                 `json:"createdAt"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
	Status            *persistedStatusTelemetry `json:"status,omitempty"`
	// ActiveTranscript, when set on the id-keyed sidecar, is the basename of
	// the transcript this session currently lives in: a snapshot recovery
	// moved the live session onto a recovery branch and left this redirect
	// behind so restart-time lookups (resolveTranscriptPath) follow the
	// session instead of reopening the pre-recovery file.
	ActiveTranscript string `json:"activeTranscript,omitempty"`
}

func (m acpSessionMeta) info(extra map[string]any) SessionInfo {
	updatedAt := ""
	if !m.UpdatedAt.IsZero() {
		updatedAt = m.UpdatedAt.Format(time.RFC3339Nano)
	}
	return SessionInfo{
		SessionID: m.SessionID,
		Cwd:       m.Cwd,
		Title:     m.Title,
		UpdatedAt: updatedAt,
		Meta:      extra,
	}
}

func validateSessionID(method, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return &RPCError{Code: ErrInvalidParams, Message: method + ": missing sessionId"}
	}
	if trimmed != id || trimmed == "." || trimmed == ".." || !isSafeSessionID(trimmed) {
		return &RPCError{Code: ErrInvalidParams, Message: method + ": invalid sessionId"}
	}
	return nil
}

func isSafeSessionID(id string) bool {
	for _, r := range id {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func deleteSessionFiles(sessionPath string) error {
	if err := store.RemoveSessionArtifacts(sessionPath, acpMetaPath(sessionPath)); err != nil {
		return err
	}
	if dir := checkpointPath(sessionPath); dir != "" {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := delegation.DeleteSubagentsByParent(filepath.Dir(sessionPath), sessionstore.BranchID(sessionPath)); err != nil {
		return err
	}
	if err := jobs.RemoveArtifacts(sessionPath); err != nil {
		return err
	}
	return sessionstore.ClearCleanupPending(sessionPath)
}

// ReconcileCleanupPending retries delayed ACP session cleanup left by a previous
// process, including ACP's own metadata sidecar.
func ReconcileCleanupPending(dir string) error {
	return sessionstore.ReconcileCleanupPending(dir, func(item sessionstore.CleanupPendingInfo) error {
		return deleteSessionFiles(item.SessionPath)
	})
}

func delayedDeleteSessionFiles(sessionPath string, destroy control.SessionDestroyHandle) {
	if destroy.WaitAll != nil {
		destroy.WaitAll()
	}
	if err := deleteSessionFiles(sessionPath); err != nil {
		slog.Warn("acp: delayed session delete failed", "path", sessionPath, "err", err)
	}
	if destroy.Finish != nil {
		destroy.Finish()
	}
}

func checkpointPath(sessionPath string) string {
	return store.SessionCheckpointDir(sessionPath)
}

// mcpSpecs converts ACP MCP server declarations to plugin.Spec.
func mcpSpecs(in []MCPServerSpec, cwd string) ([]plugin.Spec, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]plugin.Spec, 0, len(in))
	for _, m := range in {
		typ := strings.ToLower(strings.TrimSpace(m.Type))
		if typ == "" {
			typ = "stdio"
		}
		if strings.TrimSpace(m.Name) == "" {
			return nil, fmt.Errorf("MCP server name is required")
		}
		switch typ {
		case "stdio":
			if strings.TrimSpace(m.Command) == "" {
				return nil, fmt.Errorf("MCP server %q command is required", m.Name)
			}
		case "http", "streamable-http", "streamable_http", "sse":
			if strings.TrimSpace(m.URL) == "" {
				return nil, fmt.Errorf("MCP server %q url is required", m.Name)
			}
			if typ != "sse" {
				typ = "http"
			}
		default:
			return nil, fmt.Errorf("MCP server %q uses unsupported transport %q", m.Name, m.Type)
		}
		out = append(out, plugin.Spec{
			Name:          strings.TrimSpace(m.Name),
			Type:          typ,
			Command:       strings.TrimSpace(m.Command),
			Args:          append([]string(nil), m.Args...),
			Env:           mapString(m.Env),
			URL:           strings.TrimSpace(m.URL),
			Headers:       mapString(m.Headers),
			Dir:           cwd,
			WorkspaceRoot: cwd,
		})
	}
	return out, nil
}

func mapString(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

// newSessionID returns a random RFC 4122 v4 UUID string used to address a session.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
