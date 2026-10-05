package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/platform/browser"
	"reasonix/internal/runtime/guardian"
	"reasonix/internal/state/sessiontemp"
)

// SetOnSessionRecovered installs the ownership handoff invoked before the
// controller commits to an automatically created recovery branch. Frontends
// that acquire their session owner after controller construction (for example
// reasonix serve) use this before publishing the controller.
func (c *Controller) SetOnSessionRecovered(fn func(SessionRecoveryInfo) error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onSessionRecovered = fn
}

func (c *Controller) sessionRecoveredHandler() func(SessionRecoveryInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.onSessionRecovered
}

// maybeSessionStart fires the SessionStart hook exactly once per session, lazily
// on the first turn — by then the sink/notify is wired, and a resumed session
// fires it too (its first post-resume turn).
func (c *Controller) maybeSessionStart(ctx context.Context) {
	c.hooks.SetSessionID(c.parentSessionID())
	c.mu.Lock()
	if c.startedOnce {
		c.mu.Unlock()
		return
	}
	c.startedOnce = true
	c.mu.Unlock()
	c.enqueueHookContexts(c.hooks.SessionStart(ctx))
	c.extensionSessionEvent(extension.PointSessionStart, dispatch.PhaseStart, c.SessionPath())
}

// NewSession snapshots the current conversation, rotates to a fresh file, and
// resets the executor to a clean session carrying the same system prompt. It
// ends the old session and starts the new one for lifecycle hooks.
func (c *Controller) NewSession() error {
	if c.executor == nil {
		return nil
	}
	// Claim the rotation gate for the whole snapshot-then-swap sequence. A bare
	// `if c.gate.running` check released before Snapshot() left a window where a turn
	// could start during the snapshot and then have its live session replaced by
	// the SetSession below. Submit ("/new") calls this asynchronously, so the
	// gate is load-bearing, not defensive.
	if err := c.beginRotation(); err != nil {
		return err
	}
	defer c.endRotation()
	// Retire asynchronous recovery writes before Snapshot publishes the final
	// old-session checkpoint. Otherwise an earlier write can outlive the path
	// rotation (or process teardown) and race cleanup of the old session.
	oldPath := c.SessionPath()
	c.flushRecoveryPersistence(oldPath)
	if err := c.Snapshot(); err != nil {
		return err
	}
	// session.rotate: the session_policy owner rules on the rotation before
	// anything is torn down, so its failure (required-class) aborts the
	// rotation cleanly. SessionPath is the file being rotated away from; the
	// fresh path arrives with the session.start event below.
	if err := c.extensionSessionPhase(context.Background(), extension.PointSessionRotate, dispatch.PhaseRotate, oldPath); err != nil {
		return err
	}
	c.hooks.SessionEnd(context.Background(), "clear")
	c.extensionSessionEvent(extension.PointSessionEnd, dispatch.PhaseEnd, oldPath)
	// Hold snapshotMu across the swap so an in-flight save cannot pair the old
	// path with the fresh session (or the fresh path with the old session).
	c.snapshotMu.Lock()
	if c.sessionDir != "" {
		c.mu.Lock()
		c.sessionPath = sessionstore.NewSessionPath(c.sessionDir, c.label)
		c.guardianPath = guardian.PathFor(c.sessionPath)
		c.mu.Unlock()
	}
	c.setActiveJobSession(c.SessionPath())
	c.executor.SetSession(sessionstore.NewSession(c.systemPrompt))
	c.bindExecutorProjection(c.SessionPath(), false)
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.ResetPlannerSession()
	freshPath := c.SessionPath()
	c.rebindCheckpoints(freshPath)
	c.resetRecoveryForNewSession(freshPath)
	c.rotateSessionTemp()
	c.snapshotMu.Unlock()
	// Old session keeps its inbox (paused); the fresh session starts empty.
	c.pauseInboxOnRotate()
	c.rebindInbox()
	// A new session starts with no active goal: without this, a running goal's
	// text kept injecting into the fresh session's first turns. The old
	// session's goal-state sidecar was persisted before the rotation and stays
	// intact, so resuming it restores its goal; the cleared state below lands
	// on the NEW path (rebindCheckpoints just moved it).
	c.ClearGoal()
	c.mu.Lock()
	c.startedOnce = true // NewSession fires SessionStart itself; don't re-fire on the next turn
	c.mu.Unlock()
	c.hooks.SetSessionID(c.parentSessionID())
	c.enqueueHookContexts(c.hooks.SessionStart(context.Background(), "clear"))
	c.extensionSessionEvent(extension.PointSessionStart, dispatch.PhaseStart, c.SessionPath())
	return nil
}

// ClearSession discards the current conversation without preserving it in
// resume/history, then rotates to a clean session carrying the same system prompt.
func (c *Controller) ClearSession() error {
	if c.executor == nil {
		return nil
	}
	// Same rotation gate as NewSession: hold it across the whole
	// destroy-then-swap so a turn cannot start during the sequence and have its
	// live session replaced.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return fmt.Errorf("cannot clear while a turn is running")
		}
		return err
	}
	defer c.endRotation()
	c.mu.Lock()
	oldPath := c.sessionPath
	c.mu.Unlock()
	preMarkedCleanup := c.hasUnfinishedSessionJobs(oldPath)
	if preMarkedCleanup {
		if err := sessionstore.MarkCleanupPending(oldPath, "clear"); err != nil {
			return err
		}
	}
	// Retire the old recovery state before deleting its artifacts. Async gate
	// snapshots are path-bound, so wait for every already-scheduled old-path
	// write; otherwise one can recreate the sidecar after removeSessionArtifacts.
	c.loadRecoveryState("")
	c.flushRecoveryPersistence(oldPath)
	// session.rotate: the session_policy owner rules on the rotation before any
	// artifact is destroyed, so its failure (required-class) aborts the clear
	// with the old session fully intact. SessionPath is the file being rotated
	// away from; the fresh path arrives with the session.start event below.
	if err := c.extensionSessionPhase(context.Background(), extension.PointSessionRotate, dispatch.PhaseRotate, oldPath); err != nil {
		return err
	}
	// Hold snapshotMu from artifact removal through the swap: a save slipping
	// in between would resurrect the just-removed transcript, and one that
	// overlapped the swap could pair the old path with the fresh session.
	c.snapshotMu.Lock()
	destroy := c.BeginDestroySession(oldPath)
	if !destroy.Async {
		if err := removeSessionArtifacts(oldPath); err != nil {
			destroy.Finish()
			c.snapshotMu.Unlock()
			return err
		}
		destroy.Finish()
	}
	c.hooks.SessionEnd(context.Background(), "clear")
	c.extensionSessionEvent(extension.PointSessionEnd, dispatch.PhaseEnd, oldPath)
	if c.sessionDir != "" {
		c.mu.Lock()
		c.sessionPath = sessionstore.NewSessionPath(c.sessionDir, c.label)
		c.guardianPath = guardian.PathFor(c.sessionPath)
		c.mu.Unlock()
	}
	c.setActiveJobSession(c.SessionPath())
	c.executor.SetSession(sessionstore.NewSession(c.systemPrompt))
	c.bindExecutorProjection(c.SessionPath(), false)
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.ResetPlannerSession()
	freshPath := c.SessionPath()
	c.rebindCheckpoints(freshPath)
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
	if destroy.Async {
		go func() {
			result := destroy.Wait()
			if result.HasTimedOut() && destroy.WaitAll != nil {
				if err := sessionstore.MarkCleanupPending(oldPath, "clear"); err != nil {
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
	return nil
}

func (c *Controller) hasUnfinishedSessionJobs(sessionPath string) bool {
	if c.jobs == nil {
		return false
	}
	return c.jobs.HasUnfinishedForSession(sessionstore.BranchID(sessionPath))
}

// Branch copies the current conversation into a child branch and switches to it.
// It branches at the current tip and does not require a checkpoint.
func (c *Controller) Branch(name string) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("branch unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("branch needs session persistence, which is disabled"))
	}
	// Hold the rotation gate across the Snapshot and the switch below so a turn
	// cannot start mid-branch and then have its session replaced.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return "", c.rewindFail(fmt.Errorf("cannot branch while a turn is running"))
		}
		return "", c.rewindFail(err)
	}
	defer c.endRotation()
	if !c.executor.Session().HasContent() {
		return "", c.rewindFail(fmt.Errorf("nothing to branch yet"))
	}
	if err := c.Snapshot(); err != nil {
		return "", c.rewindFail(err)
	}
	parentPath := c.SessionPath()
	parentID := sessionstore.BranchID(parentPath)
	src := c.executor.Session().Snapshot()
	branched := append([]provider.Message(nil), src...)
	sess := sessionstore.NewSession("")
	sess.Messages = branched

	newPath := sessionstore.NewSessionPath(c.sessionDir, c.label)
	if err := sess.SaveIfAbsent(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	branchPreview, branchTurns := sessionstore.SessionPreviewFromMessages(branched)
	if err := sessionstore.SaveBranchMeta(newPath, sessionstore.BranchMeta{
		Name:          strings.TrimSpace(name),
		ParentID:      parentID,
		Preview:       branchPreview,
		Turns:         branchTurns,
		SchemaVersion: sessionstore.BranchMetaCountsVersion,
	}); err != nil {
		return "", c.rewindFail(err)
	}
	// See snapshotMu: the swap must not interleave with an in-flight save.
	c.snapshotMu.Lock()
	c.executor.SetSession(sess)
	c.mu.Lock()
	c.sessionPath = newPath
	c.guardianPath = guardian.PathFor(newPath)
	c.mu.Unlock()
	c.bindExecutorProjection(newPath, false)
	c.ResetPlannerSession()
	c.setActiveJobSession(newPath)
	c.rebindCheckpoints(newPath)
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.carryRecoveryState(newPath)
	c.rotateSessionTemp()
	c.snapshotMu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("created branch %s", sessionstore.BranchID(newPath))})
	return newPath, nil
}

// Branches lists saved conversation branches in this controller's session dir.
func (c *Controller) Branches() ([]sessionstore.BranchInfo, error) {
	if c.sessionDir == "" {
		return nil, fmt.Errorf("session persistence is disabled")
	}
	if err := c.Snapshot(); err != nil {
		return nil, err
	}
	return sessionstore.ListBranches(c.sessionDir)
}

func (c *Controller) SwitchBranch(ref string) (sessionstore.BranchInfo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return sessionstore.BranchInfo{}, c.rewindFail(fmt.Errorf("usage: /switch <branch id|name>"))
	}
	// Hold the rotation gate across the branch listing/load and the switch so a
	// turn cannot start between the check and the SetSession below.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return sessionstore.BranchInfo{}, c.rewindFail(fmt.Errorf("cannot switch branches while a turn is running"))
		}
		return sessionstore.BranchInfo{}, c.rewindFail(err)
	}
	defer c.endRotation()
	branches, err := c.Branches()
	if err != nil {
		return sessionstore.BranchInfo{}, c.rewindFail(err)
	}
	match, err := resolveBranch(branches, ref)
	if err != nil {
		return sessionstore.BranchInfo{}, c.rewindFail(err)
	}
	if !sessionstore.IsVisibleSession(match.Path) {
		return sessionstore.BranchInfo{}, c.rewindFail(fmt.Errorf("branch %q not found", ref))
	}
	loaded, err := sessionstore.LoadSession(match.Path)
	if err != nil {
		return sessionstore.BranchInfo{}, c.rewindFail(err)
	}
	// See snapshotMu: the swap must not interleave with an in-flight save.
	c.snapshotMu.Lock()
	if c.executor != nil {
		c.executor.SetSession(loaded)
	}
	c.mu.Lock()
	c.sessionPath = match.Path
	c.guardianPath = guardian.PathFor(match.Path)
	c.mu.Unlock()
	c.bindExecutorProjection(match.Path, true)
	c.ResetPlannerSession()
	c.setActiveJobSession(match.Path)
	c.rebindCheckpoints(match.Path)
	c.restoreTerminalGoalTodos(match.Path)
	c.loadGuardianSession()
	c.loadRecoveryState(match.Path)
	c.rotateSessionTemp()
	c.snapshotMu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("switched to branch %s", branchDisplayName(match))})
	return match, nil
}

// resume is Resume with the cold-cache notice made optional: a rebuild
// migration re-binds a session the user never left, so it records the cache
// state without announcing an idle gap nobody just sat through.
func (c *Controller) resume(s *sessionstore.Session, path string, announceColdResume bool) {
	// See snapshotMu: the swap must not interleave with an in-flight save.
	// recoverInterruptedTurn and maybeColdResumePrune snapshot on their own,
	// so they stay outside the locked section (snapshotMu is not reentrant).
	prevPath := c.SessionPath()
	c.snapshotMu.Lock()
	if c.executor != nil {
		c.executor.SetSession(s)
	}
	c.mu.Lock()
	c.sessionPath = path
	c.guardianPath = guardian.PathFor(path)
	c.mu.Unlock()
	c.bindExecutorProjection(path, true)
	c.ResetPlannerSession()
	c.setActiveJobSession(path)
	c.rebindCheckpoints(path)
	migPath, migData, migrated, _ := c.goals.restoreFromState(path)
	if migrated {
		c.persistGoalState(migPath, migData, true)
	}
	if c.executor != nil {
		c.executor.RestoreDeliveryCheckpoint(c.goals.deliveryState())
		c.observeVerificationContractDrift()
	}
	c.restoreTerminalGoalTodos(path)
	c.loadGuardianSession()
	c.loadRecoveryState(path)
	if shouldRotateSessionTempOnResume(prevPath, path) {
		c.rotateSessionTemp()
	}
	c.snapshotMu.Unlock()
	c.rebindInbox()
	c.recoverCheckpointTransactions()
	c.recoverInterruptedTurn(path)
	c.maybeColdResumePrune(path, announceColdResume)
	// session.load: Resume has no failure channel, so the session_policy
	// strategy is advisory this stage — a required-class failure is surfaced
	// as a warning and the load stands. The event still carries the final
	// (possibly owner-adjusted) phase payload.
	if err := c.extensionSessionPhase(context.Background(), extension.PointSessionLoad, dispatch.PhaseLoad, path); err != nil {
		c.extensionWarn("session policy failed at session.load", err)
	}
}

// SetSessionPath rebinds auto-save without changing the current session
// preference. Callers creating a genuinely fresh conversation should use
// SetFreshSessionPath; callers resuming history should use Resume.
func (c *Controller) SetSessionPath(p string) {
	c.setSessionPath(p, false)
}

// SetFreshSessionPath binds a path that is known to belong to a newly-created
// session and samples the configured new-session recovery default.
func (c *Controller) SetFreshSessionPath(p string) {
	c.setSessionPath(p, true)
}

func (c *Controller) setSessionPath(p string, fresh bool) {
	// See snapshotMu: the swap must not interleave with an in-flight save.
	c.snapshotMu.Lock()
	c.mu.Lock()
	c.sessionPath = p
	c.guardianPath = guardian.PathFor(p)
	c.mu.Unlock()
	// Fresh paths clear projection; rebinds keep/load the target sidecar.
	c.bindExecutorProjection(p, !fresh)
	c.setActiveJobSession(p)
	c.rebindCheckpoints(p)
	if fresh {
		c.resetRecoveryForNewSession(p)
		// A newly-created conversation must not share the previous logical
		// session's temporary files (e.g. after EnsureSessionPath on a
		// controller that already ran commands).
		c.rotateSessionTemp()
	} else {
		c.loadRecoveryState(p)
	}
	c.snapshotMu.Unlock()
	// After the unlock: taking the lease touches the filesystem, and doing that
	// under snapshotMu puts a file lock behind the mutex a live save holds.
	if follow := c.sessionPathChangedHandler(); follow != nil {
		follow(p)
	}
	c.rebindInbox()
	if !fresh {
		c.recoverCheckpointTransactions()
	}
}

func (c *Controller) setActiveJobSession(sessionPath string) {
	if c.jobs != nil {
		c.jobs.SetActiveSessionPath(sessionstore.BranchID(sessionPath), sessionPath)
	}
}

// SessionPath reports the file the current conversation auto-saves to ("" when
// persistence is disabled), so a history view can mark the active session.
func (c *Controller) SessionPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionPath
}

func (c *Controller) parentSessionID() string {
	return sessionstore.BranchID(c.SessionPath())
}

// beginRotation claims the session-rotation gate. It fails if a turn is running
// or another rotation is already in progress, so the caller holds exclusive
// rights to swap the executor session from the check here through endRotation.
// This closes the TOCTOU window that a bare `if c.gate.running` check left open:
// between that check and the actual SetSession, a turn could start and then be
// yanked out from under the run loop.
func (c *Controller) beginRotation() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gate.active() {
		return errTurnRunningRotation
	}
	if c.gate.rotating {
		return errRotationInProgress
	}
	c.gate.rotating = true
	return nil
}

func shouldRotateSessionTempOnResume(prevPath, nextPath string) bool {
	prevPath = strings.TrimSpace(prevPath)
	nextPath = strings.TrimSpace(nextPath)
	if prevPath == "" || nextPath == "" {
		return false
	}
	return filepath.Clean(prevPath) != filepath.Clean(nextPath)
}

// ReleaseResources stops plugin subprocesses and releases resources without
// firing SessionEnd. Use it only when replacing the controller for the same
// logical session.
func (c *Controller) ReleaseResources() {
	c.close(false, closeJobsWithGrace)
}

// Close stops plugin subprocesses and releases resources. A session that ever
// started fires SessionEnd so a teardown hook runs.
func (c *Controller) Close() {
	c.close(true, closeJobsWithGrace)
}

// CloseAfterDestroy releases controller resources after the caller has already
// begun session-specific job teardown. It avoids a second synchronous job grace
// wait while still cancelling the manager root and reaping temporary artifacts
// once every job goroutine finally exits.
func (c *Controller) CloseAfterDestroy() {
	c.close(true, closeJobsAsync)
}

func (c *Controller) close(fireSessionEnd bool, jobsMode closeJobsMode) {
	// Desktop tab lifecycles can race a rebind/model-switch/close on the same
	// controller; make teardown idempotent so a duplicate Close cannot re-fire
	// SessionEnd hooks or re-run cleanup. The first caller's jobsMode wins.
	c.closeOnce.Do(func() {
		c.mu.Lock()
		started := c.startedOnce
		cancel := c.gate.cancel
		// Seal turn admission and drop anything already parked: a parked turn
		// must not start against a controller that is being torn down, and
		// without the closed flag a submit landing after this critical
		// section (while a running turn's TurnDone delivery is still in
		// flight) would park again and start after teardown.
		c.gate.closed = true
		c.parkedTurns = nil
		// A finishing-only controller no longer needs the delivery gate because
		// closed seals every admission path. Keep running truthful until the
		// foreground goroutine actually exits; clearing it here would report idle
		// while tools and prompt waiters were still live.
		c.gate.finishing = false
		if cancel != nil {
			c.gate.canceling = true
		}
		c.mu.Unlock()
		if cancel != nil {
			// clearAll deliberately does not signal waiters. Pair it with the
			// foreground cancellation so approval/ask waits always unblock.
			c.approval.clearAll()
			cancel()
		}
		if fireSessionEnd && started {
			c.hooks.SessionEnd(context.Background(), "other")
			c.extensionSessionEvent(extension.PointSessionEnd, dispatch.PhaseEnd, c.SessionPath())
		}
		if c.jobs != nil {
			switch jobsMode {
			case closeJobsAsync:
				c.jobs.CloseAsync()
			default:
				c.jobs.Close() // cancel any still-running background jobs
			}
		}
		if c.cleanup != nil {
			c.cleanup()
		}
		// Drop the Controller owner reference last so background job leases
		// that outlive close still pin retired generations until they exit.
		if c.sessionTemp != nil {
			c.sessionTemp.Release()
		}
		if c.browser != nil {
			c.browser.Release()
		}
	})
}

// adoptSessionResources takes an owner reference on what outlives one runtime
// generation — the private temporary directory and the browser — reusing what a
// hot rebuild hands over, so ReleaseResources/Close never race a replacement.
func (c *Controller) adoptSessionResources(opts Options) {
	c.workspaceLease.SetSessionID(func() string { return sessionstore.BranchID(c.SessionPath()) })
	c.sessionTemp = opts.SessionTemp
	if c.sessionTemp == nil {
		c.sessionTemp = sessiontemp.New()
	}
	c.sessionTemp.Retain()
	if opts.BrowserSession != nil {
		c.browser = opts.BrowserSession
		c.browser.Retain()
		c.browser.OnTabsChanged(func() { c.sink.Emit(event.Event{Kind: event.BrowserTabsChanged}) })
	}
}

// BrowserTabs lists the agent's open tabs, none when it has no browser.
func (c *Controller) BrowserTabs() []browser.TabInfo {
	if c == nil || c.browser == nil {
		return []browser.TabInfo{}
	}
	return c.browser.Tabs()
}

// BrowserOpen opens a page in the session's one browser, whose tabs both the
// person and the agent can read and drive. The active tab is where the agent's
// calls without a tab land, so a new tab the person opens sits beside it and
// leaves the agent's active. A private surface for the window's own tabs would
// be a page neither could hand the other, and one that refuses framing.
func (c *Controller) BrowserOpen(ctx context.Context, rawURL, tabID string, newTab bool) (browser.TabInfo, error) {
	if c == nil || c.browser == nil {
		return browser.TabInfo{}, errors.New("this session has no browser")
	}
	if !newTab {
		return c.browser.Visit(ctx, rawURL, tabID, newTab)
	}
	return openBeside(ctx, c.browser, rawURL)
}

// A person's open answers once the page is drawn; only the agent's waits for
// every subresource, since it reads what loaded.
type tabOpener interface {
	Tabs() []browser.TabInfo
	Visit(ctx context.Context, rawURL, tabID string, newTab bool) (browser.TabInfo, error)
	Switch(tabID string) (browser.TabInfo, error)
}

// openBeside opens rawURL in a new tab and hands the active one back to the
// tab that held it, when one did.
func openBeside(ctx context.Context, b tabOpener, rawURL string) (browser.TabInfo, error) {
	held := ""
	for _, t := range b.Tabs() {
		if t.Active {
			held = t.ID
		}
	}
	info, err := b.Visit(ctx, rawURL, "", true)
	if held == "" || info.ID == "" || info.ID == held {
		return info, err
	}
	if _, serr := b.Switch(held); serr == nil {
		info.Active = false
	}
	return info, err
}

// BrowserSession is the agent's browser, which a rebuild hands to the
// controller that replaces this one.
func (c *Controller) BrowserSession() *browser.Session {
	if c == nil {
		return nil
	}
	return c.browser
}

// SessionTemp returns the logical-session private temporary directory manager.
// Hot rebuilds pass this to the replacement Controller so the directory survives
// model/settings swaps. Nil only when the Controller was constructed without one
// (should not happen after New).
func (c *Controller) SessionTemp() *sessiontemp.Manager {
	if c == nil {
		return nil
	}
	return c.sessionTemp
}

// rotateSessionTemp advances the private temporary generation so a new logical
// session cannot see the previous session's temporary files. In-flight command
// leases keep the old generation alive until they release.
func (c *Controller) rotateSessionTemp() {
	if c == nil || c.sessionTemp == nil {
		return
	}
	c.sessionTemp.Rotate()
}
