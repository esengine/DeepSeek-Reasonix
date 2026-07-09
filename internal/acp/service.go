package acp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/fileutil"
	"reasonix/internal/jobs"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool/builtin"
)

// SessionParams is everything a Factory needs to assemble one ACP session's
// controller. Sink is owned by this package (an updateSink bound to the session
// id) and must be wired into the controller's event sink; the controller's
// interactive approval (see control.Controller.EnableInteractiveApproval) then
// routes "ask" decisions back through that sink as ApprovalRequest events, which
// the sink forwards to the client over session/request_permission.
//
// Cwd roots the session's file tools and bash (built via builtin.Workspace).
// Model and EffortOverride are optional session-local provider selectors from
// ACP config options. MCPServers are the MCP servers the client asked the agent
// to connect for this session. OnSessionRecovered is the service's bookkeeping
// hook for automatic transcript recovery branches (see sessionRecoveredHandler);
// factories must wire it into the controller they build.
type SessionParams struct {
	Cwd                string
	MCPServers         []plugin.Spec
	Sink               event.Sink
	Model              string
	EffortOverride     *string
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
// selectors. Empty Model means the Factory should use its configured default.
// Nil EffortOverride means provider config wins; a non-nil empty string means
// provider default for this session.
type SessionConfigStateParams struct {
	Cwd            string
	Model          string
	EffortOverride *string
}

// SessionConfigState is the complete ACP-visible config state for a session.
type SessionConfigState struct {
	Model          string
	EffortOverride *string
	Models         *SessionModelState
	ConfigOptions  []SessionConfigOption
}

// SessionConfigStateProvider lets a Factory expose model and effort selectors
// without making the ACP transport depend on a concrete config backend.
type SessionConfigStateProvider interface {
	SessionConfigState(ctx context.Context, p SessionConfigStateParams) (SessionConfigState, error)
}

// SessionDirProvider lets a Factory expose the persistent session directory
// without forcing session/list to build a controller first.
type SessionDirProvider interface {
	SessionDir() string
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

// acpController is the slice of the controller's driving port the ACP transport
// drives: session lifecycle + persistence, turn execution, interactive approval,
// and the capability surface (commands/skills/MCP prompts). ACP never touches
// goals, checkpoints, or memory, so it depends on those sub-ports only — not the
// concrete *control.Controller.
type acpController interface {
	control.Lifecycle
	control.TurnControl
	control.Approvals
	control.Capabilities
	control.SessionPersistence
	// Goals is included for the session-mode surface only (PlanMode read-back
	// after a turn); the goal FSM itself is not driven over ACP.
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
	effortOverride *string
	// modeID is the ACP session mode last reported to the client (default |
	// plan | auto). Guarded by mu; compared after each turn so controller-side
	// flips (plan mode auto-exit) surface as current_mode_update.
	modeID        string
	pendingConfig *SessionConfigState
	title         string
	createdAt     time.Time
	updatedAt     time.Time

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	running bool
	deleted bool
	// lease is the session lease guarding transcript against other runtimes
	// (a desktop window, the CLI) for the life of this session. Held from
	// session/new / session/load and released on close/delete/teardown.
	// Config rebuilds keep the same transcript; when a snapshot conflict
	// retargets the controller to a recovery branch, sessionRecoveredHandler
	// moves transcript and this lease to the recovery file at commit time.
	lease *agent.SessionLease
	// maintenanceDone is non-nil while session-owned maintenance, such as an
	// idle config rebuild, is in flight outside mu.
	maintenanceDone chan struct{}
}

func (s *acpSession) begin(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	// A queued pendingConfig blocks new turns so a prompt never runs on the
	// outgoing config. The turn or maintenance that queued it applies it from
	// its defer, so no new turn is needed to drain the queue.
	if s.running || s.deleted || s.maintenanceDone != nil || s.pendingConfig != nil {
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

// swapModeID records the mode reported to the client and returns the previous
// value, so callers can emit current_mode_update only on change.
func (s *acpSession) swapModeID(id string) (old string) {
	s.mu.Lock()
	old = s.modeID
	s.modeID = id
	s.mu.Unlock()
	return old
}

// currentModeID returns the mode last reported to the client.
func (s *acpSession) currentModeID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.modeID == "" {
		return sessionModeDefault
	}
	return s.modeID
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
}

// sessionLeaseBindError maps a lease-acquisition failure to the protocol
// error the client sees: a held session names its holder with the shared CLI
// wording; anything else is an internal error.
func sessionLeaseBindError(method string, err error) *RPCError {
	if errors.Is(err, agent.ErrSessionLeaseHeld) {
		return &RPCError{
			Code:    ErrInvalidRequest,
			Message: method + ": " + control.SessionInUseMessage(err) + "; " + control.SessionLeaseCloseHint,
		}
	}
	return &RPCError{Code: ErrInternal, Message: method + ": session lease: " + err.Error()}
}

// sessionRecoveredHandler returns the OnSessionRecovered callback wired into
// every controller built for session id. When a snapshot conflict retargets
// the controller to a recovery branch (turn-end autosave in persistAfterTurn,
// or the pre-rebuild snapshot in rebuildSession), the ACP bookkeeping must
// follow at commit time: session/prompt reports sess.transcript,
// session/delete destroys it, and the session lease must guard the file the
// controller actually writes. The recovery lease is acquired before the old
// one is released so the outgoing transcript stays guarded until the new one
// is secured; a failure aborts the recovery commit and the controller stays
// on the original path (the next save retries).
func (s *service) sessionRecoveredHandler(id string) func(control.SessionRecoveryInfo) error {
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
		sess.lease = lease
		sess.transcript = recoveryPath
		meta := sess.metaLocked()
		sess.mu.Unlock()
		if old != nil {
			old.Release()
		}
		_ = saveACPMeta(recoveryPath, meta)
		// Leave a redirect on the id-keyed sidecar so restart-time lookups
		// (session/load, session/resume, session/delete, loadMeta) resolve the
		// id to the recovery file; without it the next process reopens the
		// pre-recovery transcript. Always written against the id-keyed path,
		// so resolution stays a single hop even for recovery-of-recovery.
		if dir := s.sessionDir(); dir != "" {
			if idPath := transcriptPath(dir, id); idPath != recoveryPath {
				idMeta, _, err := loadACPMeta(idPath)
				if err != nil {
					slog.Warn("acp: load id-keyed meta for recovery redirect", "err", err)
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
				idMeta.ActiveTranscript = filepath.Base(recoveryPath)
				if err := saveACPMeta(idPath, idMeta); err != nil {
					slog.Warn("acp: save recovery redirect", "err", err)
				}
			}
		}
		return nil
	}
}

// initialize advertises the agent's capability set: persisted load plus ACP v1
// list/resume/close/delete lifecycle helpers, prompts carrying inline resource
// text (embeddedContext) but not image/audio, and stdio / Streamable HTTP MCP
// (no legacy sse).
func (s *service) initialize(_ context.Context, raw json.RawMessage) (any, error) {
	var p InitializeParams
	if len(raw) > 0 && json.Unmarshal(raw, &p) == nil {
		s.setClientCapabilities(p.ClientCapabilities)
	}
	return InitializeResult{
		ProtocolVersion: ProtocolVersion,
		AgentCapabilities: AgentCapabilities{
			LoadSession: true,
			SessionCapabilities: SessionCapabilities{
				List:   &EmptyCapability{},
				Resume: &EmptyCapability{},
				Close:  &EmptyCapability{},
				Delete: &EmptyCapability{},
			},
			PromptCapabilities: PromptCapabilities{
				Image:           false,
				Audio:           false,
				EmbeddedContext: true,
			},
			MCPCapabilities: MCPCapabilities{HTTP: true, SSE: false},
		},
		AgentInfo:   Implementation{Name: s.info.Name, Version: s.info.Version},
		AuthMethods: []AuthMethod{reasonixSetupAuthMethod()},
	}, nil
}

func reasonixSetupAuthMethod() AuthMethod {
	return AuthMethod{
		ID:          "reasonix-setup",
		Name:        "Reasonix setup",
		Description: "Configure Reasonix providers and credentials in a terminal",
		Type:        "terminal",
		Args:        []string{"setup"},
	}
}

func (s *service) authenticate(_ context.Context, raw json.RawMessage) (any, error) {
	var p AuthenticateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "authenticate: " + err.Error()}
	}
	if strings.TrimSpace(p.MethodID) != reasonixSetupAuthMethod().ID {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "authenticate: unknown methodId " + p.MethodID}
	}
	return AuthenticateResult{}, nil
}

// sessionNew opens a session: it mints an id, builds the session's sink bound to
// that id, asks the Factory to assemble the controller, switches the controller
// to interactive approval (so tool gates surface as ApprovalRequest events the
// sink forwards), and registers it.
func (s *service) sessionNew(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionNewParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, &RPCError{Code: ErrInvalidParams, Message: "session/new: " + err.Error()}
		}
	}
	cwd, err := s.resolveSessionCwd(p.Cwd, "")
	if err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/new: " + err.Error()}
	}
	mcpServers, err := mcpSpecs(p.MCPServers, cwd)
	if err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/new: " + err.Error()}
	}
	cfgState, err := s.sessionConfigState(ctx, SessionConfigStateParams{Cwd: cwd})
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
	}

	id, err := newSessionID()
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
	}

	sink := newUpdateSink(s.conn, id)
	sink.bindCwd(cwd)
	sessionParams := SessionParams{
		Cwd:                cwd,
		MCPServers:         mcpServers,
		Sink:               sink,
		Model:              cfgState.Model,
		EffortOverride:     cloneStringPtr(cfgState.EffortOverride),
		OnSessionRecovered: s.sessionRecoveredHandler(id),
	}
	s.bindClientIO(&sessionParams, id)
	ctrl, err := s.factory.NewSession(ctx, sessionParams)
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
	}
	ctrl.EnableInteractiveApproval()
	sink.bindApprove(ctrl.Approve)
	sink.bindAnswer(ctrl.AnswerQuestion)

	now := time.Now().UTC()
	sess := &acpSession{
		id:             id,
		ctrl:           ctrl,
		sink:           sink,
		cwd:            cwd,
		mcpServers:     clonePluginSpecs(mcpServers),
		model:          cfgState.Model,
		effortOverride: cloneStringPtr(cfgState.EffortOverride),
		modeID:         sessionModeDefault,
		createdAt:      now,
		updatedAt:      now,
	}
	// Pin a transcript file keyed by session id when the controller has a session
	// dir, so every turn auto-saves there, session/prompt can hand the path back,
	// and session/load can find it again by id across process restarts. The
	// session lease is taken with it (defensive: the id-keyed path is brand new)
	// so no other runtime can bind the transcript while this session lives.
	if dir := ctrl.SessionDir(); dir != "" {
		sess.transcript = transcriptPath(dir, id)
		lease, err := agent.TryAcquireSessionLease(sess.transcript)
		if err != nil {
			ctrl.Close()
			return nil, sessionLeaseBindError("session/new", err)
		}
		sess.lease = lease
		ctrl.SetSessionPath(sess.transcript)
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	s.sendAvailableCommands(sess)

	return SessionNewResult{
		SessionID:     id,
		Models:        cfgState.Models,
		Modes:         sessionModesState(sessionModeDefault),
		ConfigOptions: cfgState.ConfigOptions,
	}, nil
}

// Session modes exposed over ACP, mapped onto the controller's approval
// switches: default asks per write-capable call, plan flips the read-only plan
// gate, auto approves tool calls without asking.
const (
	sessionModeDefault = "default"
	sessionModePlan    = "plan"
	sessionModeAuto    = "auto"
)

func sessionModesState(current string) *SessionModeState {
	return &SessionModeState{
		CurrentModeID: current,
		AvailableModes: []SessionMode{
			{ID: sessionModeDefault, Name: "Always Ask", Description: "Ask before write-capable tool calls"},
			{ID: sessionModePlan, Name: "Plan", Description: "Read-only research and planning; no writes or commands"},
			{ID: sessionModeAuto, Name: "Auto-Approve", Description: "Run tool calls without asking"},
		},
	}
}

// sessionSetMode switches the session's operating mode and confirms it with a
// current_mode_update, per the ACP session-mode contract.
func (s *service) sessionSetMode(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionSetModeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_mode: " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_mode: unknown session " + p.SessionID}
	}
	ctrl := sess.currentCtrl()
	switch p.ModeID {
	case sessionModeDefault:
		ctrl.SetMode(false, false)
	case sessionModePlan:
		ctrl.SetMode(true, false)
	case sessionModeAuto:
		ctrl.SetMode(false, true)
	default:
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_mode: unknown modeId " + p.ModeID}
	}
	if sess.swapModeID(p.ModeID) != p.ModeID {
		sess.sink.send(currentModeUpdate{SessionUpdate: "current_mode_update", CurrentModeID: p.ModeID})
	}
	return SessionSetModeResult{}, nil
}

// emitModeDrift reports controller-side mode flips (plan mode auto-exits when
// a plan is approved, a config rebuild resets switches) as current_mode_update
// so the client's mode picker stays truthful.
func (s *service) emitModeDrift(sess *acpSession) {
	ctrl := sess.currentCtrl()
	current := sessionModeDefault
	switch {
	case ctrl.PlanMode():
		current = sessionModePlan
	case ctrl.AutoApproveTools():
		current = sessionModeAuto
	}
	if sess.swapModeID(current) != current {
		sess.sink.send(currentModeUpdate{SessionUpdate: "current_mode_update", CurrentModeID: current})
	}
}

// sessionLoad resumes a previously-saved session by id: it builds a controller
// (rooted at the requested cwd), seeds it from the on-disk transcript, replays
// the conversation to the client as session/update notifications, and registers
// it for subsequent prompts. A session already live in this process is replayed
// from memory without rebuilding.
func (s *service) sessionLoad(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionLoadParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/load: " + err.Error()}
	}
	cfgState, err := s.openExistingSession(ctx, "session/load", p.SessionID, p.Cwd, p.MCPServers, true)
	if err != nil {
		return nil, err
	}
	return SessionLoadResult{Models: cfgState.Models, Modes: s.sessionModesFor(p.SessionID), ConfigOptions: cfgState.ConfigOptions}, nil
}

// sessionModesFor reports the modes state for a just-opened session: a session
// already live in this process keeps whatever mode the user put it in (plan /
// auto), so load/resume must not tell a reconnecting client "default".
func (s *service) sessionModesFor(id string) *SessionModeState {
	if sess := s.session(id); sess != nil {
		return sessionModesState(sess.currentModeID())
	}
	return sessionModesState(sessionModeDefault)
}

// sessionResume restores a previously-saved session without replaying its
// conversation history to the client.
func (s *service) sessionResume(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionResumeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/resume: " + err.Error()}
	}
	cfgState, err := s.openExistingSession(ctx, "session/resume", p.SessionID, p.Cwd, p.MCPServers, false)
	if err != nil {
		return nil, err
	}
	return SessionResumeResult{Models: cfgState.Models, Modes: s.sessionModesFor(p.SessionID), ConfigOptions: cfgState.ConfigOptions}, nil
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
		if agent.IsCleanupPending(sess.transcript) {
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
		if agent.IsCleanupPending(persistedPath) {
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
	}
	cfgState, err := s.sessionConfigState(ctx, cfgParams)
	if err != nil && (strings.TrimSpace(saved.Model) != "" || saved.EffortOverride != nil) {
		cfgState, err = s.sessionConfigState(ctx, SessionConfigStateParams{Cwd: cwd})
	}
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
	}

	sink := newUpdateSink(s.conn, id)
	sink.bindCwd(cwd)
	sessionParams := SessionParams{
		Cwd:                cwd,
		MCPServers:         mcpServers,
		Sink:               sink,
		Model:              cfgState.Model,
		EffortOverride:     cloneStringPtr(cfgState.EffortOverride),
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
	if path != persistedPath && agent.IsCleanupPending(path) {
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": unknown session " + id}
	}
	// Bind the transcript for writing only if no other runtime (a desktop
	// window, the CLI) holds it; the editor should not silently double-write a
	// session that is open elsewhere.
	lease, leaseErr := agent.TryAcquireSessionLease(path)
	if leaseErr != nil {
		ctrl.Close()
		return SessionConfigState{}, sessionLeaseBindError(method, leaseErr)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		lease.Release()
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": unknown session " + id}
	}
	ctrl.Resume(loaded, path)

	meta := metadataForLoadedSession(path, id, cwd, ctrl.History())
	meta.Model = cfgState.Model
	meta.EffortOverride = cloneStringPtr(cfgState.EffortOverride)
	sess := &acpSession{
		id:             id,
		ctrl:           ctrl,
		sink:           sink,
		transcript:     path,
		cwd:            meta.Cwd,
		mcpServers:     clonePluginSpecs(mcpServers),
		model:          cfgState.Model,
		effortOverride: cloneStringPtr(cfgState.EffortOverride),
		modeID:         sessionModeDefault,
		title:          meta.Title,
		createdAt:      meta.CreatedAt,
		updatedAt:      meta.UpdatedAt,
		lease:          lease,
	}
	if err := saveACPMeta(path, sess.meta()); err != nil {
		sess.releaseSessionLease()
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	s.sendAvailableCommands(sess)

	if replay {
		sink.replay(ctrl.History())
	}
	return cfgState, nil
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

// sessionPrompt runs one turn. It flattens the prompt blocks to text and runs the
// session's controller synchronously under a per-turn cancelable context (so
// session/cancel can stop it), then reports why the turn ended. The controller
// streams the turn's events to the session's sink as it runs.
func (s *service) sessionPrompt(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionPromptParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/prompt: " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/prompt: unknown session " + p.SessionID}
	}
	text := FlattenPrompt(p.Prompt)
	if text == "" {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/prompt: empty prompt"}
	}
	text = s.resolveSlashPrompt(ctx, sess, text)

	runCtx, cancel, ok := sess.begin(ctx)
	if !ok {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: "session/prompt: session already has an active prompt"}
	}
	sess.sink.setTurnContext(runCtx)
	defer func() {
		sess.sink.clearTurnContext()
		sess.finish()
		s.reportPendingSessionConfigError(ctx, sess, s.applyPendingSessionConfig(ctx, sess), "after turn")
		s.emitModeDrift(sess)
		cancel()
	}()
	runErr := sess.ctrl.RunTurn(runCtx, text)

	// Persist after the turn (best-effort) so a crash loses at most this prompt;
	// save even on cancel/error since the partial conversation is still resumable.
	sess.persistAfterTurn(text)

	stop := StopEndTurn
	if runErr != nil {
		if runCtx.Err() != nil {
			stop = StopCancelled
		} else {
			stop = StopError
		}
	}
	res := SessionPromptResult{StopReason: stop}
	if sess.transcript != "" {
		res.TranscriptPath = &sess.transcript
	}
	return res, nil
}

// sessionSetConfigOption applies ACP's generic session-level selector. Reasonix
// currently exposes model and reasoning-effort selectors through this path.
func (s *service) sessionSetConfigOption(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SetSessionConfigOptionParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_config_option: " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_config_option: unknown session " + p.SessionID}
	}
	cfgState, err := s.configStateForSession(ctx, sess)
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/set_config_option: " + err.Error()}
	}
	option, ok := findConfigOption(cfgState.ConfigOptions, p.ConfigID)
	if !ok {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_config_option: unknown config option " + p.ConfigID}
	}
	if !configOptionHasValue(option, p.Value) {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_config_option: invalid value " + p.Value + " for " + option.ID}
	}

	var next SessionConfigState
	switch configOptionCategory(option) {
	case "model":
		next, err = s.switchSessionModel(ctx, sess, p.Value)
	case "thought_level":
		next, err = s.switchSessionEffort(ctx, sess, p.Value)
	default:
		err = &RPCError{Code: ErrInvalidParams, Message: "session/set_config_option: unsupported config option " + option.ID}
	}
	if err != nil {
		return nil, err
	}
	return SetSessionConfigOptionResult{ConfigOptions: next.ConfigOptions}, nil
}

// sessionSetModel keeps older ACP clients working while configOptions becomes
// the preferred model selector.
func (s *service) sessionSetModel(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SetSessionModelParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_model: " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_model: unknown session " + p.SessionID}
	}
	if _, err := s.switchSessionModel(ctx, sess, p.ModelID); err != nil {
		return nil, err
	}
	return SetSessionModelResult{}, nil
}

func (s *service) switchSessionModel(ctx context.Context, sess *acpSession, modelID string) (SessionConfigState, error) {
	params := sess.configStateParams()
	params.Model = modelID
	cfgState, err := s.sessionConfigState(ctx, params)
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: "session/set_model: " + err.Error()}
	}
	if cfgState.Model == "" {
		return SessionConfigState{}, &RPCError{Code: ErrInvalidRequest, Message: "session/set_model: model switching is unavailable in this session"}
	}
	if err := s.rebuildSession(ctx, sess, cfgState); err != nil {
		return SessionConfigState{}, err
	}
	return cfgState, nil
}

func (s *service) switchSessionEffort(ctx context.Context, sess *acpSession, effort string) (SessionConfigState, error) {
	params := sess.configStateParams()
	level := strings.TrimSpace(effort)
	if level == "auto" {
		level = ""
	}
	params.EffortOverride = &level
	cfgState, err := s.sessionConfigState(ctx, params)
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: "session/set_config_option: " + err.Error()}
	}
	if err := s.rebuildSession(ctx, sess, cfgState); err != nil {
		return SessionConfigState{}, err
	}
	return cfgState, nil
}

func (s *service) rebuildSession(ctx context.Context, sess *acpSession, cfgState SessionConfigState) (retErr error) {
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
		pending := cloneSessionConfigState(cfgState)
		sess.pendingConfig = &pending
		sess.mu.Unlock()
		sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
		return nil
	}
	// Claim the queue in the same critical section that raises maintenanceDone
	// below: begin must never observe an idle session between the two.
	sess.pendingConfig = nil

	cur := sess.ctrl
	sink := sess.sink
	mcpServers := clonePluginSpecs(sess.mcpServers)
	cwd := sess.cwd
	maintenanceDone := make(chan struct{})
	sess.maintenanceDone = maintenanceDone
	sess.mu.Unlock()
	defer func() {
		sess.finishMaintenance(maintenanceDone)
		if retErr == nil {
			s.reportPendingSessionConfigError(ctx, sess, s.applyPendingSessionConfig(ctx, sess), "after maintenance")
		}
	}()

	if err := cur.Snapshot(); err != nil {
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

	rebuildParams := SessionParams{
		Cwd:                cwd,
		MCPServers:         mcpServers,
		Sink:               sink,
		Model:              cfgState.Model,
		EffortOverride:     cloneStringPtr(cfgState.EffortOverride),
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
	sink.bindApprove(newCtrl.Approve)
	sink.bindAnswer(newCtrl.AnswerQuestion)
	newCtrl.AdoptHistory(carried, prevPath)
	// Re-apply the session's ACP mode: a fresh controller boots with default
	// switches, which would silently drop a user-selected plan/auto mode and
	// desynchronize the client's mode picker.
	switch sess.currentModeID() {
	case sessionModePlan:
		newCtrl.SetMode(true, false)
	case sessionModeAuto:
		newCtrl.SetMode(false, true)
	}
	// InheritLifecycleFrom wires two concrete controllers' turn/hook state; it's a
	// construction concern, not part of the driving port. cur is always the
	// *control.Controller the factory built for this session, so this is safe.
	if prev, ok := cur.(*control.Controller); ok {
		newCtrl.InheritLifecycleFrom(prev)
	}

	sess.mu.Lock()
	if sess.deleted {
		sess.mu.Unlock()
		newCtrl.Close()
		return &RPCError{Code: ErrInvalidRequest, Message: "session config: session is deleted"}
	}
	if sess.ctrl != cur {
		sess.mu.Unlock()
		newCtrl.Close()
		return sessionConfigActiveWorkError("session changed while switching config; retry")
	}
	sess.ctrl = newCtrl
	sess.model = cfgState.Model
	sess.effortOverride = cloneStringPtr(cfgState.EffortOverride)
	if sess.transcript != "" && sessionFileExists(sess.transcript) {
		_ = saveACPMeta(sess.transcript, sess.metaLocked())
	}
	sess.mu.Unlock()

	cur.ReleaseResources()
	s.sendAvailableCommands(sess)
	sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
	return nil
}

func (s *service) applyPendingSessionConfig(ctx context.Context, sess *acpSession) error {
	if s.session(sess.id) != sess {
		return nil
	}
	sess.mu.Lock()
	if sess.deleted || sess.pendingConfig == nil {
		sess.mu.Unlock()
		return nil
	}
	cfgState := cloneSessionConfigState(*sess.pendingConfig)
	// Keep pendingConfig set while rebuilding: begin refuses new turns until
	// rebuildSession claims it together with raising maintenanceDone, so no
	// promptable instant is visible in between.
	sess.mu.Unlock()

	if err := s.rebuildSession(ctx, sess, cfgState); err != nil {
		// Once this attempt failed nothing in flight is left to retry a parked
		// config, and begin refuses new turns while one is queued — drop it so
		// the session stays promptable (the caller reports the failure).
		sess.mu.Lock()
		if !sess.deleted && !sess.running && sess.maintenanceDone == nil {
			sess.pendingConfig = nil
		}
		sess.mu.Unlock()
		return err
	}
	return nil
}

func (s *service) reportPendingSessionConfigError(ctx context.Context, sess *acpSession, err error, when string) {
	if err == nil || sess == nil || sess.sink == nil {
		return
	}
	sess.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "session config switch failed " + when + ": " + err.Error()})
	if isSessionConfigActiveWorkError(err) {
		if current, stateErr := s.configStateForSession(ctx, sess); stateErr == nil {
			sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: current.ConfigOptions})
		}
	}
}

type activeSessionConfigWorkError struct {
	*RPCError
}

func (e *activeSessionConfigWorkError) Unwrap() error {
	return e.RPCError
}

func sessionConfigActiveWorkError(message string) error {
	return &activeSessionConfigWorkError{
		RPCError: &RPCError{Code: ErrInvalidRequest, Message: "session config: " + message},
	}
}

func isSessionConfigActiveWorkError(err error) bool {
	var activeErr *activeSessionConfigWorkError
	return errors.As(err, &activeErr)
}

// sessionClose releases an active session. Unknown sessions are accepted as a
// no-op because closing is an idempotent resource cleanup request.
func (s *service) sessionClose(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionCloseParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/close: " + err.Error()}
	}
	if err := validateSessionID("session/close", p.SessionID); err != nil {
		return nil, err
	}
	if sess := s.takeSession(p.SessionID); sess != nil {
		sess.abortAndWait()
		sess.ctrl.Close()
		sess.releaseSessionLease()
	}
	return SessionCloseResult{}, nil
}

// sessionList returns ACP sessions known to this process or persisted as ACP
// sidecars. It deliberately ignores ordinary CLI timestamp sessions.
func (s *service) sessionList(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionListParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, &RPCError{Code: ErrInvalidParams, Message: "session/list: " + err.Error()}
		}
	}
	filterCwd := strings.TrimSpace(p.Cwd)
	if filterCwd != "" && !filepath.IsAbs(filterCwd) {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/list: cwd must be an absolute path"}
	}
	if strings.TrimSpace(p.Cursor) != "" {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/list: unsupported cursor"}
	}

	byID := map[string]SessionInfo{}
	if dir := s.sessionDir(); dir != "" {
		metas, err := listACPMetas(dir)
		if err != nil {
			return nil, &RPCError{Code: ErrInternal, Message: "session/list: " + err.Error()}
		}
		// A recovered session has two sidecars claiming the same id: the
		// active recovery transcript's own meta and the id-keyed redirect.
		// Reduce to one representative per id before filtering, so the entry
		// shown never carries the stale pre-recovery title/timestamps.
		best := map[string]acpSessionMeta{}
		for _, meta := range metas {
			cur, ok := best[meta.SessionID]
			if !ok || listMetaBeats(meta, cur) {
				best[meta.SessionID] = meta
			}
		}
		for _, meta := range best {
			info := meta.info(nil)
			if sessionInfoMatchesCwd(info, filterCwd) {
				byID[info.SessionID] = info
			}
		}
	}
	for _, sess := range s.liveSessions() {
		info := sess.info()
		if sessionInfoMatchesCwd(info, filterCwd) {
			byID[info.SessionID] = info
		}
	}

	sessions := make([]SessionInfo, 0, len(byID))
	for _, info := range byID {
		sessions = append(sessions, info)
	}
	sort.Slice(sessions, func(i, j int) bool {
		ti := parseSessionUpdatedAt(sessions[i].UpdatedAt)
		tj := parseSessionUpdatedAt(sessions[j].UpdatedAt)
		if ti.Equal(tj) {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return ti.After(tj)
	})
	return SessionListResult{Sessions: sessions}, nil
}

// sessionDelete removes a session from future list results. Deleting a missing
// session succeeds silently, matching ACP's idempotent delete guidance.
func (s *service) sessionDelete(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionDeleteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/delete: " + err.Error()}
	}
	if err := validateSessionID("session/delete", p.SessionID); err != nil {
		return nil, err
	}

	path := ""
	var destroy control.SessionDestroyHandle
	var delayed bool
	if sess := s.takeSession(p.SessionID); sess != nil {
		sess.deleteAndWait()
		// The session is going away; drop its lease before removing files so
		// the lease sidecars retire with the release (they are not in
		// SessionSidecarFiles and would otherwise linger).
		sess.releaseSessionLease()
		path = sess.transcript
		destroy = sess.ctrl.BeginDestroySession(path)
		if result := destroy.Wait(); result.HasTimedOut() {
			if err := agent.MarkCleanupPending(path, "delete"); err != nil {
				go delayedDeleteSessionFiles(path, destroy)
				sess.ctrl.CloseAfterDestroy()
				return nil, &RPCError{Code: ErrInternal, Message: "session/delete: " + err.Error()}
			}
			go delayedDeleteSessionFiles(path, destroy)
			delayed = true
		}
		sess.ctrl.CloseAfterDestroy()
	}
	if path == "" {
		if dir := s.sessionDir(); dir != "" {
			path = resolveTranscriptPath(dir, p.SessionID)
		}
	}
	if path != "" && !delayed {
		if err := deleteSessionFiles(path); err != nil {
			return nil, &RPCError{Code: ErrInternal, Message: "session/delete: " + err.Error()}
		}
		if destroy.Finish != nil {
			destroy.Finish()
		}
	}
	// A recovered session lives in two files: the recovery transcript (deleted
	// above) and the id-keyed original holding the redirect. Remove the twin
	// too, or it resurfaces in session/list as a ghost that delete-by-id can
	// never reach again.
	if dir := s.sessionDir(); dir != "" {
		if idPath := transcriptPath(dir, p.SessionID); idPath != path {
			if err := deleteSessionFiles(idPath); err != nil {
				return nil, &RPCError{Code: ErrInternal, Message: "session/delete: " + err.Error()}
			}
		}
	}
	return SessionDeleteResult{}, nil
}

// sessionCancel aborts a session's in-flight turn, if any. It is a notification:
// no reply, and an unknown session is silently ignored.
func (s *service) sessionCancel(_ context.Context, raw json.RawMessage) {
	var p SessionCancelParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}
	if sess := s.session(p.SessionID); sess != nil {
		sess.abort()
	}
}

func (s *service) session(id string) *acpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
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

func (s *service) sessionDir() string {
	if p, ok := s.factory.(SessionDirProvider); ok {
		if dir := strings.TrimSpace(p.SessionDir()); dir != "" {
			return dir
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if dir := sess.currentCtrl().SessionDir(); dir != "" {
			return dir
		}
	}
	return ""
}

func (s *service) sessionConfigState(ctx context.Context, p SessionConfigStateParams) (SessionConfigState, error) {
	if provider, ok := s.factory.(SessionConfigStateProvider); ok {
		return provider.SessionConfigState(ctx, p)
	}
	return SessionConfigState{}, nil
}

func (s *service) configStateForSession(ctx context.Context, sess *acpSession) (SessionConfigState, error) {
	return s.sessionConfigState(ctx, sess.configStateParams())
}

func (s *acpSession) configStateParams() SessionConfigStateParams {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionConfigStateParams{
		Cwd:            s.cwd,
		Model:          s.model,
		EffortOverride: cloneStringPtr(s.effortOverride),
	}
}

func findConfigOption(options []SessionConfigOption, id string) (SessionConfigOption, bool) {
	id = normalizeConfigID(id)
	for _, opt := range options {
		if normalizeConfigID(opt.ID) == id {
			return opt, true
		}
	}
	return SessionConfigOption{}, false
}

func normalizeConfigID(id string) string {
	switch strings.TrimSpace(id) {
	case "models":
		return "model"
	case "reasoning_effort", "thought_level":
		return "effort"
	default:
		return strings.TrimSpace(id)
	}
}

func configOptionHasValue(option SessionConfigOption, value string) bool {
	for _, opt := range option.Options {
		if opt.Value == value {
			return true
		}
	}
	return false
}

func configOptionCategory(option SessionConfigOption) string {
	if option.Category != "" {
		return option.Category
	}
	switch normalizeConfigID(option.ID) {
	case "model":
		return "model"
	case "effort":
		return "thought_level"
	default:
		return ""
	}
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func cloneSessionConfigState(in SessionConfigState) SessionConfigState {
	out := in
	out.EffortOverride = cloneStringPtr(in.EffortOverride)
	if in.Models != nil {
		models := *in.Models
		models.AvailableModels = append([]ModelInfo(nil), in.Models.AvailableModels...)
		out.Models = &models
	}
	out.ConfigOptions = append([]SessionConfigOption(nil), in.ConfigOptions...)
	for i := range out.ConfigOptions {
		out.ConfigOptions[i].Options = append([]SessionConfigSelectOption(nil), out.ConfigOptions[i].Options...)
	}
	return out
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

func (s *service) loadMeta(id string) (acpSessionMeta, bool) {
	dir := s.sessionDir()
	if dir == "" {
		return acpSessionMeta{}, false
	}
	meta, ok, err := loadACPMeta(resolveTranscriptPath(dir, id))
	if err != nil {
		return acpSessionMeta{}, false
	}
	return meta, ok
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

	_ = ctrl.Snapshot()

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

func (s *acpSession) meta() acpSessionMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metaLocked()
}

func (s *acpSession) metaLocked() acpSessionMeta {
	return acpSessionMeta{
		SessionID:      s.id,
		Cwd:            s.cwd,
		Model:          s.model,
		EffortOverride: cloneStringPtr(s.effortOverride),
		Title:          s.title,
		CreatedAt:      s.createdAt,
		UpdatedAt:      s.updatedAt,
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
	for _, sk := range ctrl.Skills() {
		name := strings.TrimSpace(sk.Name)
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
	return text
}

type acpSessionMeta struct {
	SessionID      string    `json:"sessionId"`
	Cwd            string    `json:"cwd"`
	Model          string    `json:"model,omitempty"`
	EffortOverride *string   `json:"effortOverride,omitempty"`
	Title          string    `json:"title,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
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

func metadataForLoadedSession(path, id, cwd string, history []provider.Message) acpSessionMeta {
	now := time.Now().UTC()
	meta, ok, err := loadACPMeta(path)
	if err != nil || !ok {
		meta = acpSessionMeta{
			SessionID: id,
			Cwd:       cwd,
			Title:     titleFromHistory(history),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if info, statErr := os.Stat(path); statErr == nil {
			meta.CreatedAt = info.ModTime().UTC()
			meta.UpdatedAt = info.ModTime().UTC()
		}
	}
	if meta.SessionID == "" {
		meta.SessionID = id
	}
	if cwd != "" {
		meta.Cwd = cwd
	}
	if meta.Title == "" {
		meta.Title = titleFromHistory(history)
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}
	return meta
}

func loadACPMeta(sessionPath string) (acpSessionMeta, bool, error) {
	path := acpMetaPath(sessionPath)
	if path == "" {
		return acpSessionMeta{}, false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return acpSessionMeta{}, false, nil
		}
		return acpSessionMeta{}, false, err
	}
	var meta acpSessionMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return acpSessionMeta{}, false, fmt.Errorf("decode ACP session metadata %s: %w", path, err)
	}
	return meta, true, nil
}

func saveACPMeta(sessionPath string, meta acpSessionMeta) error {
	path := acpMetaPath(sessionPath)
	if path == "" {
		return nil
	}
	now := time.Now().UTC()
	if meta.SessionID == "" {
		meta.SessionID = sessionIDFromTranscript(sessionPath)
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".acp-session.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

func listACPMetas(dir string) ([]acpSessionMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []acpSessionMeta{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".acp.json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".acp.json")
		sessionPath := transcriptPath(dir, id)
		if agent.IsCleanupPending(sessionPath) {
			continue
		}
		if !sessionFileExists(sessionPath) {
			continue
		}
		meta, ok, err := loadACPMeta(sessionPath)
		if err != nil || !ok {
			continue
		}
		if meta.SessionID == "" {
			meta.SessionID = id
		}
		if meta.Cwd == "" {
			continue
		}
		out = append(out, meta)
	}
	return out, nil
}

func sessionFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func acpMetaPath(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return strings.TrimSuffix(sessionPath, filepath.Ext(sessionPath)) + ".acp.json"
}

func sessionIDFromTranscript(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

// listMetaBeats reports whether a should represent its session id in
// session/list over b. A meta without an ActiveTranscript redirect is the
// session's live transcript and always beats a redirect sidecar; between two
// of the same kind the later UpdatedAt wins.
func listMetaBeats(a, b acpSessionMeta) bool {
	aRedirect := strings.TrimSpace(a.ActiveTranscript) != ""
	bRedirect := strings.TrimSpace(b.ActiveTranscript) != ""
	if aRedirect != bRedirect {
		return !aRedirect
	}
	return a.UpdatedAt.After(b.UpdatedAt)
}

func sessionInfoMatchesCwd(info SessionInfo, filter string) bool {
	if filter == "" {
		return true
	}
	return filepath.Clean(info.Cwd) == filepath.Clean(filter)
}

func titleFromHistory(history []provider.Message) string {
	for _, m := range history {
		if m.Role == provider.RoleUser {
			if title := previewTitle(m.Content); title != "" {
				return title
			}
		}
	}
	return ""
}

func previewTitle(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) <= 80 {
		return text
	}
	runes := []rune(text)
	return string(runes[:77]) + "..."
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

func parseSessionUpdatedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func deleteSessionFiles(sessionPath string) error {
	paths := []string{
		sessionPath,
		acpMetaPath(sessionPath),
	}
	paths = append(paths, store.SessionSidecarFiles(sessionPath)...)
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if dir := checkpointPath(sessionPath); dir != "" {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := agent.DeleteSubagentsByParent(filepath.Dir(sessionPath), agent.BranchID(sessionPath)); err != nil {
		return err
	}
	if err := jobs.RemoveArtifacts(sessionPath); err != nil {
		return err
	}
	return agent.ClearCleanupPending(sessionPath)
}

// ReconcileCleanupPending retries delayed ACP session cleanup left by a previous
// process, including ACP's own metadata sidecar.
func ReconcileCleanupPending(dir string) error {
	return agent.ReconcileCleanupPending(dir, func(item agent.CleanupPendingInfo) error {
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
		case "http", "streamable-http", "streamable_http":
			if strings.TrimSpace(m.URL) == "" {
				return nil, fmt.Errorf("MCP server %q url is required", m.Name)
			}
			typ = "http"
		default:
			return nil, fmt.Errorf("MCP server %q uses unsupported transport %q", m.Name, m.Type)
		}
		out = append(out, plugin.Spec{
			Name:    strings.TrimSpace(m.Name),
			Type:    typ,
			Command: strings.TrimSpace(m.Command),
			Args:    append([]string(nil), m.Args...),
			Env:     mapString(m.Env),
			URL:     strings.TrimSpace(m.URL),
			Headers: mapString(m.Headers),
			Dir:     cwd,
		})
	}
	return out, nil
}

func mapString(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
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
