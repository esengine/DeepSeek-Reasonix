package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"reasonix/internal/state/sessionstore"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func (a *contextWindow) contextMaintenanceInputHash(visible []provider.Message) string {
	if a == nil {
		return ""
	}
	seed := a.currentPromptCacheKey() + "\n" + sessionstore.ProviderVisibleFingerprint(provider.ModelMessages(visible))
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// transientSummaryFailure reports a failure of the summary request itself, which
// a later attempt can answer differently. Every other class is a property of
// the input or the install, so retrying the same input cannot change it.
func transientSummaryFailure(code string) bool {
	switch CompactionNoopReason(code) {
	case FailSummaryTruncated, FailSummaryTimeout, FailSummaryCeiling, FailSummaryFailed, FailSummaryNotDigest:
		return true
	}
	return false
}

// retriesOnGrowth reports a failure the input's growth releases: the summary
// request failed, or the checkpoint was refused for the protected content it
// would carry, which a larger input and a forced fold both answer differently.
// Anything else holds, because retrying the same input cannot change it.
func retriesOnGrowth(code string) bool {
	return transientSummaryFailure(code) || CompactionNoopReason(code) == NoopCandidateAboveCeiling
}

// retryGrowthStep is how much the input must grow past a transient failure
// before another summary request is worth its cost: an eighth of the trigger,
// never more than half of what the failure left below the hard ceiling, so the
// release point always sits short of it.
func (a *contextWindow) retryGrowthStep(failedAt int) int {
	return max(1, min(a.compactTrigger()/8, (a.hardInputCeiling()-failedAt)/2))
}

// blockedReceiptHolds reports whether a blocked or failed receipt still
// suppresses automatic maintenance at this input size. A failure that growth
// releases, recorded without a size (older sidecars), is released once.
func (a *contextWindow) blockedReceiptHolds(r *sessionstore.ContextMaintenanceReceipt, tokens int) bool {
	if r == nil || (r.Status != "blocked" && r.Status != "failed") {
		return false
	}
	switch {
	case transientSummaryFailure(r.Code):
		return r.InputTokens > 0 && tokens < r.InputTokens+a.retryGrowthStep(r.InputTokens)
	case retriesOnGrowth(r.Code):
		// Growth releases a protected-content refusal only below the physical
		// ceiling: past it the request goes out and the provider rules.
		return r.InputTokens == 0 || tokens < r.InputTokens+a.retryGrowthStep(r.InputTokens) || tokens >= a.hardInputCeiling()
	}
	return true
}

// contextMaintenanceBlocked reports whether automatic maintenance is
// suppressed. Overflow is a fresh provider refusal, so it passes a failure
// growth releases; one overflow recovery per request bounds the cost.
func (a *contextWindow) contextMaintenanceBlocked(inputHash string, tokens int, overflow bool) (bool, string) {
	if a == nil {
		return false, ""
	}
	a.sess.win.compactionMu.Lock()
	defer a.sess.win.compactionMu.Unlock()
	r := a.sess.win.compactionState.LastReceipt
	if r == nil {
		// Legacy sidecars may only have BlockedInputHash without a receipt.
		if a.sess.win.compactionState.BlockedInputHash != "" &&
			(inputHash == "" || a.sess.win.compactionState.BlockedInputHash == inputHash) {
			return true, a.sess.win.compactionState.BlockedReason
		}
		return false, ""
	}
	if overflow && (transientSummaryFailure(r.Code) || retriesOnGrowth(r.Code) && r.InputHash != inputHash) {
		return false, ""
	}
	if !a.blockedReceiptHolds(r, tokens) {
		return false, ""
	}
	return true, firstNonEmpty(a.sess.win.compactionState.BlockedReason, r.Reason)
}

func (a *contextWindow) emitContextMaintenance(r *sessionstore.ContextMaintenanceReceipt) {
	if a == nil || r == nil || a.svc.sink == nil {
		return
	}
	a.svc.sink.Emit(event.Event{Kind: event.ContextMaintenanceEvent, Maintenance: &event.ContextMaintenance{
		Status: r.Status, Action: r.Action, Trigger: r.Trigger, OperationID: r.OperationID,
		InputTokens: r.InputTokens, ResultTokens: r.ResultTokens, SavedTokens: r.SavedTokens,
		AffectedToolResults: r.AffectedToolResults, ProjectionVersion: r.ProjectionVersion,
		CacheBreak: r.CacheBreak, Reason: r.Reason,
		Code: r.Code, Boundary: r.Boundary, TriggerTokens: r.TriggerTokens,
	}})
}

// compactionFrame stamps the boundary that sent a fold onto one frame. Every
// emitter carries it, and four copies of the same two lookups is how one of
// them comes to report a threshold the session is not running under.
func (a *contextWindow) compactionFrame(c event.Compaction) event.Compaction {
	c.Boundary, c.TriggerTokens = a.compactBoundary(), a.compactTrigger()
	return c
}

// compactBoundary names the threshold that decides maintenance right now: the
// window share, or the absolute visible-input size that does not come from it.
func (a *contextWindow) compactBoundary() string {
	capacity := a.capacityCompactTrigger()
	if capacity <= 0 || a.compactRatio > 1 {
		return ""
	}
	if economic := a.economicCompactTrigger(); economic > 0 && economic < capacity {
		return "economic"
	}
	return "capacity"
}

// noteMaintenanceNoop reports one attempt that folded nothing. It writes no
// receipt and advances no generation: nothing was spent and nothing is blocked,
// so the next round is free to try again — which is exactly why the attempt
// would otherwise leave no trace at all.
func (a *contextWindow) noteMaintenanceNoop(trigger string, reason CompactionNoopReason, inputTokens int) {
	if a == nil || reason == "" || a.svc.sink == nil {
		return
	}
	turn := a.activeTurnCreatedAt.Load()
	if a.sess.win.compaction.lastReported.reason == reason && a.sess.win.compaction.lastReported.turn == turn {
		return
	}
	a.sess.win.compaction.lastReported = maintenanceReport{reason: reason, turn: turn}
	a.svc.sink.Emit(event.Event{Kind: event.ContextMaintenanceEvent, Maintenance: &event.ContextMaintenance{
		Status: "noop", Action: "summary", Trigger: trigger, Code: string(reason),
		Boundary: a.compactBoundary(), TriggerTokens: a.compactTrigger(), InputTokens: inputTokens,
	}})
}

// noteMaintenanceHeld tells the user that compaction is due but a failed attempt
// still holds the retry. It speaks once per turn and cause; the model is not
// told, because the hold has no model-visible symptom of its own.
func (a *contextWindow) noteMaintenanceHeld() {
	if a == nil || a.svc.sink == nil {
		return
	}
	a.sess.win.compactionMu.Lock()
	var code string
	if r := a.sess.win.compactionState.LastReceipt; r != nil {
		code = r.Code
	}
	a.sess.win.compactionMu.Unlock()
	reason := CompactionNoopReason(code)
	if reason == "" {
		reason = FailUnclassified
	}
	turn := a.activeTurnCreatedAt.Load()
	if a.sess.win.compaction.lastReported.reason == reason && a.sess.win.compaction.lastReported.turn == turn {
		return
	}
	a.sess.win.compaction.lastReported = maintenanceReport{reason: reason, turn: turn}
	a.svc.sink.Emit(event.Event{
		Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeCompactHeld, Detail: string(reason),
		Text: fmt.Sprintf("Automatic compaction is paused: the last attempt did not finish (%s).", reason),
	})
}

// recordContextMaintenanceBlocked persists a generation-scoped blocked receipt.
// code is empty where the verdict has no identity beyond its sentence.
func (a *contextWindow) recordContextMaintenanceBlocked(inputHash, trigger, action string, code CompactionNoopReason, reason string) {
	a.recordContextMaintenanceOutcome(inputHash, trigger, action, "blocked", code, reason)
}

// recordContextMaintenanceOutcome records blocked or failed for the current
// generation. Automatic Prepare will not re-enter summary until the generation
// advances, or, for a transient summary failure, the input has grown.
func (a *contextWindow) recordContextMaintenanceOutcome(inputHash, trigger, action, status string, code CompactionNoopReason, reason string) {
	if a == nil || a.sess.conversation == nil {
		return
	}
	if inputHash == "" {
		inputHash = a.contextMaintenanceInputHash(a.modelVisibleMessages())
	}
	if trigger == "" {
		trigger = CompactionTriggerPressure
	}
	if action == "" {
		action = "summary"
	}
	if status != "failed" {
		status = "blocked"
	}
	visible := a.modelVisibleMessages()
	inputTokens := a.estimatedVisibleRequestTokens(visible)
	_, transcriptVersion := a.sess.conversation.SnapshotMessagesVersion()
	promptCacheKey := a.currentPromptCacheKey()
	a.sess.win.compactionMu.Lock()
	state := a.sess.win.compactionState
	previous := state
	if state.LastReceipt != nil &&
		(state.LastReceipt.Status == "blocked" || state.LastReceipt.Status == "failed") &&
		state.LastReceipt.Action == action && !retriesOnGrowth(string(code)) {
		a.sess.win.compactionMu.Unlock()
		return
	}
	now := time.Now().UTC()
	state.SchemaVersion = sessionstore.CompactionStateSchemaCurrent
	state.TranscriptVersion = transcriptVersion
	state.PromptCacheKey = promptCacheKey
	// Do not advance projection version on failure; generation still advances so
	// CAS losers and concurrent writers cannot overwrite a newer success.
	state.Generation++
	// LastReceipt carries the blocked signal; clear legacy top-level mirrors.
	state.BlockedInputHash = ""
	state.BlockedReason = ""
	state.LastTrigger = ""
	state.LastMode = ""
	state.LastSourceTokens = 0
	state.LastResultTokens = 0
	state.LastReceipt = &sessionstore.ContextMaintenanceReceipt{
		OperationID: fmt.Sprintf("%s-%s-%d", status, action, state.Generation), Status: status, Action: action,
		Trigger: trigger, SourceProjection: state.Projection.ProjectionVersion,
		ProjectionVersion: state.Projection.ProjectionVersion, InputHash: inputHash, InputTokens: inputTokens,
		BlockedInputHash: inputHash, Reason: reason, CreatedAt: now,
		Code: string(code), Boundary: a.compactBoundary(), TriggerTokens: a.compactTrigger(),
	}
	state.UpdatedAt = now
	a.sess.win.compactionState = state
	if err := a.persistCompactionStateLocked(); err != nil {
		a.sess.win.compactionState = previous
		a.sess.win.compactionMu.Unlock()
		return
	}
	a.sess.win.compactionMu.Unlock()
	a.emitContextMaintenance(state.LastReceipt)
}

func (a *contextWindow) emitCompactionTelemetry(t CompactionTelemetry) {
	detail := fmt.Sprintf("trigger=%s mode=%s cache=%s src=%d fold=%d spans=%d proj=%d in=%d out=%d hit=%d miss=%d write=%d reqs=%d user_kept=%d user_dropped=%d",
		t.Trigger, t.Mode, t.CacheState, t.SourceTokens, t.FoldTokens, t.Spans, t.ProjectionTokens,
		t.InputTokens, t.OutputTokens, t.CacheHitTokens, t.CacheMissTokens, t.CacheWriteTokens, t.RequestCount,
		t.UserTurnsKept, t.UserTurnsDropped)
	if t.ProviderRequestID != "" {
		detail += " provider_request_id=" + t.ProviderRequestID
	}
	if t.Error != "" {
		// A degraded fold carries the summarizer's error but still freed the
		// context, so it is a notice with a cause rather than a failure.
		if t.Mode != CompactionModeDegraded {
			slog.Warn("agent: compaction failed", "detail", detail+" err_type="+t.Error)
			return
		}
		detail += " err_type=" + t.Error
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "compaction telemetry", Detail: detail})
}

// emitCompactionAborted closes the card of a fold that installed nothing. code
// is the class the host settled on, so the card can say why rather than only
// that nothing folded; it is empty where no class applies.
func (a *contextWindow) emitCompactionAborted(trigger string, code CompactionNoopReason) {
	a.svc.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{Trigger: trigger, Code: string(code)}})
}
