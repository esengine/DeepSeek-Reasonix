package control

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/jobs"
)

// errNoSessionPath is returned by snapshot when a session has content to persist
// but no path was ever set (e.g. no SetSessionPath call and no resume path). It is
// a loud failure so a misconfigured deployment surfaces instead of dropping data.
var errNoSessionPath = errors.New("session has content but no session path; conversation cannot be persisted")

func (c *Controller) hasUnfinishedSessionJobs(sessionPath string) bool {
	if c.jobs == nil {
		return false
	}
	return c.jobs.HasUnfinishedForSession(agent.BranchID(sessionPath))
}

func removeSessionArtifacts(path string) error {
	if path == "" {
		return nil
	}
	if err := jobs.RemoveArtifacts(path); err != nil {
		return err
	}
	for _, p := range []string{path, agent.BranchMetaPath(path)} {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if dir := ckptDir(path); dir != "" {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := agent.DeleteSubagentsByParent(filepath.Dir(path), agent.BranchID(path)); err != nil {
		return err
	}
	if err := agent.ClearCleanupPending(path); err != nil {
		return err
	}
	return nil
}

// ReconcileCleanupPending retries physical cleanup for logically removed
// sessions that were left behind by a previous process.
func ReconcileCleanupPending(dir string) error {
	return agent.ReconcileCleanupPending(dir, func(item agent.CleanupPendingInfo) error {
		return removeSessionArtifacts(item.SessionPath)
	})
}

// Snapshot writes the executor's conversation to the active session file. No-op
// when the executor is absent or the session has never been used (no user
// interaction). Returns errNoSessionPath when there IS content but no resolved
// path, so a misconfigured deployment surfaces instead of dropping data.
// Called after every turn so a crash loses at most one in-flight prompt.
func (c *Controller) Snapshot() error {
	return c.snapshot(false)
}

// SnapshotActivity writes the active conversation and marks the session as
// recently active. Use it only after a real user/model turn changes the
// transcript; switch/close snapshots should call Snapshot so they do not reorder
// recent-session pickers.
func (c *Controller) SnapshotActivity() error {
	return c.snapshot(true)
}

// midTurnSnapshotInterval is atomic (nanoseconds) so a test shrinking it
// cannot race a previous test's still-parking autosave goroutine.
var midTurnSnapshotInterval atomic.Int64

func init() { midTurnSnapshotInterval.Store(int64(30 * time.Second)) }

// autosaveWhileRunning snapshots the session periodically while a turn runs,
// so an abrupt kill (SSH drop, force-quit) loses at most one interval of a
// long turn instead of all of it (#3772). Session.Save copies under the lock
// and replaces the file atomically, so racing the turn's appends is safe.
func (c *Controller) autosaveWhileRunning(ctx context.Context) {
	t := time.NewTicker(time.Duration(midTurnSnapshotInterval.Load()))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.snapshot(false); err != nil {
				slog.Warn("controller: mid-turn snapshot", "err", err)
			}
		}
	}
}

func (c *Controller) snapshot(markActivity bool) error {
	c.mu.Lock()
	path := c.sessionPath
	modelRef := c.modelRef
	c.mu.Unlock()
	if c.executor == nil {
		return nil
	}
	s := c.executor.Session()
	if !s.HasContent() {
		// Nothing to persist yet (e.g. a fresh session with only a system
		// prompt) — staying quiet here is correct, not a data-loss path.
		return nil
	}
	if path == "" {
		// There IS content but nowhere to write it: this silently dropped whole
		// bot conversations (#4414). Surface it loudly instead of returning nil
		// so the missing session path can be diagnosed and fixed at the source.
		slog.Warn("controller: session has content but no session path; conversation will not be persisted",
			"label", c.Label(), "session_dir", c.SessionDir())
		return errNoSessionPath
	}
	if !markActivity {
		if _, err := agent.EnsureBranchMeta(path); err != nil {
			return err
		}
	}
	if err := s.Save(path); err != nil {
		return err
	}
	if strings.TrimSpace(modelRef) != "" {
		if err := agent.SetBranchModelPreserveUpdated(path, modelRef); err != nil {
			return err
		}
	}
	if markActivity {
		return agent.TouchBranchMeta(path)
	}
	return nil
}

func (c *Controller) messageCount() int {
	if c.executor == nil {
		return 0
	}
	return len(c.executor.Session().Snapshot())
}

func (c *Controller) snapshotActivityIfChanged(startMessages int) {
	if c.messageCount() <= startMessages {
		return
	}
	if err := c.SnapshotActivity(); err != nil {
		slog.Warn("controller: activity snapshot", "err", err)
	}
}

// SetSessionPath pins where auto-save lands (a fresh session file minted by the
// caller when no resume path applies).
func (c *Controller) SetSessionPath(p string) {
	c.mu.Lock()
	c.sessionPath = p
	c.mu.Unlock()
	c.setActiveJobSession(p)
	c.rebindCheckpoints(p)
}

// SessionDestroyHandle separates waiting for cancelled jobs from ending the
// destroy window, so callers can move/delete persistent artifacts in between.
type SessionDestroyHandle struct {
	Wait    func() jobs.TeardownResult
	WaitAll func()
	Finish  func()
	Async   bool
}

// BeginDestroySession marks a session as leaving active use and cancels its
// background jobs. Call Wait before moving/deleting artifacts, then Finish after
// persistent cleanup/move work is complete.
func (c *Controller) BeginDestroySession(sessionPath string) SessionDestroyHandle {
	parentSession := agent.BranchID(sessionPath)
	if c.jobs == nil || parentSession == "" {
		wait := func() jobs.TeardownResult { return jobs.TeardownResult{} }
		noop := func() {}
		return SessionDestroyHandle{Wait: wait, WaitAll: noop, Finish: noop}
	}
	teardown := c.jobs.BeginDestroySession(parentSession)
	return SessionDestroyHandle{
		Wait: func() jobs.TeardownResult {
			return c.jobs.WaitTeardown(context.Background(), teardown, c.jobs.TeardownGrace())
		},
		WaitAll: func() {
			for _, ch := range teardown.DoneChannels() {
				<-ch
			}
		},
		Finish: func() {
			c.jobs.FinishDestroySession(parentSession)
		},
		Async: teardown.Async(),
	}
}

// IsDestroyingSession reports whether sessionPath is currently in the destroy
// window for this controller's job manager.
func (c *Controller) IsDestroyingSession(sessionPath string) bool {
	if c.jobs == nil {
		return false
	}
	return c.jobs.IsDestroying(agent.BranchID(sessionPath))
}

func (c *Controller) setActiveJobSession(sessionPath string) {
	if c.jobs != nil {
		c.jobs.SetActiveSessionPath(agent.BranchID(sessionPath), sessionPath)
	}
}

func (c *Controller) sessionMessageCount() int {
	if c.executor == nil {
		return 0
	}
	return len(c.executor.Session().Messages)
}
