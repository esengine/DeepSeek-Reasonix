package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/guardian"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/store"
	"reasonix/internal/tools/jobs"
)

// Snapshot writes the executor's conversation to the active session file. No-op
// when the executor is absent or the session has never been used (no user
// interaction). Returns errNoSessionPath when there IS content but no resolved
// path, so a misconfigured deployment surfaces instead of dropping data.
// Called after every turn so a crash loses at most one in-flight prompt.
func (c *Controller) Snapshot() error {
	return c.snapshot(false, false, false)
}

// SnapshotForShutdown performs the final session snapshot and, only when the
// compatibility file lock remains held for the full bounded wait, persists the
// in-memory transcript to a distinct recovery branch before teardown proceeds.
// Other snapshot errors retain their normal behavior and remain visible to the
// caller.
func (c *Controller) SnapshotForShutdown() error {
	return c.snapshot(false, false, true)
}

// SnapshotActivity writes the active conversation and marks the session as
// recently active. Use it only after a real user/model turn changes the
// transcript; switch/close snapshots should call Snapshot so they do not reorder
// recent-session pickers.
func (c *Controller) SnapshotActivity() error {
	return c.snapshot(true, false, false)
}

// SnapshotRewrite persists an intentional history rewrite, such as rewind or
// manual compaction. Ordinary autosave paths should use Snapshot so stale
// controllers cannot overwrite a newer transcript.
func (c *Controller) SnapshotRewrite() error {
	return c.snapshot(false, true, false)
}

// midTurnSnapshotInterval is atomic (nanoseconds) so a test shrinking it
// cannot race a previous test's still-parking autosave goroutine.
var midTurnSnapshotInterval atomic.Int64

func init() { midTurnSnapshotInterval.Store(int64(30 * time.Second)) }

// SaveAdmittedMessage implements agent.AdmissionSaver. The next save is the
// autosave tick or turn end, so without this one a kill in between leaves the
// prompt in memory only. A failed save is logged and does not refuse the turn.
func (c *Controller) SaveAdmittedMessage(context.Context) {
	if c.SessionPath() == "" {
		return
	}
	if err := c.snapshot(false, false, false); err != nil {
		slog.Warn("controller: save admitted user message", "err", err)
	}
}

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
			if err := c.snapshot(false, false, false); err != nil {
				slog.Warn("controller: mid-turn snapshot", "err", err)
			}
		}
	}
}

// snapshotWithDurability reports whether the canonical transcript reached disk
// even when a later sidecar update failed. Callers that guard a crash marker
// need this distinction: a metadata error must not make a complete transcript
// look like an in-memory-only turn.
func (c *Controller) snapshotWithDurability(markActivity, forceRewrite, shutdownRecovery bool) (bool, error) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	c.mu.Lock()
	path := c.sessionPath
	modelRef := c.modelRef
	c.mu.Unlock()
	c.writeSessionUsageRecord(path)
	if c.executor == nil {
		return false, nil
	}
	s := c.executor.Session()
	if !s.HasContent() {
		// Nothing to persist yet (e.g. a fresh session with only a system
		// prompt) — staying quiet here is correct, not a data-loss path.
		return false, nil
	}
	if !s.HasSystemMessage() {
		// The session has user/assistant/tool messages but no leading system
		// prompt.  Persisting it would create a session file that, when
		// reloaded, has no agent-identity contract — the model falls back to
		// its training-data defaults, giving wrong answers to identity
		// queries ("who are you?").  Log the anomaly so the root cause
		// (typically an empty sysPrompt reaching NewSession) can be
		// diagnosed, then refuse to write a corrupted transcript.
		slog.Warn("controller: refusing to snapshot session with content but no system message",
			"label", c.Label(), "session_dir", c.SessionDir(), "message_count", len(s.Snapshot()))
		return false, nil
	}
	if path == "" {
		// There IS content but nowhere to write it: this silently dropped whole
		// bot conversations (#4414). Surface it loudly instead of returning nil
		// so the missing session path can be diagnosed and fixed at the source.
		slog.Warn("controller: session has content but no session path; conversation will not be persisted",
			"label", c.Label(), "session_dir", c.SessionDir())
		return false, errNoSessionPath
	}
	// session.save: the session_policy owner rules on the impending save; a
	// failure (required-class) vetoes the write. The event goes out after a
	// successful save carrying the final payload. The early no-content and
	// no-path returns above are not saves and stay unobserved. Conflict
	// recovery below may rewrite the path; the phase payload reports the path
	// the save targeted.
	savePayload, strategyErr := c.extensionSessionStrategy(context.Background(), extension.PointSessionSave, dispatch.PhaseSave, path)
	if strategyErr != nil {
		return false, strategyErr
	}
	err, forceRewrite := persistSessionSnapshot(s, path, forceRewrite)
	var stop bool
	if path, stop, err = c.settleForeignSave(path, err); stop {
		return false, err
	}
	if authoritySaveError(err) {
		// Missing/stale authority must not enter diverged/recovery. Frontends
		// rebind the lease or surface the typed error.
		return false, err
	}
	if err != nil {
		if shutdownRecovery && errors.Is(err, sessionstore.ErrSessionFileLockHeld) {
			recoveredPath, recoverErr := c.recoverShutdownSnapshot(path, err)
			if recoverErr != nil {
				return false, recoverErr
			}
			path = recoveredPath
			s = c.executor.Session()
			err = nil
		}
	}
	if err != nil {
		if !errors.Is(err, sessionstore.ErrSessionSnapshotConflict) {
			return false, err
		}
		recoveredPath, outcome, recoverErr := c.recoverSnapshotConflict(path, err, forceRewrite)
		if recoverErr != nil {
			if shutdownRecovery && errors.Is(recoverErr, sessionstore.ErrSessionFileLockHeld) {
				recoveredPath, recoverErr = c.recoverShutdownSnapshot(path, recoverErr)
				if recoverErr != nil {
					return false, recoverErr
				}
				path = recoveredPath
				s = c.executor.Session()
			} else {
				return false, recoverErr
			}
		} else {
			if outcome == conflictDropped {
				return false, nil
			}
			// Whatever recovery did — adopted the disk transcript, isolated the
			// depth-capped copy, or forked — the rewrite baseline lives on
			// the session object and was advanced by the save that succeeded, so
			// there is nothing to re-anchor here.
			path = recoveredPath
			s = c.executor.Session()
		}
	}
	// Persist guardian session so the prefix cache stays warm after restart.
	if c.guardianSess != nil {
		gp := c.guardianPath
		if gp != "" {
			if gerr := c.guardianSess.Save(gp); gerr != nil {
				slog.Warn("controller: guardian snapshot", "err", gerr)
			}
		}
	}
	transcriptDurable := true
	// Persist recovery gate state so unresolved checkpoints survive restart.
	c.saveRecoveryState(path)
	// Record the listing-only sidecar fields (model, preview, user-turn count)
	// straight from the in-memory conversation, so the sidebar and resume picker
	// never have to decode the whole .jsonl just to show them. markActivity bumps
	// UpdatedAt; false preserves it.
	preview, turns := sessionstore.SessionPreviewFromMessages(s.Snapshot())
	if err := sessionstore.UpdateSessionMeta(path, modelRef, preview, turns, markActivity); err != nil {
		return transcriptDurable, err
	}
	c.extensionSessionPayloadEvent(extension.PointSessionSave, savePayload)
	return transcriptDurable, nil
}

func (c *Controller) emitRecoveryDepthCapNotice(path string) {
	key := filepath.Clean(strings.TrimSpace(path))
	c.mu.Lock()
	if c.recoveryDepthCapNotices == nil {
		c.recoveryDepthCapNotices = make(map[string]bool)
	}
	if c.recoveryDepthCapNotices[key] {
		c.mu.Unlock()
		return
	}
	c.recoveryDepthCapNotices[key] = true
	c.mu.Unlock()
	c.sink.Emit(sessionRecoveryNotice(event.NoticeCodeSessionRecoveryDepthCap, recoveryDepthCapNoticeText))
}

func (c *Controller) recoverSnapshotConflict(path string, saveErr error, forceRewrite bool) (string, conflictOutcome, error) {
	if c.executor == nil || strings.TrimSpace(path) == "" {
		return "", conflictDropped, saveErr
	}
	mode := "snapshot"
	if forceRewrite {
		mode = "rewrite"
	}
	logAttrs := snapshotConflictLogAttrs(saveErr, path, mode)
	if kind, ok := sessionstore.SnapshotConflictKind(saveErr); ok && kind == sessionstore.SessionSnapshotConflictStalePrefix {
		if c.adoptDiskSession(path) {
			appendSnapshotConflictDiagnostic(path, mode, "adopted_newer_disk_transcript", saveErr, "", false)
			slog.Warn("controller: snapshot conflict; adopted newer disk transcript", logAttrs...)
			c.sink.Emit(sessionRecoveryNotice(event.NoticeCodeSessionRecoveryAdopted,
				"session changed on disk; adopted the newer transcript"))
			return path, conflictAdoptedDisk, nil
		}
	}
	reason := "snapshot conflict"
	if forceRewrite {
		reason = "rewrite conflict"
	}
	req := SessionRecoveryRequest{OriginalPath: path, Reason: reason, Mode: mode}
	meta := sessionstore.BranchMeta{}
	if c.sessionRecoveryMeta != nil {
		meta = c.sessionRecoveryMeta(req)
	}
	info, err := c.executor.Session().SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{
		OriginalPath: path,
		Reason:       reason,
		BranchMeta:   meta,
	})
	if err != nil {
		if errors.Is(err, sessionstore.ErrSessionRecoveryDepthExceeded) {
			// The canonical branch may have advanced since this runtime loaded it.
			// Never force-write the stale in-memory snapshot back onto that path just
			// to stop a recovery chain. Preserve it in a writer-specific isolated
			// branch instead; the depth cap limits lineage fan-out, not data safety.
			isolated, isolatedErr := c.executor.Session().SaveConflictRecoveryBranch(sessionstore.RecoveryBranchOptions{
				OriginalPath: path,
				Reason:       reason,
				BranchMeta:   meta,
			})
			if isolatedErr != nil {
				return "", conflictDropped, fmt.Errorf("recovery chain depth exceeded; isolated copy failed: %w", isolatedErr)
			}
			if err := c.commitRecoveredSession(path, reason, isolated); err != nil {
				return "", conflictDropped, err
			}
			appendSnapshotConflictDiagnostic(path, mode, "recovery_depth_cap_isolated", saveErr, isolated.Path, isolated.Existing)
			slog.Warn("controller: snapshot conflict; recovery depth cap reached, isolated stale transcript", append(logAttrs, "recovery", isolated.Path)...)
			c.emitRecoveryDepthCapNotice(path)
			return isolated.Path, conflictForkedBranch, nil
		}
		if errors.Is(err, sessionstore.ErrSessionRecoveryNotNeeded) {
			if c.adoptDiskSession(path) {
				appendSnapshotConflictDiagnostic(path, mode, "recovery_not_needed_adopted_disk_transcript", saveErr, "", false)
				slog.Warn("controller: snapshot conflict; recovery not needed, adopted disk transcript", logAttrs...)
				c.sink.Emit(sessionRecoveryNotice(event.NoticeCodeSessionRecoveryAdoptedCovered,
					"session changed on disk; adopted the newer transcript (local changes already covered)"))
				return path, conflictAdoptedDisk, nil
			}
			// Nothing was recovered AND the disk transcript could not be
			// adopted: the snapshot is silently dropped. Leave a trace so
			// "my last turns vanished" reports can be tied to this path.
			appendSnapshotConflictDiagnostic(path, mode, "recovery_not_needed_adopt_failed", saveErr, "", false)
			slog.Warn("controller: snapshot conflict; recovery not needed but disk transcript could not be adopted", logAttrs...)
			return "", conflictDropped, nil
		}
		return "", conflictDropped, fmt.Errorf("recover stale session snapshot: %w", err)
	}
	if err := c.commitRecoveredSession(path, reason, info); err != nil {
		return "", conflictDropped, err
	}
	appendSnapshotConflictDiagnostic(path, mode, "forked_recovery_branch", saveErr, info.Path, info.Existing)
	slog.Warn("controller: snapshot conflict; forked recovery branch",
		append(logAttrs, "recovery", info.Path, "existing", info.Existing)...)
	c.sink.Emit(sessionRecoveryNotice(event.NoticeCodeSessionRecoveryForked,
		"session changed on disk; unsaved local transcript was saved as a conflict copy"))
	return info.Path, conflictForkedBranch, nil
}

func (c *Controller) recoverShutdownSnapshot(path string, saveErr error) (string, error) {
	if c.executor == nil || strings.TrimSpace(path) == "" {
		return "", saveErr
	}
	const reason = "shutdown session file lock timeout"
	req := SessionRecoveryRequest{OriginalPath: path, Reason: reason, Mode: "shutdown"}
	meta := sessionstore.BranchMeta{}
	if c.sessionRecoveryMeta != nil {
		meta = c.sessionRecoveryMeta(req)
	}
	info, err := c.executor.Session().SaveShutdownRecoveryBranch(sessionstore.RecoveryBranchOptions{
		OriginalPath: path,
		Reason:       reason,
		BranchMeta:   meta,
	})
	if err != nil {
		return "", fmt.Errorf("save shutdown recovery branch: %w", err)
	}
	if err := c.commitRecoveredSession(path, reason, info); err != nil {
		return "", err
	}
	appendSnapshotConflictDiagnostic(path, "shutdown", "forked_file_lock_recovery", saveErr, info.Path, info.Existing)
	slog.Warn("controller: shutdown snapshot lock timed out; forked recovery branch",
		"path", path, "recovery", info.Path, "existing", info.Existing)
	c.sink.Emit(sessionRecoveryNotice(event.NoticeCodeSessionShutdownRecoveryForked,
		"session file stayed busy during shutdown; unsaved transcript was saved as a recovery copy"))
	return info.Path, nil
}

func (c *Controller) commitRecoveredSession(originalPath, reason string, info sessionstore.RecoveryBranchInfo) error {
	recoveryInfo := SessionRecoveryInfo{
		OriginalPath: originalPath,
		RecoveryPath: info.Path,
		Existing:     info.Existing,
		Reason:       reason,
		Meta:         info.Meta,
	}
	if onSessionRecovered := c.sessionRecoveredHandler(); onSessionRecovered != nil {
		if err := onSessionRecovered(recoveryInfo); err != nil {
			return fmt.Errorf("commit recovered session: %w", err)
		}
	}
	c.mu.Lock()
	c.sessionPath = info.Path
	c.guardianPath = guardian.PathFor(info.Path)
	c.mu.Unlock()
	// Recovery branch is a new lineage path. Load an inherited projection
	// sidecar when present so the model view stays compressed across the fork.
	c.bindExecutorProjection(info.Path, true)
	c.setActiveJobSession(info.Path)
	c.rebindCheckpoints(info.Path)
	c.transplantInFlightTurnMarker(originalPath, info.Path)
	return nil
}

func (c *Controller) adoptDiskSession(path string) bool {
	loaded, err := sessionstore.LoadSession(path)
	if err != nil || loaded == nil {
		return false
	}
	c.executor.SetSession(loaded)
	c.bindExecutorProjection(path, true)
	c.ResetPlannerSession()
	c.rebindCheckpoints(path)
	c.setActiveJobSession(path)
	return true
}

func (c *Controller) clearInFlightTurn(marker sessionstore.InFlightTurnMeta) {
	path := c.SessionPath()
	if path == "" || marker.ID == "" {
		return
	}
	if _, err := sessionstore.ClearSessionInFlightTurnIfMatch(path, marker); err != nil {
		slog.Warn("controller: clear in-flight turn", "err", err)
	}
}

func (c *Controller) recoverInterruptedTurn(path string) {
	if c.executor == nil || path == "" {
		return
	}
	meta, ok, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !ok || meta.InFlightTurn == nil {
		if err != nil {
			slog.Warn("controller: load in-flight turn marker", "err", err)
		}
		return
	}
	marker := meta.InFlightTurn
	if interruptedTurnContinuedOnRecoveryBranch(path, marker) {
		// The "interrupted" turn did not die with a runtime: a recovery branch
		// forked off this session after the marker was set, so the turn kept
		// running (and completing) there. Runtimes predating the marker
		// transplant in recoverSnapshotConflict left the marker behind on the
		// forked-from branch; stripping now would truncate a transcript the
		// completed turn already superseded. Clear the stale marker instead.
		if _, err := sessionstore.ClearSessionInFlightTurnIfMatch(path, *marker); err != nil {
			slog.Warn("controller: clear fork-orphaned in-flight turn", "err", err)
		}
		return
	}
	msgs := c.executor.Session().Snapshot()
	if marker.CommitDigest != "" {
		if digest, digestErr := c.executor.Session().ContentDigest(); digestErr != nil {
			slog.Warn("controller: digest resumed in-flight turn", "err", digestErr)
		} else if digest == marker.CommitDigest {
			// The exact transcript named before the final snapshot is present. The
			// process died after commit and before CAS cleanup; preserve everything.
			if _, err := sessionstore.ClearSessionInFlightTurnIfMatch(path, *marker); err != nil {
				slog.Warn("controller: clear committed in-flight turn marker", "err", err)
			}
			return
		}
	}
	start, found := resolveInterruptedTurnStart(msgs, marker.StartMessageIndex, marker.PreserveUser, marker.StartedAt, provider.Message{})
	if found && interruptedTurnCrossesLaterTurn(msgs, start) {
		slog.Warn("controller: preserving WAL transcript after stale in-flight marker",
			"path", path, "messages", len(msgs), "marker_index", marker.StartMessageIndex, "resolved_index", start,
			"marker_revision", marker.StartRevision, "current_revision", meta.Revision)
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
			Text: "Session recovery found completed turns after a stale interruption marker; the full WAL history was preserved."})
		if _, err := sessionstore.ClearSessionInFlightTurnIfMatch(path, *marker); err != nil {
			slog.Warn("controller: clear stale multi-turn in-flight marker", "err", err)
		}
		return
	}
	changed := found && len(msgs) > start
	if changed {
		if marker.PreserveUser {
			c.stripCancelledVisibleTurnMessagesAfterWithFallbackAt(start, provider.Message{}, marker.StartedAt)
		} else {
			c.stripTurnMessagesAfter(start)
		}
		if err := c.snapshot(false, true, false); err != nil {
			slog.Warn("controller: post-interrupted-turn snapshot", "err", err)
		}
	}
	if _, err := sessionstore.ClearSessionInFlightTurnIfMatch(path, *marker); err != nil {
		slog.Warn("controller: clear stale in-flight turn", "err", err)
	}
}

func (c *Controller) snapshotActivityIfChanged(startMessages int) (bool, error) {
	if c.messageCount() <= startMessages {
		return true, nil
	}
	return c.snapshotWithDurability(true, false, false)
}

// SessionHasUnsavedChanges tells desktop history whether it may safely refresh
// an idle view from the durable WAL. A failed or contended save can leave the
// controller with a newer in-memory transcript; replacing that view from disk
// would hide the user's latest turn until the next retry.
func (c *Controller) SessionHasUnsavedChanges() bool {
	if c == nil || c.executor == nil {
		return false
	}
	return c.executor.Session().HasUnsavedChanges(c.SessionPath())
}

// SessionPersistedState exposes the session's persistence baseline for the
// controller's current session path, so a paging frontend can validate a
// display-index sidecar against the live session.
func (c *Controller) SessionPersistedState() (sessionstore.PersistedState, bool) {
	if c.executor == nil {
		return sessionstore.PersistedState{}, false
	}
	return c.executor.Session().PersistedState(c.SessionPath())
}

func removeSessionArtifacts(path string) error {
	if path == "" {
		return nil
	}
	if err := jobs.RemoveArtifacts(path); err != nil {
		return err
	}
	// Artifacts include the event log — the authoritative transcript. Leaving it
	// behind would both leak the cleared conversation and let LoadSession
	// resurrect it on the recycled path. The guardian transcript saves through
	// the same session layer, so its own artifacts go the same way.
	if err := store.RemoveSessionArtifacts(path); err != nil {
		return err
	}
	if err := store.RemoveSessionArtifacts(guardian.PathFor(path), guardian.CursorPathFor(path)); err != nil {
		return err
	}
	if err := sessioninbox.RemoveDir(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if dir := ckptDir(path); dir != "" {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := delegation.DeleteSubagentsByParent(filepath.Dir(path), sessionstore.BranchID(path)); err != nil {
		return err
	}
	if err := sessionstore.ClearCleanupPending(path); err != nil {
		return err
	}
	return nil
}

// RemoveSessionArtifacts removes a transcript and every durable artifact owned
// by it. Remote runtimes use this when a newly-created fork fails before it can
// be registered as a live session.
func RemoveSessionArtifacts(path string) error {
	return removeSessionArtifacts(path)
}

// ReconcileCleanupPending retries physical cleanup for logically removed
// sessions that were left behind by a previous process.
func ReconcileCleanupPending(dir string) error {
	return sessionstore.ReconcileCleanupPending(dir, func(item sessionstore.CleanupPendingInfo) error {
		return removeSessionArtifacts(item.SessionPath)
	})
}

// snapshotConflictLogAttrs flattens a snapshot-conflict error into slog attrs.
// Field reports of #6069-class "session changed on disk" spam are only
// diagnosable when the logs say which trigger fired and what the revision
// ledger looked like, so every recoverSnapshotConflict outcome logs these.
func snapshotConflictLogAttrs(saveErr error, path, mode string) []any {
	attrs := []any{"path", path, "mode", mode}
	var conflict *sessionstore.SessionSnapshotConflictError
	if errors.As(saveErr, &conflict) && conflict != nil {
		attrs = append(attrs,
			"kind", string(conflict.Kind),
			"disk_messages", conflict.ExistingMessages,
			"snapshot_messages", conflict.SnapshotMessages,
			"base_revision", conflict.BaseRevision,
			"disk_revision", conflict.DiskRevision,
		)
	}
	return attrs
}

func sessionRecoveryNotice(code, text string) event.Event {
	return event.Event{
		Kind:     event.Notice,
		Level:    event.LevelWarn,
		Audience: event.NoticeAudienceOperator,
		Code:     code,
		Text:     text,
	}
}

func (c *Controller) messageCount() int {
	if c.executor == nil {
		return 0
	}
	return c.executor.Session().Len()
}

func (c *Controller) sessionMessageCount() int {
	if c.executor == nil {
		return 0
	}
	return c.executor.Session().Len()
}
