package acp

// Switching a live session's configuration: which axes an ACP client may set,
// how a pending change merges and applies, and the mode/approval state each
// switch reports back.

import (
	"context"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

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
		return sessionModeNormal
	}
	return s.modeID
}

func (s *acpSession) setGoalDraftMode(on bool) {
	s.mu.Lock()
	s.goalDraftMode = on
	s.mu.Unlock()
}

func (s *acpSession) takeGoalDraftMode() bool {
	s.mu.Lock()
	on := s.goalDraftMode
	s.goalDraftMode = false
	s.mu.Unlock()
	return on
}

func (s *acpSession) isGoalDraftMode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.goalDraftMode
}

func (s *acpSession) setToolApprovalMode(mode string) {
	s.mu.Lock()
	s.toolApprovalMode = normalizeACPToolApprovalMode(mode)
	s.mu.Unlock()
}

func (s *acpSession) swapToolApprovalMode(mode string) (old string) {
	mode = normalizeACPToolApprovalMode(mode)
	s.mu.Lock()
	old = normalizeACPToolApprovalMode(s.toolApprovalMode)
	s.toolApprovalMode = mode
	s.mu.Unlock()
	return old
}

// mergePendingConfig queues delta with last-write-wins per axis: it replaces a
// queued entry for the same axis and appends otherwise, so a change queued for
// one axis can never drop a change queued for another. Callers hold sess.mu.
func mergePendingConfig(queue []sessionConfigDelta, delta sessionConfigDelta) []sessionConfigDelta {
	for i := range queue {
		if queue[i].axis == delta.axis {
			queue[i] = delta.clone()
			return queue
		}
	}
	return append(queue, delta.clone())
}

// removePendingAxes drops the queue entries whose axis a rebuild is applying,
// keeping entries other requests queued in the meantime so the post-maintenance
// drain still applies them. Callers hold sess.mu.
func removePendingAxes(queue, applied []sessionConfigDelta) []sessionConfigDelta {
	if len(queue) == 0 {
		return nil
	}
	kept := queue[:0]
	for _, q := range queue {
		drop := false
		for _, d := range applied {
			if q.axis == d.axis {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, q)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func clonePendingConfig(queue []sessionConfigDelta) []sessionConfigDelta {
	if len(queue) == 0 {
		return nil
	}
	out := make([]sessionConfigDelta, len(queue))
	for i := range queue {
		out[i] = queue[i].clone()
	}
	return out
}

func (d sessionConfigDelta) applyTo(p *SessionConfigStateParams) {
	switch d.axis {
	case "model":
		p.Model = d.model
	case "thought_level":
		p.EffortOverride = cloneStringPtr(d.effortOverride)
	case "work_mode", "agent_preset":
		p.RuntimeProfile = d.runtimeProfile
	}
}

// resolveSessionConfigDeltas resolves deltas against the session's current
// config baseline. Calling this fresh at every apply — instead of reusing a
// snapshot taken when a change was first requested — is what keeps a queued
// delta for one axis from clobbering another axis that rebuilt in between.
func (s *service) resolveSessionConfigDeltas(ctx context.Context, sess *acpSession, deltas []sessionConfigDelta) (SessionConfigState, error) {
	params := sess.configStateParams()
	for _, delta := range deltas {
		delta.applyTo(&params)
	}
	cfgState, err := s.sessionConfigState(ctx, params)
	if err != nil {
		return SessionConfigState{}, err
	}
	return withToolApprovalConfig(cfgState, sess.currentToolApprovalMode()), nil
}

// sessionConfigMatchesRuntime reports whether cfgState names the running
// controller generation, then drops older queued deltas for the same axes:
// a later request that restores the running value is their last write.
func sessionConfigMatchesRuntime(sess *acpSession, cfgState SessionConfigState, deltas []sessionConfigDelta) bool {
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	if sess.deleted || sess.maintenanceDone != nil {
		sess.mu.Unlock()
		return false
	}
	ctrl := sess.ctrl
	sess.mu.Unlock()
	matcher, ok := ctrl.(control.RuntimeSelectionMatcher)
	if !ok {
		return false
	}
	if !matcher.MatchesRuntimeSelection(control.RuntimeSelection{
		ModelRef:            cfgState.Model,
		Effort:              cfgState.ResolvedEffort,
		ProviderFingerprint: cfgState.ProviderFingerprint,
	}) {
		return false
	}
	sess.mu.Lock()
	if sess.deleted || sess.maintenanceDone != nil {
		sess.mu.Unlock()
		return false
	}
	sess.pendingConfig = removePendingAxes(sess.pendingConfig, deltas)
	sess.mu.Unlock()
	return true
}

func (s *service) switchSessionModel(ctx context.Context, sess *acpSession, modelID string) (SessionConfigState, error) {
	deltas := []sessionConfigDelta{{axis: "model", model: modelID}}
	return s.switchSessionConfig(ctx, sess, deltas)
}

func (s *service) switchSessionEffort(ctx context.Context, sess *acpSession, effort string) (SessionConfigState, error) {
	level := strings.TrimSpace(effort)
	if level == "auto" {
		level = ""
	}
	deltas := []sessionConfigDelta{{axis: "thought_level", effortOverride: &level}}
	return s.switchSessionConfig(ctx, sess, deltas)
}

func (s *service) switchSessionRuntimeProfile(ctx context.Context, sess *acpSession, profile string) (SessionConfigState, error) {
	// Role settings switch in place without rebuilding the controller when
	// the session is idle. Busy sessions return an explicit error (no silent
	// queue). TryLock so a concurrent model/effort rebuild cannot deadlock us.
	if !sess.stateChangeMu.TryLock() {
		return SessionConfigState{}, sessionConfigActiveWorkError("session is busy; retry when idle")
	}
	defer sess.stateChangeMu.Unlock()
	sess.mu.Lock()
	if sess.deleted {
		sess.mu.Unlock()
		return SessionConfigState{}, &RPCError{Code: ErrInvalidRequest, Message: "session/set_config_option: session is deleted"}
	}
	status := sess.ctrl.RuntimeStatus()
	if status.PendingPrompt {
		sess.mu.Unlock()
		return SessionConfigState{}, sessionConfigActiveWorkError("answer pending prompts before switching execution setting")
	}
	if sess.running || status.Running {
		sess.mu.Unlock()
		return SessionConfigState{}, sessionConfigActiveWorkError("finish or cancel the active turn before switching execution setting")
	}
	if status.BackgroundJobs > 0 {
		sess.mu.Unlock()
		return SessionConfigState{}, sessionConfigActiveWorkError("stop background jobs before switching execution setting")
	}
	if sess.maintenanceDone != nil {
		sess.mu.Unlock()
		return SessionConfigState{}, sessionConfigActiveWorkError("session is busy; retry when idle")
	}
	ctrl := sess.ctrl
	sess.mu.Unlock()
	if ctrl != nil {
		ctrl.SetAgentPreset(profile)
	}
	// Dual-write session runtime profile label for config option responses.
	var normalized string
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "light", "economy", "eco", "lite":
		normalized = "economy"
	case "delivery", "deliver", "quality":
		normalized = "delivery"
	default:
		normalized = "balanced"
	}
	sess.mu.Lock()
	sess.runtimeProfile = normalized
	// Keep status planner mode aligned without a controller rebuild.
	if isLightRuntimeProfile(normalized) {
		sess.runtimeState.PlannerMode = "off"
	} else {
		sess.runtimeState.PlannerMode = "on"
	}
	sess.mu.Unlock()
	sess.saveMetaIfPresent()
	cfgState, err := s.configStateForSession(ctx, sess)
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: "session/set_config_option: " + err.Error()}
	}
	sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
	return cfgState, nil
}

// switchSessionConfig resolves and applies one explicit config request without
// letting its full config snapshot roll back another axis. Resolution must be
// repeated after stateChangeMu is acquired: a different-axis rebuild may finish
// while this request is resolving or waiting for the lock, making the earlier
// baseline stale even though this request's own delta is still current.
func (s *service) switchSessionConfig(ctx context.Context, sess *acpSession, deltas []sessionConfigDelta) (SessionConfigState, error) {
	resolve := func() (SessionConfigState, error) {
		cfgState, err := s.resolveSessionConfigDeltas(ctx, sess, deltas)
		if err != nil {
			method := "session/set_config_option"
			if len(deltas) == 1 && deltas[0].axis == "model" {
				method = "session/set_model"
			}
			return SessionConfigState{}, &RPCError{Code: ErrInvalidParams, Message: method + ": " + err.Error()}
		}
		if len(deltas) == 1 && deltas[0].axis == "model" && cfgState.Model == "" {
			return SessionConfigState{}, &RPCError{Code: ErrInvalidRequest, Message: "session/set_model: model switching is unavailable in this session"}
		}
		return cfgState, nil
	}

	if !sess.stateChangeMu.TryLock() {
		// Preserve the non-blocking queue contract while a rebuild is already in
		// maintenance. Resolve once for validation and the immediate client update;
		// the drain resolves the queued deltas again against live state.
		cfgState, err := resolve()
		if err != nil {
			return SessionConfigState{}, err
		}
		sess.mu.Lock()
		if sess.maintenanceDone != nil && !sess.deleted {
			for _, delta := range deltas {
				sess.pendingConfig = mergePendingConfig(sess.pendingConfig, delta)
			}
			sess.mu.Unlock()
			sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
			return cfgState, nil
		}
		sess.mu.Unlock()
		sess.stateChangeMu.Lock()
	}

	// Always resolve inside the serialization domain. Even a successful TryLock
	// can follow a concurrent rebuild that completed after this request began.
	cfgState, err := resolve()
	if err != nil {
		sess.stateChangeMu.Unlock()
		return SessionConfigState{}, err
	}
	if sessionConfigMatchesRuntime(sess, cfgState, deltas) {
		sess.storeConfigState(cfgState)
		sess.stateChangeMu.Unlock()
		return cfgState, nil
	}
	didMaintenance := false
	err = s.rebuildSessionLocked(ctx, sess, cfgState, deltas, &didMaintenance)
	sess.stateChangeMu.Unlock()
	if didMaintenance {
		pendingErr := s.applyPendingSessionConfig(ctx, sess)
		s.reportPendingSessionConfigError(ctx, sess, pendingErr, "after maintenance")
		// A reloadExtensions request queued behind this maintenance runs next.
		s.drainPendingReload(ctx, sess)
		// The pending drain completes before this request returns. Refresh the RPC
		// result so an older response cannot overwrite the newer config_option_update
		// with the pre-drain full snapshot on the client.
		if current, stateErr := s.configStateForSession(ctx, sess); stateErr == nil {
			cfgState = current
		}
	}
	if err != nil {
		return SessionConfigState{}, err
	}
	return cfgState, nil
}

func (s *service) switchSessionToolApproval(ctx context.Context, sess *acpSession, mode string) (SessionConfigState, error) {
	sess.stateChangeMu.Lock()
	defer sess.stateChangeMu.Unlock()
	mode = normalizeACPToolApprovalMode(mode)
	ctrl := sess.currentCtrl()
	ctrl.SetToolApprovalMode(mode)
	sess.setToolApprovalMode(mode)
	sess.saveMetaIfPresent()
	cfgState, err := s.configStateForSession(ctx, sess)
	if err != nil {
		return SessionConfigState{}, &RPCError{Code: ErrInternal, Message: "session/set_config_option: " + err.Error()}
	}
	sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfgState.ConfigOptions})
	return cfgState, nil
}

func (s *service) applyPendingSessionConfig(ctx context.Context, sess *acpSession) error {
	var firstErr error
	for {
		if s.session(sess.id) != sess {
			return firstErr
		}
		// Claim the queue in the same serialization domain as explicit config
		// switches. Without this lock, a newer same-axis request can rebuild after
		// the clone below but before this apply starts, then the stale cloned delta
		// queues behind it and wins last instead of preserving request order.
		sess.stateChangeMu.Lock()
		didMaintenance := false
		sess.mu.Lock()
		if sess.deleted || len(sess.pendingConfig) == 0 {
			sess.mu.Unlock()
			sess.stateChangeMu.Unlock()
			return firstErr
		}
		deltas := clonePendingConfig(sess.pendingConfig)
		// Keep pendingConfig set while rebuilding: begin refuses new turns until
		// rebuildSession claims it together with raising maintenanceDone, so no
		// promptable instant is visible in between.
		sess.mu.Unlock()

		// Re-resolve against the session's current state rather than reusing
		// whatever baseline existed when each delta queued: another axis may have
		// finished rebuilding in the meantime, and replaying its old value here
		// would silently roll it back. All queued axes resolve into one state so a
		// single rebuild applies them together.
		cfgState, err := s.resolveSessionConfigDeltas(ctx, sess, deltas)
		if err != nil {
			sess.mu.Lock()
			if !sess.deleted && !sess.running && sess.maintenanceDone == nil {
				sess.pendingConfig = removePendingAxes(sess.pendingConfig, deltas)
			}
			sess.mu.Unlock()
			sess.stateChangeMu.Unlock()
			if firstErr != nil {
				s.reportPendingSessionConfigError(ctx, sess, err, "after failed maintenance")
				return firstErr
			}
			return err
		}

		err = s.rebuildSessionLocked(ctx, sess, cfgState, deltas, &didMaintenance)
		if err != nil && !didMaintenance {
			// Once this attempt failed nothing in flight is left to retry the
			// claimed axes, and begin refuses new turns while any are queued — drop
			// them so the session stays promptable. Once maintenance started, those
			// axes were already removed; anything queued now is a newer request and
			// must survive this failure.
			sess.mu.Lock()
			if !sess.deleted && !sess.running && sess.maintenanceDone == nil {
				sess.pendingConfig = removePendingAxes(sess.pendingConfig, deltas)
			}
			sess.mu.Unlock()
		}
		sess.stateChangeMu.Unlock()

		if err != nil {
			if firstErr == nil {
				firstErr = err
			} else {
				s.reportPendingSessionConfigError(ctx, sess, err, "after failed maintenance")
			}
			if !didMaintenance {
				return firstErr
			}
		}
		if !didMaintenance {
			return firstErr
		}
		// Requests can queue while NewSession/Snapshot runs. Iterate even when this
		// rebuild failed so their already-successful RPCs cannot leave the session
		// blocked. A loop keeps sustained config traffic from growing the call stack.
	}
}

func (s *service) reportPendingSessionConfigError(ctx context.Context, sess *acpSession, err error, when string) {
	if err == nil || sess == nil || sess.sink == nil {
		return
	}
	sess.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "session config switch failed " + when + ": " + err.Error()})
	// A queued request already announced its desired config to the client. Every
	// apply failure leaves the outgoing controller/config active, so always send
	// the live state back; otherwise snapshot/build/resolve failures leave the
	// picker claiming a switch that never happened.
	if current, stateErr := s.configStateForSession(ctx, sess); stateErr == nil {
		sess.sink.send(configOptionUpdate{SessionUpdate: "config_option_update", ConfigOptions: current.ConfigOptions})
	}
}

func (s *service) configStateForSession(ctx context.Context, sess *acpSession) (SessionConfigState, error) {
	state, err := s.sessionConfigState(ctx, sess.configStateParams())
	if err != nil {
		return SessionConfigState{}, err
	}
	// Fold in the live controller's extension catalog so plugin/... models
	// are discoverable on every config-state read, not only when current.
	state = enrichStateWithExtensionModels(state, sess.currentCtrl().ProviderCatalog())
	return withToolApprovalConfig(state, sess.currentToolApprovalMode()), nil
}

func (s *acpSession) configStateParams() SessionConfigStateParams {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionConfigStateParams{
		Cwd:            s.cwd,
		Model:          s.model,
		EffortOverride: cloneStringPtr(s.effortOverride),
		RuntimeProfile: s.runtimeProfile,
	}
}

func (s *acpSession) storeConfigState(cfgState SessionConfigState) {
	s.mu.Lock()
	s.model = cfgState.Model
	s.effortOverride = cloneStringPtr(cfgState.EffortOverride)
	s.runtimeProfile = cfgState.RuntimeProfile
	s.mu.Unlock()
	s.saveMetaIfPresent()
}

func (s *acpSession) currentToolApprovalMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return normalizeACPToolApprovalMode(s.toolApprovalMode)
}

func normalizeACPToolApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case control.ToolApprovalAuto:
		return control.ToolApprovalAuto
	case control.ToolApprovalYolo:
		return control.ToolApprovalYolo
	default:
		return control.ToolApprovalAsk
	}
}

func normalizeACPCollaborationMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case sessionModePlan:
		return sessionModePlan
	case sessionModeGoal:
		return sessionModeGoal
	default:
		return sessionModeNormal
	}
}

func withToolApprovalConfig(state SessionConfigState, mode string) SessionConfigState {
	mode = normalizeACPToolApprovalMode(mode)
	option := SessionConfigOption{
		ID:           "tool_approval",
		Name:         "Tool Approval",
		Category:     "tool_approval",
		Type:         "select",
		CurrentValue: mode,
		Options: []SessionConfigSelectOption{
			{Value: control.ToolApprovalAsk, Name: "Ask", Description: "Ask before permission-gated tool calls"},
			{Value: control.ToolApprovalAuto, Name: "Auto", Description: "Follow configured permission rules without fallback prompts"},
			{Value: control.ToolApprovalYolo, Name: "Yolo", Description: "Approve tool calls except protected decisions"},
		},
	}
	for i := range state.ConfigOptions {
		if normalizeConfigID(state.ConfigOptions[i].ID) == option.ID {
			state.ConfigOptions[i] = option
			return state
		}
	}
	state.ConfigOptions = append(state.ConfigOptions, option)
	return state
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
	case "profile", "runtime_profile", "token_mode":
		return "work_mode"
	case "approval", "approval_mode", "tool_approval_mode":
		return "tool_approval"
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
	case "work_mode":
		return "work_mode"
	case "tool_approval":
		return "tool_approval"
	default:
		return ""
	}
}
