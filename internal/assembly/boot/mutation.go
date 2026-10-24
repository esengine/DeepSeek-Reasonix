package boot

import (
	"context"
	"fmt"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/workspacelease"
	"slices"
	"time"
)

func buildMutationCoordinator(ctx context.Context, cfg *config.Config, root string, sink event.Sink, repo gitcmd.Repo, extraRoots ...string) (*mutation.Coordinator, error) {
	workspaceRepos, resources, err := mutationRepositories(ctx, repo, extraRoots)
	if err != nil {
		return nil, err
	}
	return newMutationCoordinator(cfg, root, sink, workspaceRepos, resources)
}

func newMutationCoordinator(cfg *config.Config, root string, sink event.Sink, workspaceRepos []gitcmd.Repo, resources []string) (*mutation.Coordinator, error) {
	mode, err := cfg.CollaborationMode()
	if err != nil {
		return nil, err
	}
	if mode == "legacy" {
		return nil, recoveryBarrier(workspaceRepos, nil)
	}
	domain, err := workspacelease.CanonicalWorkspace(root)
	if err != nil {
		return nil, err
	}
	return mutation.New(domain, config.WorkspaceLeaseDir(), func(n mutation.Notice) {
		if e, ok := mutationNotice(n); ok {
			sink.Emit(e)
		}
	}, mutation.WithContextAdmissionCheck(func(ctx context.Context, intent mutation.Intent) error {
		return repositoriesAdmission(ctx, workspaceRepos, intent)
	}), mutation.WithWorkspaceResources(resources...))
}

func mutationNotice(n mutation.Notice) (event.Event, bool) {
	outcome := workspacelease.WaitBegan
	switch n.State {
	case "waiting":
	case "acquired":
		outcome = workspacelease.WaitAcquired
	case "abandoned":
		outcome = workspacelease.WaitAbandoned
	default:
		return event.Event{}, false
	}
	e := workspaceLeaseNotice(workspacelease.Wait{Outcome: outcome, Elapsed: n.Elapsed,
		HolderSessionID: n.HolderSessionID, Paths: n.HolderPaths, RequestedPaths: n.Paths})
	if n.State == "abandoned" {
		e.Detail = fmt.Sprintf("waited %s; transaction ended before mutation", n.Elapsed.Round(100*time.Millisecond))
	}
	w := e.WorkspaceLease
	w.ResourceKind = string(n.Effect)
	if n.BlockedOn != "" {
		w.ResourceKind = string(n.BlockedOn)
	}
	if w.ResourceKind == string(mutation.CapacityOnly) {
		switch n.State {
		case "waiting":
			e.Text = "Waiting for execution capacity; the request will continue when capacity is available."
		case "acquired":
			e.Text = "Execution capacity was granted; the request has continued."
		case "abandoned":
			e.Text = "The wait for execution capacity ended before the request started."
		}
		e.Detail = fmt.Sprintf("waited %s for execution capacity", n.Elapsed.Round(100*time.Millisecond))
	}
	w.TransactionID, w.SessionID, w.ExecutionID = n.TransactionID, n.SessionID, n.ExecutionID
	w.CallID, w.State, w.Reason = n.CallID, n.State, n.Reason
	w.StartedAt, w.WaitedMs = n.StartedAt.UTC().Format(time.RFC3339Nano), n.Elapsed.Milliseconds()
	w.HolderTransactionID, w.HolderExecutionID = n.HolderTransactionID, n.HolderExecutionID
	e.Detail += fmt.Sprintf("; transaction %q; execution %q; call %q; state %q", n.TransactionID, n.ExecutionID, n.CallID, n.State)
	if n.Reason != "" {
		e.Detail += fmt.Sprintf("; reason %q", n.Reason)
	}
	return e, true
}

func integrationAdmission(repo gitcmd.Repo) func(mutation.Intent) error {
	return func(intent mutation.Intent) error {
		if intent.Effect == mutation.CapacityOnly {
			return nil
		}
		if !intent.Recovery {
			if err := worktree.CheckApplyRecovery(repo); err != nil {
				return tool.Refusal{Code: "isolation.recovery_required", Message: err.Error()}
			}
			return nil
		}
		paths, err := worktree.ApplyRecoveryPaths(worktree.Snapshot{Repo: repo})
		if err != nil {
			return err
		}
		for _, path := range paths {
			canonical, err := fileutil.CanonicalWritePath(path)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(intent.Paths, func(root string) bool { return fileutil.AtOrUnder(canonical, root) }) {
				return tool.Refusal{Code: "isolation.recovery_required", Message: "recovery resources changed; retry integration"}
			}
		}
		return nil
	}
}
