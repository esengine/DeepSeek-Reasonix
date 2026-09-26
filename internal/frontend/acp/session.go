package acp

// The session half of the ACP surface: new, load, resume, prompt, cancel and
// close, with the lookups they share. The service's own wiring stays in
// service.go.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"sort"
	"strings"
	"time"

	"reasonix/internal/session/control"
	"reasonix/internal/state/sessioninbox"
)

// sessionLeaseBindError maps a lease-acquisition failure to the protocol
// error the client sees: a held session names its holder with the shared CLI
// wording; anything else is an internal error.
func sessionLeaseBindError(method string, err error) *RPCError {
	if errors.Is(err, sessionstore.ErrSessionLeaseHeld) {
		return &RPCError{
			Code:    ErrInvalidRequest,
			Message: method + ": " + control.SessionInUseMessage(err) + "; " + control.SessionLeaseCloseHint,
		}
	}
	return &RPCError{Code: ErrInternal, Message: method + ": session lease: " + err.Error()}
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
	cfgState = withToolApprovalConfig(cfgState, control.ToolApprovalAsk)
	runtimeState, err := s.sessionRuntimeState(ctx, SessionRuntimeStateParams{
		Cwd: cwd, Model: cfgState.Model, RuntimeProfile: cfgState.RuntimeProfile,
	})
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
	}

	id, err := newSessionID()
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
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
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
	}
	ctrl.EnableInteractiveApproval()
	sink.bindApprove(ctrl.Approve)
	sink.bindAnswer(ctrl.AnswerQuestion)

	now := time.Now().UTC()
	sess := &acpSession{
		id:               id,
		ctrl:             ctrl,
		sink:             sink,
		cwd:              cwd,
		mcpServers:       clonePluginSpecs(mcpServers),
		model:            cfgState.Model,
		effortOverride:   cloneStringPtr(cfgState.EffortOverride),
		runtimeProfile:   cfgState.RuntimeProfile,
		toolApprovalMode: control.ToolApprovalAsk,
		runtimeState:     runtimeState,
		status:           newStatusTelemetry(),
		modeID:           sessionModeNormal,
		createdAt:        now,
		updatedAt:        now,
	}
	s.bindStatusEvents(sess)
	// Pin a transcript file keyed by session id when the controller has a session
	// dir, so every turn auto-saves there, session/prompt can hand the path back,
	// and session/load can find it again by id across process restarts. The
	// session lease is taken with it (defensive: the id-keyed path is brand new)
	// so no other runtime can bind the transcript while this session lives.
	if dir := ctrl.SessionDir(); dir != "" {
		sess.transcript = transcriptPath(dir, id)
		lease, err := sessionstore.TryAcquireSessionLease(sess.transcript)
		if err != nil {
			ctrl.Close()
			return nil, sessionLeaseBindError("session/new", err)
		}
		sess.lease = lease
		ctrl.SetFreshSessionPath(sess.transcript)
		if err := bindACPWriteAuthorityOrClose(ctrl, lease); err != nil {
			sess.lease = nil
			return nil, sessionLeaseBindError("session/new", err)
		}
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	// Fold in the live controller's extension catalog so plugin/... models
	// are discoverable from the very first session/new result.
	cfgState = enrichStateWithExtensionModels(cfgState, ctrl.ProviderCatalog())
	return afterResponse{
		result: SessionNewResult{
			SessionID:     id,
			Models:        cfgState.Models,
			Modes:         sessionModesState(sessionModeNormal),
			ConfigOptions: cfgState.ConfigOptions,
		},
		after: func() { s.sendAvailableCommands(sess) },
	}, nil
}

func sessionModesState(current string) *SessionModeState {
	return &SessionModeState{
		CurrentModeID: current,
		AvailableModes: []SessionMode{
			{ID: sessionModeNormal, Name: "Normal", Description: "Work directly and pause when user input is required"},
			{ID: sessionModePlan, Name: "Plan", Description: "Research and propose a plan before making changes"},
			{ID: sessionModeGoal, Name: "Goal", Description: "Keep advancing the next prompt as a goal until complete or blocked"},
		},
	}
}

// sessionSetMode switches the session's operating mode and confirms it with a
// current_mode_update, per the ACP session-mode contract.
func (s *service) sessionSetMode(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionSetModeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_mode: " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_mode: unknown session " + p.SessionID}
	}
	sess.stateChangeMu.Lock()
	defer sess.stateChangeMu.Unlock()
	ctrl := sess.currentCtrl()
	nextMode := p.ModeID
	legacyApproval := ""
	switch p.ModeID {
	case sessionModeNormal:
		ctrl.SetPlanMode(false)
		ctrl.ClearGoal()
	case sessionModePlan:
		ctrl.ClearGoal()
		ctrl.SetPlanMode(true)
	case sessionModeGoal:
		ctrl.SetPlanMode(false)
	case sessionModeLegacyDefault:
		nextMode = sessionModeNormal
		legacyApproval = control.ToolApprovalAsk
		ctrl.SetPlanMode(false)
		ctrl.ClearGoal()
	case sessionModeLegacyAuto:
		nextMode = sessionModeNormal
		legacyApproval = control.ToolApprovalYolo
		ctrl.SetPlanMode(false)
		ctrl.ClearGoal()
	default:
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/set_mode: unknown modeId " + p.ModeID}
	}
	sess.setGoalDraftMode(nextMode == sessionModeGoal && ctrl.GoalStatus() != control.GoalStatusRunning)
	if legacyApproval != "" {
		ctrl.SetToolApprovalMode(legacyApproval)
		sess.setToolApprovalMode(legacyApproval)
		if cfgState, err := s.configStateForSession(ctx, sess); err == nil {
			sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
		}
	}
	if sess.swapModeID(nextMode) != nextMode {
		sess.sink.send(currentModeUpdate{SessionUpdate: "current_mode_update", CurrentModeID: nextMode})
	}
	sess.saveMetaIfPresent()
	return SessionSetModeResult{}, nil
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
	return afterResponse{
		result: SessionLoadResult{Models: cfgState.Models, Modes: s.sessionModesFor(p.SessionID), ConfigOptions: cfgState.ConfigOptions},
		after:  func() { s.sendAvailableCommands(s.session(p.SessionID)) },
	}, nil
}

// sessionModesFor reports the modes state for a just-opened session. A live
// session keeps its current normal/plan/goal selection, so load/resume must not
// reset a reconnecting client's mode picker to normal.
func (s *service) sessionModesFor(id string) *SessionModeState {
	if sess := s.session(id); sess != nil {
		return sessionModesState(sess.currentModeID())
	}
	return sessionModesState(sessionModeNormal)
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
	return afterResponse{
		result: SessionResumeResult{Models: cfgState.Models, Modes: s.sessionModesFor(p.SessionID), ConfigOptions: cfgState.ConfigOptions},
		after:  func() { s.sendAvailableCommands(s.session(p.SessionID)) },
	}, nil
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
	text, err := promptText(sess.cwd, p.Prompt)
	if err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/prompt: " + err.Error()}
	}
	if text == "" {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/prompt: empty prompt"}
	}
	text = s.resolveSlashPrompt(ctx, sess, text)

	runCtx, cancel, ok := sess.begin(ctx)
	if !ok {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: "session/prompt: session already has an active prompt"}
	}
	if sess.status == nil {
		sess.status = newStatusTelemetry()
	}
	sess.status.beginTurn()
	s.publishStatus(sess, "phase")
	sess.sink.setTurnContext(runCtx)
	if sess.takeGoalDraftMode() {
		sess.currentCtrl().SetGoal(text)
		sess.saveMetaIfPresent()
	}
	defer func() {
		sess.sink.clearTurnContext()
		s.finishTurn(ctx, sess)
		cancel()
	}()
	runErr := drainACPInbox(runCtx, sess.ctrl, runPrompt(runCtx, sess.ctrl, text))

	statusEvent := sess.status.finishTurn(
		runErr,
		runCtx.Err() != nil,
		sess.currentCtrl().GoalStatus(),
		finalAssistantSummary(sess.currentCtrl()),
	)
	s.publishStatus(sess, statusEvent)
	// Persist after status finalization (best-effort) so reconnect recovers both
	// the transcript and the same sequence/usage/outcome snapshot.
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

// sessionSteer durably persists guidance then attempts mid-turn admission.
// Parameter/session errors remain RPC errors; busy rejection returns a
// disposition so clients can keep the durable follow-up.
func (s *service) sessionSteer(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionSteerParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionSteerMethod + ": " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionSteerMethod + ": unknown session " + p.SessionID}
	}
	text, err := promptText(sess.cwd, p.Prompt)
	if err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionSteerMethod + ": " + err.Error()}
	}
	if text == "" {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionSteerMethod + ": empty prompt"}
	}
	ctrl := sess.currentCtrl()
	if api, ok := ctrl.(control.EditorAPI); ok {
		if ensurer, ok := any(api).(interface{ EnsureSessionPath() }); ok {
			ensurer.EnsureSessionPath()
		}
		// Durable path when the session has a transcript path; ephemeral
		// test controllers without persistence fall back to TrySteer.
		if api.SessionPath() != "" {
			rec, err := api.TryEnqueueAndSteer(control.InboxRequest{
				Intent:  sessioninbox.IntentSteer,
				Display: text,
				Raw:     text,
				Submit:  text,
				Source:  "acp",
			})
			if err != nil {
				return nil, &RPCError{Code: ErrInvalidRequest, Message: sessionSteerMethod + ": " + err.Error()}
			}
			return SessionSteerResult{ItemID: rec.ItemID, Disposition: string(rec.Disposition)}, nil
		}
	}
	// Compatibility for older controller stubs / pathless sessions.
	if !ctrl.TrySteer(text) {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: sessionSteerMethod + ": session has no active prompt"}
	}
	return SessionSteerResult{Disposition: "steer_accepted"}, nil
}

// sessionReloadExtensions rebuilds a session's agent runtime in place —
// tools, skills, commands, hooks, MCP servers, and providers are re-discovered
// — while the session (transcript, approval grants, goal and recovery state)
// carries over via boot.Rebuild. It follows the same contract as a config
// switch: a turn or rebuild in flight coalesces exactly one queued reload,
// drained when the session goes idle; a failure keeps the old controller fully
// usable; the old controller's resources are released only after the swap.
func (s *service) sessionReloadExtensions(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionReloadExtensionsParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionReloadExtensionsMethod + ": " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionReloadExtensionsMethod + ": unknown session " + p.SessionID}
	}
	return s.reloadSessionExtensions(ctx, sess)
}

// sessionSetConfigOption applies ACP's generic session-level selectors for
// model, reasoning effort, work mode, and tool approval.
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
	case "work_mode", "agent_preset":
		next, err = s.switchSessionRuntimeProfile(ctx, sess, p.Value)
	case "tool_approval":
		next, err = s.switchSessionToolApproval(ctx, sess, p.Value)
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

func sessionConfigActiveWorkError(message string) error {
	return &activeSessionConfigWorkError{
		RPCError: &RPCError{Code: ErrInvalidRequest, Message: "session config: " + message},
	}
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
			if err := sessionstore.MarkCleanupPending(path, "delete"); err != nil {
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

func sessionFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
