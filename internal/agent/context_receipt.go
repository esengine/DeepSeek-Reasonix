package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func (a *Agent) contextMaintenanceInputHash(visible []provider.Message) string {
	if a == nil {
		return ""
	}
	seed := a.currentPromptCacheKey() + "\n" + providerVisibleFingerprint(modelInputMessages(visible))
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func (a *Agent) contextMaintenanceBlocked(inputHash string) (bool, string) {
	if a == nil {
		return false, ""
	}
	a.sess.compactionMu.Lock()
	defer a.sess.compactionMu.Unlock()
	r := a.sess.compactionState.LastReceipt
	if r == nil {
		// Legacy sidecars may only have BlockedInputHash without a receipt.
		if a.sess.compactionState.BlockedInputHash != "" &&
			(inputHash == "" || a.sess.compactionState.BlockedInputHash == inputHash) {
			return true, a.sess.compactionState.BlockedReason
		}
		return false, ""
	}
	if r.Status != "blocked" && r.Status != "failed" {
		return false, ""
	}
	reason := firstNonEmpty(a.sess.compactionState.BlockedReason, r.Reason)
	// A failed view stays blocked for this generation. Changed input may retry
	// on a later turn, but not once per tool result in the same active turn.
	if r.BlockedInputHash != "" && inputHash != "" && r.BlockedInputHash != inputHash {
		turn := a.activeTurnCreatedAt.Load()
		if turn != 0 && a.sess.compaction.failedTurn.Load() == turn {
			return true, reason
		}
		return false, ""
	}
	return true, reason
}

func (a *Agent) emitContextMaintenance(r *ContextMaintenanceReceipt) {
	if a == nil || r == nil || a.svc.sink == nil {
		return
	}
	a.svc.sink.Emit(event.Event{Kind: event.ContextMaintenanceEvent, Maintenance: &event.ContextMaintenance{
		Status: r.Status, Action: r.Action, Trigger: r.Trigger, OperationID: r.OperationID,
		InputTokens: r.InputTokens, ResultTokens: r.ResultTokens, SavedTokens: r.SavedTokens,
		AffectedToolResults: r.AffectedToolResults, ProjectionVersion: r.ProjectionVersion,
		CacheBreak: r.CacheBreak, Reason: r.Reason,
	}})
}

// recordContextMaintenanceBlocked persists a generation-scoped blocked receipt.
func (a *Agent) recordContextMaintenanceBlocked(inputHash, trigger, action, reason string) {
	a.recordContextMaintenanceOutcome(inputHash, trigger, action, "blocked", reason)
}

// recordContextMaintenanceOutcome records blocked or failed for the current
// generation. Automatic Prepare will not re-enter summary until the generation
// advances (successful install, manual compress, or lineage change).
func (a *Agent) recordContextMaintenanceOutcome(inputHash, trigger, action, status, reason string) {
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
	_, transcriptVersion := a.sess.conversation.snapshotMessagesVersion()
	promptCacheKey := a.currentPromptCacheKey()
	a.sess.compactionMu.Lock()
	state := a.sess.compactionState
	previous := state
	// Suppress only repeated failures of the same view; a failure on a new
	// view must refresh the stored hash or later retries of that view are
	// never backed off.
	if state.LastReceipt != nil &&
		(state.LastReceipt.Status == "blocked" || state.LastReceipt.Status == "failed") &&
		state.LastReceipt.Action == action &&
		state.LastReceipt.BlockedInputHash == inputHash {
		a.sess.compactionMu.Unlock()
		return
	}
	now := time.Now().UTC()
	state.SchemaVersion = compactionStateSchemaCurrent
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
	state.LastReceipt = &ContextMaintenanceReceipt{
		OperationID: fmt.Sprintf("%s-%s-%d", status, action, state.Generation), Status: status, Action: action,
		Trigger: trigger, SourceProjection: state.Projection.ProjectionVersion,
		ProjectionVersion: state.Projection.ProjectionVersion, InputHash: inputHash,
		BlockedInputHash: inputHash, Reason: reason, CreatedAt: now,
	}
	state.UpdatedAt = now
	a.sess.compactionState = state
	if err := a.persistCompactionStateLocked(); err != nil {
		a.sess.compactionState = previous
		a.sess.compactionMu.Unlock()
		return
	}
	a.sess.compaction.failedTurn.Store(a.activeTurnCreatedAt.Load())
	a.sess.compactionMu.Unlock()
	a.emitContextMaintenance(state.LastReceipt)
}

func (a *Agent) emitCompactionTelemetry(t CompactionTelemetry) {
	detail := fmt.Sprintf("trigger=%s mode=%s summary_input=%s cache=%s src=%d fold=%d spans=%d proj=%d in=%d out=%d hit=%d miss=%d write=%d reqs=%d user_kept=%d user_dropped=%d",
		t.Trigger, t.Mode, t.SummaryInputMode, t.CacheState, t.SourceTokens, t.FoldTokens, t.Spans, t.ProjectionTokens,
		t.InputTokens, t.OutputTokens, t.CacheHitTokens, t.CacheMissTokens, t.CacheWriteTokens, t.RequestCount,
		t.UserTurnsKept, t.UserTurnsDropped)
	if t.ProviderRequestID != "" {
		detail += " provider_request_id=" + t.ProviderRequestID
	}
	if t.Error != "" {
		// CompactionModeDegraded remains readable for legacy telemetry, although
		// new summarizer failures never install a degraded projection.
		if t.Mode != CompactionModeDegraded {
			slog.Warn("agent: compaction failed", "detail", detail+" err_type="+t.Error)
			return
		}
		detail += " err_type=" + t.Error
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "compaction telemetry", Detail: detail})
}

func (a *Agent) emitCompactionAborted(trigger string) {
	a.svc.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{Trigger: trigger}})
}
