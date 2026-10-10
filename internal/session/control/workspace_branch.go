package control

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/workspacelease"

	"reasonix/internal/platform/gitstatus"
)

var (
	// ErrWorkspaceBusy means a checkout or an external write claim prevents switching.
	ErrWorkspaceBusy = errors.New("control: workspace is busy")
	// ErrJobsRunning means unfinished background work still holds the workspace.
	ErrJobsRunning = errors.New("control: background jobs are running in the workspace")
)

const workspaceBranchLeaseWait = 150 * time.Millisecond

// SwitchWorkspaceBranch checks out another local branch while the workspace
// is idle. Panes in this host share the admission guard; the write lease also
// honors existing cross-process write claims through checkout and summary.
func (c *Controller) SwitchWorkspaceBranch(ctx context.Context, name string) (gitstatus.Info, bool, error) {
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	if err := c.workspaceActivityConflict(); err != nil {
		return gitstatus.Info{}, false, err
	}
	releaseLease, err := c.acquireBranchWriteLease(ctx)
	if err != nil {
		return gitstatus.Info{}, false, err
	}
	defer releaseLease()
	releaseWorkspace, err := c.excludeWorkspaceActivity()
	if err != nil {
		return gitstatus.Info{}, false, err
	}
	defer releaseWorkspace()
	c.mu.Lock()
	busy := c.gate.busy()
	c.mu.Unlock()
	if busy {
		return gitstatus.Info{}, false, ErrTurnRunning
	}
	if err := gitstatus.SwitchBranch(ctx, c.workspaceRepo, name); err != nil {
		return gitstatus.Info{}, false, err
	}
	info, ok := gitstatus.Summary(ctx, c.workspaceRepo)
	return info, ok, nil
}

// Lease contention must not prevent other panes from admitting work.
func (c *Controller) acquireBranchWriteLease(ctx context.Context) (func(), error) {
	if !c.workspaceRepo.Valid() {
		return func() {}, nil
	}
	lease, err := workspacelease.New(c.workspaceRepo.WorkTree, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		return nil, err
	}
	lease.BeginRun()
	leaseCtx, cancel := context.WithTimeout(ctx, workspaceBranchLeaseWait)
	defer cancel()
	if err := lease.AcquireWrite(leaseCtx); err != nil {
		lease.EndRun()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, workspacelease.ErrConflict) {
			return nil, fmt.Errorf("%w: %w", ErrWorkspaceBusy, err)
		}
		return nil, err
	}
	return lease.EndRun, nil
}
