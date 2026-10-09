package boot

import (
	"fmt"
	"reasonix/internal/state/sessionstore"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/jobs"
)

// sessionRuntime is the session-scoped machinery a build hands to the
// controller: the workspace write lease, the background job manager that
// retains it while a job runs, and the directory sessions are read from.
type sessionRuntime struct {
	lease *workspacelease.Owner
	jobs  *jobs.Manager
	dir   string
}

// startSessionRuntime acquires the workspace lease and opens the job manager.
// Every role setting lazily acquires the lease on the first real writer, so
// read-only turns never take it.
func startSessionRuntime(opts Options, cfg *config.Config, root string, sink event.Sink) (sessionRuntime, error) {
	jobOptions := []jobs.Option{
		jobs.WithStalledWarningAfter(time.Duration(cfg.BackgroundJobStalledWarningSeconds()) * time.Second),
		jobs.WithSessionOwnershipProbe(sessionstore.SessionLeaseHeldByCurrentRuntime),
	}
	// The lease mode is a user setting: "off" takes no cross-session lease at
	// all, "optimistic" stops an undeclaring writer from holding the whole
	// workspace against other sessions, and "strict" holds it for every
	// writer. Each runtime builds its lease with the mode it was configured
	// with; a live runtime keeps the lease it was built with.
	var leaseOptions []workspacelease.Option
	switch {
	case cfg.Agent.SkipWriteLease():
		leaseOptions = append(leaseOptions, workspacelease.WithoutWriteSerialization())
	case cfg.Agent.RelaxedWriteLease():
		leaseOptions = append(leaseOptions, workspacelease.WithoutWholeWorkspaceHold())
	}
	lease, err := workspacelease.New(root, config.WorkspaceLeaseDir(), func(w workspacelease.Wait) {
		sink.Emit(workspaceLeaseNotice(w))
	}, leaseOptions...)
	if err != nil {
		return sessionRuntime{}, fmt.Errorf("initialize workspace write lease: %w", err)
	}
	lease.OnRelease(func(st workspacelease.Stats) { sink.Emit(workspaceLeaseAccount(st)) })
	manager := jobs.NewManager(sink, jobOptions...)

	dir := opts.SessionDir
	if dir == "" {
		dir = opts.roots().SessionDir()
	}
	reconcileCleanupPending := opts.CleanupPendingReconciler
	if reconcileCleanupPending == nil {
		reconcileCleanupPending = control.ReconcileCleanupPending
	}
	if err := reconcileCleanupPending(dir); err != nil {
		report(sink, event.Event{Level: event.LevelWarn, Text: "cleanup-pending reconciliation failed: " + err.Error()})
	}
	return sessionRuntime{lease: lease, jobs: manager, dir: dir}, nil
}

// workspaceLeaseAccount carries what serialising writers cost this session.
// The notice above reports a wait a person sat through; this reports every
// wait, including the ones under the grace, and how much of the hold was spent
// writing nothing.
func workspaceLeaseAccount(st workspacelease.Stats) event.Event {
	ms := func(d time.Duration) int64 { return d.Milliseconds() }
	return event.Event{
		Kind: event.WorkspaceLeaseEvent,
		WorkspaceLease: &event.WorkspaceLease{
			Contended: st.Contended, Reported: st.Reported,
			WaitedMs: ms(st.Waited), HeldMs: ms(st.Held), IdleMs: ms(st.Idle),
		},
	}
}

// workspaceLeaseNotice turns one reported wait into what a frontend can
// resolve. The wait is a warning because the turn is stopped for the length of
// it; its close is not, and carries the measured wait so nothing is left on
// screen still claiming a wait that is over.
func workspaceLeaseNotice(w workspacelease.Wait) event.Event {
	waited := w.Elapsed.Round(100 * time.Millisecond)
	switch w.Outcome {
	case workspacelease.WaitAcquired:
		return event.Event{
			Kind: event.Notice, Level: event.LevelInfo,
			Code:           event.NoticeCodeWorkspaceLeaseResumed,
			Text:           "This session's requested write claim was granted; it has continued.",
			Detail:         fmt.Sprintf("waited %s for the workspace write lease", waited),
			WorkspaceLease: workspaceLeaseScope(w),
		}
	case workspacelease.WaitAbandoned:
		return event.Event{
			Kind: event.Notice, Level: event.LevelInfo,
			Code:           event.NoticeCodeWorkspaceLeaseAbandoned,
			Text:           "The wait for the workspace ended before this session's turn to write came.",
			Detail:         fmt.Sprintf("waited %s; the turn was cancelled or timed out first", waited),
			WorkspaceLease: workspaceLeaseScope(w),
		}
	default:
		return event.Event{
			Kind: event.Notice, Level: event.LevelWarn,
			Code: event.NoticeCodeWorkspaceLease,
			Text: "Another session holds an overlapping write claim; this session will continue automatically when its claim is available.",
			// The holder's own name, as data: which conversation to go and
			// look at is the one thing the text above cannot say.
			Detail:         fmt.Sprintf("session %q (%q) holds %q; requested %q", w.Holder, w.HolderSessionID, w.Paths, w.RequestedPaths),
			WorkspaceLease: workspaceLeaseScope(w),
		}
	}
}

func workspaceLeaseScope(w workspacelease.Wait) *event.WorkspaceLease {
	return &event.WorkspaceLease{Holder: w.Holder, HolderSessionID: w.HolderSessionID,
		Paths: w.Paths, RequestedPaths: w.RequestedPaths}
}
