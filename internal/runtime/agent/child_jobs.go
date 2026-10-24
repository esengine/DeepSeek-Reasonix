package agent

import (
	"context"

	"reasonix/internal/safety/evidence"
	"reasonix/internal/tools/jobs"
)

func settleChildJobs(ctx context.Context, sub *Agent, manager *jobs.Manager, runErr *error) {
	if !jobs.JoinExecution(ctx, manager, runErr) || sub == nil || sub.task.ledger == nil {
		return
	}
	session := jobs.SessionFromContext(ctx)
	var pending []evidence.ChildEvidenceSummary
	for _, id := range manager.PendingEvidenceJobIDsForSession(session) {
		if sub.task.ledger.NoteBackgroundLease(session, id) {
			pending = append(pending, manager.LeaseEvidenceForSession(session, id))
		}
	}
	sub.task.ledger.MergeConcurrentChildren(pending...)
}
