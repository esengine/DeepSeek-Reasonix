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
	seed := a.currentPromptCacheKey() + "\n" + providerVisibleFingerprint(provider.ModelMessages(visible))
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func (a *Agent) contextMaintenanceBlocked(inputHash string) (bool, string) {
	if a == nil {
		return false, ""
	}
	a.compactionMu.Lock()
	defer a.compactionMu.Unlock()
	r := a.compactionState.LastReceipt
	if r == nil {
		// Legacy sidecars may only have BlockedInputHash without a receipt.
		if a.compactionState.BlockedInputHash != "" &&
			(inputHash == "" || a.compactionState.BlockedInputHash == inputHash) {
			return true, a.compactionState.BlockedReason
		}
		return false, ""
	}
	if r.Status != "blocked" && r.Status != "failed" {
		return false, ""
	}
	// Generation-scoped: once this generation fails, automatic maintenance
	// does not pay for another summary until a successful install, manual
	// compress, or lineage change advances the generation.
	return true, firstNonEmpty(a.compactionState.BlockedReason, r.Reason)
}

func (a *Agent) emitContextMaintenance(r *ContextMaintenanceReceipt) {
	if a == nil || r == nil || a.sink == nil {
		return
	}
	a.sink.Emit(event.Event{Kind: event.ContextMaintenanceEvent, Maintenance: &event.ContextMaintenance{
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
	if a == nil || a.session == nil {
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
	_, transcriptVersion := a.session.snapshotMessagesVersion()
	promptCacheKey := a.currentPromptCacheKey()
	a.compactionMu.Lock()
	state := a.compactionState
	previous := state
	if state.LastReceipt != nil &&
		(state.LastReceipt.Status == "blocked" || state.LastReceipt.Status == "failed") &&
		state.LastReceipt.Action == action {
		a.compactionMu.Unlock()
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
	a.compactionState = state
	if err := a.persistCompactionStateLocked(); err != nil {
		a.compactionState = previous
		a.compactionMu.Unlock()
		return
	}
	a.compactionMu.Unlock()
	a.emitContextMaintenance(state.LastReceipt)
}

func (a *Agent) emitCompactionTelemetry(t CompactionTelemetry) {
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
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "compaction telemetry", Detail: detail})
}

func (a *Agent) emitCompactionAborted(trigger string) {
	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{Trigger: trigger}})
}
