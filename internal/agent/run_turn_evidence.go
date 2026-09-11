package agent

import (
	"context"

	"reasonix/internal/jobs"
)

// leasePendingBackgroundEvidence re-leases this session's uncommitted job
// mutations after the new turn resets a failed or cancelled turn's ledger.
// Process restarts also begin with an empty ledger while the job manager still
// owns the uncommitted evidence. Plan turns defer the lease until approval.
func (a *Agent) leasePendingBackgroundEvidence(ctx context.Context) {
	if a.task.ledger == nil || a.svc.jobs == nil || a.planMode.Load() {
		return
	}
	session := jobs.SessionFromContext(ctx)
	for _, jobID := range a.svc.jobs.PendingEvidenceJobIDsForSession(session) {
		summary, ready := a.svc.jobs.TryLeaseEvidenceForSession(session, jobID)
		if !ready || !a.task.ledger.NoteBackgroundLease(session, jobID) {
			continue
		}
		a.task.ledger.MergeChild(summary)
	}
}
