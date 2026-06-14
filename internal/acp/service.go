package acp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
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
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
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
// ACP config options. MCPServers are the stdio MCP servers the client asked the
// agent to connect for this session.
type SessionParams struct {
	Cwd            string
	MCPServers     []plugin.Spec
	Sink           event.Sink
	Model          string
	EffortOverride *string
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
	conn.Handle("session/new", svc.sessionNew)
	conn.Handle("session/load", svc.sessionLoad)
	conn.Handle("session/resume", svc.sessionResume)
	conn.Handle("session/prompt", svc.sessionPrompt)
	conn.Handle("session/set_config_option", svc.sessionSetConfigOption)
	conn.Handle("session/set_model", svc.sessionSetModel)
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
}

// acpSession is one open session: its controller, the on-disk transcript path
// (empty when persistence is off), and the cancel func of the in-flight turn
// (nil when idle) so session/cancel can abort it.
type acpSession struct {
	id         string
	ctrl       *control.Controller
	sink       *updateSink
	transcript string
	cwd        string
	mcpServers []plugin.Spec
	model      string
	// nil means use config; non-nil empty string means provider default.
	effortOverride *string
	pendingConfig  *SessionConfigState
	title          string
	createdAt      time.Time
	updatedAt      time.Time

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	running bool
	deleted bool
}

func (s *acpSession) begin(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.running || s.deleted {
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
	s.mu.Unlock()
	if c != nil {
		c()
	}
	if done != nil {
		<-done
	}
}

func (s *acpSession) deleteAndWait() {
	s.mu.Lock()
	s.deleted = true
	c := s.cancel
	done := s.done
	s.mu.Unlock()
	if c != nil {
		c()
	}
	if done != nil {
		<-done
	}
}

// initialize advertises the agent's capability set: persisted load plus ACP v1
// list/resume/close/delete lifecycle helpers, prompts carrying inline resource
// text (embeddedContext) but not image/audio, and stdio-only MCP (no http/sse).
func (s *service) initialize(_ context.Context, _ json.RawMessage) (any, error) {
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
			MCPCapabilities: MCPCapabilities{HTTP: false, SSE: false},
		},
		AgentInfo:   Implementation{Name: s.info.Name, Version: s.info.Version},
		AuthMethods: []any{},
	}, nil
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
	ctrl, err := s.factory.NewSession(ctx, SessionParams{
		Cwd:            cwd,
		MCPServers:     mcpServers,
		Sink:           sink,
		Model:          cfgState.Model,
		EffortOverride: cloneStringPtr(cfgState.EffortOverride),
	})
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
	}
	ctrl.EnableInteractiveApproval()
	sink.bindApprove(ctrl.Approve)

	now := time.Now().UTC()
	sess := &acpSession{
		id:             id,
		ctrl:           ctrl,
		sink:           sink,
		cwd:            cwd,
		mcpServers:     clonePluginSpecs(mcpServers),
		model:          cfgState.Model,
		effortOverride: cloneStringPtr(cfgState.EffortOverride),
		createdAt:      now,
		updatedAt:      now,
	}
	// Pin a transcript file keyed by session id when the controller has a session
	// dir, so every turn auto-saves there, session/prompt can hand the path back,
	// and session/load can find it again by id across process restarts.
	if dir := ctrl.SessionDir(); dir != "" {
		sess.transcript = transcriptPath(dir, id)
		ctrl.SetSessionPath(sess.transcript)
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	return SessionNewResult{
		SessionID:     id,
		Models:        cfgState.Models,
		ConfigOptions: cfgState.ConfigOptions,
	}, nil
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
	return SessionLoadResult{Models: cfgState.Models, ConfigOptions: cfgState.ConfigOptions}, nil
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
	return SessionResumeResult{Models: cfgState.Models, ConfigOptions: cfgState.ConfigOptions}, nil
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
		if replay {
			newUpdateSink(s.conn, id).replay(sess.ctrl.History())
		}
		cfgState, err := s.configStateForSession(ctx, sess)
		if err != nil {
			return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
		}
		return cfgState, nil
	}

	var saved acpSessionMeta
	if dir := s.sessionDir(); dir != "" {
		meta, _, metaErr := loadACPMeta(transcriptPath(dir, id))
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
	ctrl, err := s.factory.NewSession(ctx, SessionParams{
		Cwd:            cwd,
		MCPServers:     mcpServers,
		Sink:           sink,
		Model:          cfgState.Model,
		EffortOverride: cloneStringPtr(cfgState.EffortOverride),
	})
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
	}
	ctrl.EnableInteractiveApproval()
	sink.bindApprove(ctrl.Approve)

	dir := ctrl.SessionDir()
	if dir == "" {
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": persistence is disabled"}
	}
	path := transcriptPath(dir, id)
	loaded, err := agent.LoadSession(path)
	if err != nil {
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
		title:          meta.Title,
		createdAt:      meta.CreatedAt,
		updatedAt:      meta.UpdatedAt,
	}
	if err := saveACPMeta(path, sess.meta()); err != nil {
		ctrl.Close()
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: method + ": " + err.Error()}
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

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

	runCtx, cancel, ok := sess.begin(ctx)
	if !ok {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: "session/prompt: session already has an active prompt"}
	}
	sess.sink.setTurnContext(runCtx)
	defer func() {
		sess.sink.clearTurnContext()
		sess.finish()
		if err := s.applyPendingSessionConfig(ctx, sess); err != nil {
			sess.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "session config switch failed after turn: " + err.Error()})
		}
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

func (s *service) rebuildSession(ctx context.Context, sess *acpSession, cfgState SessionConfigState) error {
	sess.mu.Lock()
	if sess.deleted {
		sess.mu.Unlock()
		return &RPCError{Code: ErrInvalidRequest, Message: "session config: session is deleted"}
	}
	if sess.running || sess.ctrl.Running() {
		pending := cloneSessionConfigState(cfgState)
		sess.model = cfgState.Model
		sess.effortOverride = cloneStringPtr(cfgState.EffortOverride)
		sess.pendingConfig = &pending
		if sess.transcript != "" && sessionFileExists(sess.transcript) {
			_ = saveACPMeta(sess.transcript, sess.metaLocked())
		}
		sess.mu.Unlock()
		sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
		return nil
	}
	sess.pendingConfig = nil

	cur := sess.ctrl
	prevPath := cur.SessionPath()
	if err := cur.Snapshot(); err != nil {
		sess.mu.Unlock()
		return &RPCError{Code: ErrInternal, Message: "session config: snapshot before switch: " + err.Error()}
	}
	carried := cur.History()
	sink := sess.sink
	mcpServers := clonePluginSpecs(sess.mcpServers)
	cwd := sess.cwd

	newCtrl, err := s.factory.NewSession(ctx, SessionParams{
		Cwd:            cwd,
		MCPServers:     mcpServers,
		Sink:           sink,
		Model:          cfgState.Model,
		EffortOverride: cloneStringPtr(cfgState.EffortOverride),
	})
	if err != nil {
		sess.mu.Unlock()
		return &RPCError{Code: ErrInternal, Message: "session config: " + err.Error()}
	}
	newCtrl.EnableInteractiveApproval()
	sink.bindApprove(newCtrl.Approve)
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, prevPath)
	} else if prevPath != "" {
		newCtrl.SetSessionPath(prevPath)
	}
	newCtrl.InheritLifecycleFrom(cur)

	sess.ctrl = newCtrl
	sess.model = cfgState.Model
	sess.effortOverride = cloneStringPtr(cfgState.EffortOverride)
	if sess.transcript != "" && sessionFileExists(sess.transcript) {
		_ = saveACPMeta(sess.transcript, sess.metaLocked())
	}
	sess.mu.Unlock()

	cur.ReleaseResources()
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
	sess.pendingConfig = nil
	sess.mu.Unlock()

	if err := s.rebuildSession(ctx, sess, cfgState); err != nil {
		sess.mu.Lock()
		if !sess.deleted && sess.pendingConfig == nil {
			pending := cloneSessionConfigState(cfgState)
			sess.pendingConfig = &pending
		}
		sess.mu.Unlock()
		return err
	}
	return nil
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
		for _, meta := range metas {
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
	if sess := s.takeSession(p.SessionID); sess != nil {
		sess.deleteAndWait()
		sess.ctrl.Close()
		path = sess.transcript
	}
	if path == "" {
		if dir := s.sessionDir(); dir != "" {
			path = transcriptPath(dir, p.SessionID)
		}
	}
	if path != "" {
		if err := deleteSessionFiles(path); err != nil {
			return nil, &RPCError{Code: ErrInternal, Message: "session/delete: " + err.Error()}
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
		if dir := sess.ctrl.SessionDir(); dir != "" {
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
	meta, ok, err := loadACPMeta(transcriptPath(dir, id))
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
		sess.abort()
		sess.ctrl.Close()
	}
}

func (s *acpSession) persistAfterTurn(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted {
		return
	}
	_ = s.ctrl.Snapshot()
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
	extra := map[string]any{}
	if n := len(s.ctrl.History()); n > 0 {
		extra["messageCount"] = n
	}
	if len(extra) == 0 {
		extra = nil
	}
	return meta.info(extra)
}

type acpSessionMeta struct {
	SessionID      string    `json:"sessionId"`
	Cwd            string    `json:"cwd"`
	Model          string    `json:"model,omitempty"`
	EffortOverride *string   `json:"effortOverride,omitempty"`
	Title          string    `json:"title,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
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
		sessionPath + ".meta",
		acpMetaPath(sessionPath),
	}
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
	return nil
}

func checkpointPath(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return strings.TrimSuffix(sessionPath, ".jsonl") + ".ckpt"
}

// mcpSpecs converts ACP stdio MCP server declarations to plugin.Spec. ACP's
// session/new only carries stdio servers (the agent advertises http/sse off).
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
		if typ != "stdio" {
			return nil, fmt.Errorf("MCP server %q uses unsupported transport %q", m.Name, m.Type)
		}
		if strings.TrimSpace(m.Name) == "" {
			return nil, fmt.Errorf("MCP server name is required")
		}
		if strings.TrimSpace(m.Command) == "" {
			return nil, fmt.Errorf("MCP server %q command is required", m.Name)
		}
		out = append(out, plugin.Spec{
			Name:    m.Name,
			Type:    typ,
			Command: m.Command,
			Args:    m.Args,
			Env:     map[string]string(m.Env),
			Dir:     cwd,
		})
	}
	return out, nil
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
