package checkpoint

import "time"

func newRewindTransaction(root string, plan RewindPlan) *TransactionManifest {
	scope := plan.Scope
	hasBoundary := plan.HasBoundary
	boundaryIndex := plan.BoundaryIndex
	if plan.ConversationAction == "fork" {
		// The fork is durable before this transaction starts and the parent is
		// unchanged. Keep file undo/compensation entirely conversation-free.
		if scope == RewindBoth {
			scope = RewindCode
		}
		hasBoundary = false
		boundaryIndex = 0
	}
	return &TransactionManifest{
		SchemaVersion:      SchemaV2,
		ID:                 newID("tx"),
		WorkspaceRoot:      root,
		State:              TxPrepared,
		Kind:               "rewind",
		Turn:               plan.Turn,
		Scope:              scope,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		SessionRevision:    plan.SessionRevision,
		WorkspaceToken:     plan.WorkspaceToken,
		Coverage:           plan.Coverage,
		CoverageGaps:       append([]CoverageGap(nil), plan.CoverageGaps...),
		BoundaryIndex:      boundaryIndex,
		HasBoundary:        hasBoundary,
		ConversationAction: plan.ConversationAction,
		TruncateFrom:       plan.Turn,
	}
}

func setTransactionConversationForward(tx *TransactionManifest, forward []byte) {
	if shouldTruncateConversation(tx) {
		tx.ConversationForward = forward
	}
}
