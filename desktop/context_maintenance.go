package main

import (
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
)

// ContextInfo is the prompt-vs-window gauge payload plus session totals. Used
// and Window both zero means no context-window data yet.
type ContextInfo struct {
	Used                int                         `json:"used"`
	Window              int                         `json:"window"`
	SessionTokens       int                         `json:"sessionTokens"`
	CompactRatio        float64                     `json:"compactRatio,omitempty"`
	SessionCost         float64                     `json:"sessionCost,omitempty"`
	SessionCurrency     string                      `json:"sessionCurrency,omitempty"`
	CacheHitTokens      int                         `json:"cacheHitTokens,omitempty"`
	CacheMissTokens     int                         `json:"cacheMissTokens,omitempty"`
	Estimated           bool                        `json:"estimated,omitempty"`
	SessionCostComplete bool                        `json:"sessionCostComplete,omitempty"`
	SessionCostQuote    *billing.CostQuote          `json:"sessionCostQuote,omitempty"`
	Sources             map[string]usageSourceStats `json:"sources,omitempty"`
	Maintenance         *ContextMaintenanceInfo     `json:"maintenance,omitempty"`
	ContextBudget       *ContextBudgetInfo          `json:"contextBudget,omitempty"`
}

// ContextMaintenanceInfo is the Wails-safe current-view snapshot. Optional
// fields preserve compatibility with older desktop/front-end combinations.
type ContextMaintenanceInfo struct {
	CanonicalTokens   int                            `json:"canonicalTokens,omitempty"`
	ProjectedTokens   int                            `json:"projectedTokens,omitempty"`
	SummaryTokens     int                            `json:"summaryTokens,omitempty"`
	LastSavedTokens   int                            `json:"lastSavedTokens,omitempty"`
	SnipTrigger       int                            `json:"snipTrigger,omitempty"`  // always 0; legacy compatibility
	FoldTrigger       int                            `json:"foldTrigger,omitempty"`  // alias of TriggerTokens
	ForceTrigger      int                            `json:"forceTrigger,omitempty"` // always 0; legacy compatibility
	TriggerTokens     int                            `json:"triggerTokens,omitempty"`
	CheckpointState   string                         `json:"checkpointState,omitempty"` // none|restored|applied
	HardInputCeiling  int                            `json:"hardInputCeiling,omitempty"`
	Headroom          int                            `json:"headroom,omitempty"`
	ProjectionVersion uint64                         `json:"projectionVersion,omitempty"`
	Blocked           bool                           `json:"blocked,omitempty"`
	LastReceipt       *ContextMaintenanceReceiptInfo `json:"lastReceipt,omitempty"`
	ContextBudget     *ContextBudgetInfo             `json:"contextBudget,omitempty"`
}

// ContextBudgetInfo is the optional send-time admission view. Older desktop
// builds ignore unknown fields; omission is the compatibility default.
type ContextBudgetInfo struct {
	WindowMode            string `json:"windowMode,omitempty"`
	LimitMode             string `json:"limitMode,omitempty"`
	Source                string `json:"source,omitempty"`
	WindowTokens          int    `json:"windowTokens,omitempty"`
	PromptTokens          int    `json:"promptTokens,omitempty"`
	AutoOutputTokens      int    `json:"autoOutputTokens,omitempty"`
	MaxOutputTokens       int    `json:"maxOutputTokens,omitempty"`
	RequestedOutputTokens int    `json:"requestedOutputTokens,omitempty"`
	EffectiveOutputTokens int    `json:"effectiveOutputTokens,omitempty"`
	ReserveTokens         int    `json:"reserveTokens,omitempty"`
	PhysicalRemaining     int    `json:"physicalRemaining,omitempty"`
	Clipped               bool   `json:"clipped,omitempty"`
	LastRecovery          string `json:"lastRecovery,omitempty"`
	ObservedWindow        int    `json:"observedWindow,omitempty"`
	ObservedPrompt        int    `json:"observedPrompt,omitempty"`
	ObservedCompletion    int    `json:"observedCompletion,omitempty"`
}

type ContextMaintenanceReceiptInfo struct {
	OperationID         string `json:"operationId,omitempty"`
	Status              string `json:"status,omitempty"`
	Action              string `json:"action,omitempty"`
	Trigger             string `json:"trigger,omitempty"`
	SourceProjection    uint64 `json:"sourceProjection,omitempty"`
	ProjectionVersion   uint64 `json:"projectionVersion,omitempty"`
	CoveredCount        int    `json:"coveredCount,omitempty"`
	CoveredPrefixHash   string `json:"coveredPrefixHash,omitempty"`
	InputHash           string `json:"inputHash,omitempty"`
	OutputHash          string `json:"outputHash,omitempty"`
	InputTokens         int    `json:"inputTokens,omitempty"`
	ResultTokens        int    `json:"resultTokens,omitempty"`
	SavedTokens         int    `json:"savedTokens,omitempty"`
	AffectedToolResults int    `json:"affectedToolResults,omitempty"`
	SummaryHash         string `json:"summaryHash,omitempty"`
	Archive             string `json:"archive,omitempty"`
	CacheBreak          bool   `json:"cacheBreak,omitempty"`
	Reason              string `json:"reason,omitempty"`
	BlockedInputHash    string `json:"blockedInputHash,omitempty"`
	CreatedAt           string `json:"createdAt,omitempty"`
}

func contextMaintenanceInfo(snapshot agent.ContextMaintenanceSnapshot) *ContextMaintenanceInfo {
	if snapshot.CanonicalTokens == 0 && snapshot.ProjectedTokens == 0 && snapshot.LastReceipt == nil {
		return nil
	}
	info := &ContextMaintenanceInfo{
		CanonicalTokens: snapshot.CanonicalTokens, ProjectedTokens: snapshot.ProjectedTokens,
		SummaryTokens: snapshot.SummaryTokens, LastSavedTokens: snapshot.LastSavedTokens,
		// Snip/Force remain zero for one-version compatibility with older frontends.
		FoldTrigger: snapshot.TriggerTokens, TriggerTokens: snapshot.TriggerTokens,
		CheckpointState: snapshot.CheckpointState, HardInputCeiling: snapshot.HardInputCeiling,
		Headroom: snapshot.Headroom, ProjectionVersion: snapshot.ProjectionVersion,
		Blocked: snapshot.Blocked,
	}
	if snapshot.ContextBudget != nil {
		info.ContextBudget = contextBudgetInfo(snapshot.ContextBudget)
	}
	if source := snapshot.LastReceipt; source != nil {
		info.LastReceipt = contextMaintenanceReceiptInfo(source)
	}
	return info
}

func contextBudgetInfo(source *agent.ContextBudgetSnapshot) *ContextBudgetInfo {
	if source == nil {
		return nil
	}
	return &ContextBudgetInfo{
		WindowMode: source.WindowMode, LimitMode: source.LimitMode, Source: source.Source,
		WindowTokens: source.WindowTokens, PromptTokens: source.PromptTokens,
		AutoOutputTokens: source.AutoOutputTokens, MaxOutputTokens: source.MaxOutputTokens,
		RequestedOutputTokens: source.RequestedOutputTokens, EffectiveOutputTokens: source.EffectiveOutputTokens,
		ReserveTokens: source.ReserveTokens, PhysicalRemaining: source.PhysicalRemaining,
		Clipped: source.Clipped, LastRecovery: source.LastRecovery,
		ObservedWindow: source.ObservedWindow, ObservedPrompt: source.ObservedPrompt,
		ObservedCompletion: source.ObservedCompletion,
	}
}

func contextMaintenanceReceiptInfo(source *agent.ContextMaintenanceReceipt) *ContextMaintenanceReceiptInfo {
	receipt := &ContextMaintenanceReceiptInfo{
		OperationID: source.OperationID, Status: source.Status, Action: source.Action, Trigger: source.Trigger,
		SourceProjection: source.SourceProjection, ProjectionVersion: source.ProjectionVersion,
		CoveredCount: source.CoveredCount, CoveredPrefixHash: source.CoveredPrefixHash,
		InputHash: source.InputHash, OutputHash: source.OutputHash,
		InputTokens: source.InputTokens, ResultTokens: source.ResultTokens, SavedTokens: source.SavedTokens,
		AffectedToolResults: source.AffectedToolResults, SummaryHash: source.SummaryHash, Archive: source.Archive,
		CacheBreak: source.CacheBreak, Reason: source.Reason, BlockedInputHash: source.BlockedInputHash,
	}
	if !source.CreatedAt.IsZero() {
		receipt.CreatedAt = source.CreatedAt.Format(time.RFC3339Nano)
	}
	return receipt
}
