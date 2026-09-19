package control

import (
	"context"
	"errors"
	"fmt"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
)

// ClearSession discards the current conversation without preserving it in
// resume/history, then rotates to a clean session carrying the same base system
// prompt and no pinned context.
func (c *Controller) ClearSession() error {
	if c.executor == nil {
		return nil
	}
	// Same rotation gate as NewSession: hold it across the whole
	// destroy-then-swap so a turn cannot start during the sequence and have its
	// live session replaced.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return fmt.Errorf("cannot clear while a turn is running: %w", err)
		}
		return err
	}
	defer c.endRotation()
	if c.sessionEngineEnabled() {
		return c.rotateExclusiveSession(true)
	}
	c.mu.Lock()
	oldPath := c.sessionPath
	c.mu.Unlock()
	preMarkedCleanup := c.hasUnfinishedSessionJobs(oldPath)
	if preMarkedCleanup {
		if err := agent.MarkCleanupPending(oldPath, "clear"); err != nil {
			return err
		}
	}
	// Retire the old recovery state before deleting its artifacts. Async gate
	// snapshots are path-bound, so wait for every already-scheduled old-path
	// write; otherwise one can recreate the sidecar after removeSessionArtifacts.
	c.loadRecoveryState("")
	c.flushRecoveryPersistence(oldPath)
	// Let session_policy rule before destroying artifacts. A required failure
	// keeps the old session intact; the fresh path arrives with session.start.
	if err := c.extensionSessionPhase(context.Background(), extension.PointSessionRotate, dispatch.PhaseRotate, oldPath); err != nil {
		return err
	}
	// Hold snapshotMu from artifact removal through the swap: a save slipping
	// in between would resurrect the just-removed transcript, and one that
	// overlapped the swap could pair the old path with the fresh session.
	c.snapshotMu.Lock()
	c.detachInboxForDiscard(oldPath)
	destroy := c.BeginDestroySession(oldPath)
	if !destroy.Async {
		if err := removeSessionArtifacts(oldPath); err != nil {
			destroy.Finish()
			c.snapshotMu.Unlock()
			c.rebindInbox()
			return err
		}
		destroy.Finish()
	}
	freshPath := oldPath
	if c.sessionDir != "" {
		freshPath = agent.NewSessionPath(c.sessionDir, c.label)
	}
	freshSession := agent.NewSession(c.basePrompt())
	commitTransition, err := c.prepareSessionTransition(freshPath, "clear", freshSession)
	if err != nil {
		if destroy.Async {
			destroy.Finish()
		}
		c.snapshotMu.Unlock()
		c.rebindInbox()
		return fmt.Errorf("bind cleared session: %w", err)
	}
	c.hooks.SessionEnd(context.Background(), "clear")
	c.extensionSessionEvent(extension.PointSessionEnd, dispatch.PhaseEnd, oldPath)
	commitTransition.publish()
	c.bindExecutorProjection(c.SessionPath(), false)
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.ResetPlannerSession()
	c.rebindCheckpoints(freshPath)
	seedErr := c.seedSessionEventsFromExecutor("session-clear")
	c.resetRecoveryForNewSession(freshPath)
	c.rotateSessionTemp()
	c.snapshotMu.Unlock()
	c.rebindInbox()
	// Same contract as NewSession: the fresh session starts with no active goal.
	c.ClearGoal()
	c.mu.Lock()
	c.startedOnce = true
	c.mu.Unlock()
	c.hooks.SetSessionID(c.parentSessionID())
	c.enqueueHookContexts(c.hooks.SessionStart(context.Background(), "clear"))
	c.extensionSessionEvent(extension.PointSessionStart, dispatch.PhaseStart, c.SessionPath())
	c.clearSessionWriteAccess()
	if destroy.Async {
		go func() {
			result := destroy.Wait()
			if result.HasTimedOut() && destroy.WaitAll != nil {
				if err := agent.MarkCleanupPending(oldPath, "clear"); err != nil {
					c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "mark cleanup pending failed: " + err.Error()})
				}
				destroy.WaitAll()
			}
			if err := removeSessionArtifacts(oldPath); err != nil {
				c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "clear session cleanup failed: " + err.Error()})
			}
			destroy.Finish()
		}()
	}
	if seedErr != nil {
		return fmt.Errorf("seed cleared session events: %w", seedErr)
	}
	return nil
}

// detachInboxForDiscard releases the old inbox transaction lock before a
// destructive session clear. Windows does not permit removing an open lock
// file, while Unix silently unlinks it, so the ordering must be explicit.
func (c *Controller) detachInboxForDiscard(sessionPath string) {
	c.inbox.scanMu.Lock()
	defer c.inbox.scanMu.Unlock()
	c.inbox.mu.Lock()
	defer c.inbox.mu.Unlock()
	if c.inbox.store == nil || c.inbox.store.SessionPath() != sessionPath {
		return
	}
	c.inbox.store.Close()
	c.inbox.store = nil
	c.inbox.clearActive()
}
