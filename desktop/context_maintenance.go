package main

import (
	"time"

	"reasonix/internal/agent"
)

// ContextInfo is the prompt-vs-window gauge payload plus session totals. Used
// and Window both zero means no context-window data yet.
type ContextInfo struct {
	Used            int                         `json:"used"`
	Window          int                         `json:"window"`
	SessionTokens   int                         `json:"sessionTokens"`
	CompactRatio    float64                     `json:"compactRatio,omitempty"`
	SessionCost     float64                     `json:"sessionCost,omitempty"`
	SessionCurrency string                      `json:"sessionCurrency,omitempty"`
	CacheHitTokens  int                         `json:"cacheHitTokens,omitempty"`
	CacheMissTokens int                         `json:"cacheMissTokens,omitempty"`
	Estimated       bool                        `json:"estimated,omitempty"`
	Sources         map[string]usageSourceStats `json:"sources,omitempty"`
	Maintenance     *ContextMaintenanceInfo     `json:"maintenance,omitempty"`
}

// ContextMaintenanceInfo is the Wails-safe current-view snapshot. Optional
// fields preserve compatibility with older desktop/front-end combinations.
type ContextMaintenanceInfo struct {
	CanonicalTokens   int                            `json:"canonicalTokens,omitempty"`
	ProjectedTokens   int                            `json:"projectedTokens,omitempty"`
	SummaryTokens     int                            `json:"summaryTokens,omitempty"`
	LastSavedTokens   int                            `json:"lastSavedTokens,omitempty"`
	SnipTrigger       int                            `json:"snipTrigger,omitempty"`
	FoldTrigger       int                            `json:"foldTrigger,omitempty"`
	ForceTrigger      int                            `json:"forceTrigger,omitempty"`
	HardInputCeiling  int                            `json:"hardInputCeiling,omitempty"`
	Headroom          int                            `json:"headroom,omitempty"`
	ProjectionVersion uint64                         `json:"projectionVersion,omitempty"`
	Blocked           bool                           `json:"blocked,omitempty"`
	LastReceipt       *ContextMaintenanceReceiptInfo `json:"lastReceipt,omitempty"`
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
		SnipTrigger: snapshot.SnipTrigger, FoldTrigger: snapshot.FoldTrigger,
		ForceTrigger: snapshot.ForceTrigger, HardInputCeiling: snapshot.HardInputCeiling,
		Headroom: snapshot.Headroom, ProjectionVersion: snapshot.ProjectionVersion,
		Blocked: snapshot.Blocked,
	}
	if source := snapshot.LastReceipt; source != nil {
		info.LastReceipt = contextMaintenanceReceiptInfo(source)
	}
	return info
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
