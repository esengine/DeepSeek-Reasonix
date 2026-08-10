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
	if a == nil || inputHash == "" {
		return false, ""
	}
	a.compactionMu.Lock()
	defer a.compactionMu.Unlock()
	if a.compactionState.BlockedInputHash != inputHash {
		return false, ""
	}
	return true, a.compactionState.BlockedReason
}

func (a *Agent) observeNativeContextEditing(u *provider.Usage) {
	if a == nil || u == nil || a.effectiveContextEditing() != "native" ||
		u.ContextEditingType == "" || u.ContextEditingClearedTokens <= 0 {
		return
	}
	inputHash := a.contextMaintenanceInputHash(a.modelVisibleMessages())
	operationID := fmt.Sprintf("native-tool-clear-%s-%d-%d", inputHash, u.ContextEditingClearedToolUses, u.ContextEditingClearedTokens)
	a.compactionMu.Lock()
	state := a.compactionState
	if state.LastReceipt != nil && state.LastReceipt.OperationID == operationID {
		a.compactionMu.Unlock()
		return
	}
	now := time.Now().UTC()
	resultTokens := u.LatestPromptTokens()
	receipt := &ContextMaintenanceReceipt{
		OperationID: operationID, Status: "applied", Action: "native_tool_clear",
		Trigger: CompactionTriggerPressure, SourceProjection: state.Projection.ProjectionVersion,
		ProjectionVersion: state.Projection.ProjectionVersion, InputHash: inputHash,
		InputTokens: resultTokens + u.ContextEditingClearedTokens, ResultTokens: resultTokens,
		SavedTokens: u.ContextEditingClearedTokens, AffectedToolResults: u.ContextEditingClearedToolUses,
		CacheBreak: true, CreatedAt: now,
	}
	previous := state
	state.SchemaVersion = compactionStateSchemaCurrent
	state.Generation++
	state.LastTrigger = CompactionTriggerPressure
	state.LastMode = CompactionModeNative
	state.LastReceipt = receipt
	state.UpdatedAt = now
	a.compactionState = state
	if err := a.persistCompactionStateLocked(); err != nil {
		a.compactionState = previous
		a.compactionMu.Unlock()
		return
	}
	a.compactionMu.Unlock()
	a.emitContextMaintenance(receipt)
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

// recordContextMaintenanceBlocked persists one failed visible-view fingerprint.
func (a *Agent) recordContextMaintenanceBlocked(inputHash, trigger, action, reason string) {
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
		action = "noop"
	}
	_, transcriptVersion := a.session.snapshotMessagesVersion()
	promptCacheKey := a.currentPromptCacheKey()
	a.compactionMu.Lock()
	state := a.compactionState
	previous := state
	if state.BlockedInputHash == inputHash && state.LastReceipt != nil && state.LastReceipt.Status == "blocked" {
		a.compactionMu.Unlock()
		return
	}
	now := time.Now().UTC()
	state.SchemaVersion = compactionStateSchemaCurrent
	state.TranscriptVersion = transcriptVersion
	state.PromptCacheKey = promptCacheKey
	state.Generation++
	state.BlockedInputHash = inputHash
	state.BlockedReason = reason
	state.LastReceipt = &ContextMaintenanceReceipt{
		OperationID: fmt.Sprintf("blocked-%s", inputHash), Status: "blocked", Action: action,
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
	detail := fmt.Sprintf("trigger=%s mode=%s cache=%s src=%d fold=%d spans=%d proj=%d in=%d out=%d hit=%d miss=%d write=%d reqs=%d",
		t.Trigger, t.Mode, t.CacheState, t.SourceTokens, t.FoldTokens, t.Spans, t.ProjectionTokens,
		t.InputTokens, t.OutputTokens, t.CacheHitTokens, t.CacheMissTokens, t.CacheWriteTokens, t.RequestCount)
	if t.ProviderRequestID != "" {
		detail += " provider_request_id=" + t.ProviderRequestID
	}
	if t.Error != "" {
		slog.Warn("agent: compaction failed", "detail", detail+" err_type="+t.Error)
		return
	}
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "compaction telemetry", Detail: detail})
}

func (a *Agent) emitCompactionAborted(trigger string) {
	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{Trigger: trigger}})
}
