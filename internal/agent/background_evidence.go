package agent

import (
	"context"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
)

// publishBackgroundEvidence retains host-only workspace context so persisted
// task metadata classifies paths without treating checkout ancestors as scope.
func publishBackgroundEvidence(ctx context.Context, ledger *evidence.Ledger, workspaceRoot string) {
	if ledger == nil {
		return
	}
	summary := ledger.Summary()
	summary.WorkspaceRoot = strings.TrimSpace(workspaceRoot)
	jobs.PublishEvidence(ctx, summary)
}
