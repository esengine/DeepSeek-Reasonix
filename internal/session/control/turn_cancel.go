package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension"
	"reasonix/internal/runtime/agent"
)

// spawnGuardedTurn launches an admitted turn body plus its autosave companion.
// The caller must already have claimed admission (running=true) under c.mu.
func (c *Controller) spawnGuardedTurn(ctx context.Context, cancel context.CancelFunc, body func(ctx context.Context) error) {
	ctx, completion := withGuardedTurnCompletion(ctx)
	c.autosaveWG.Go(func() {
		c.autosaveWhileRunning(ctx)
	})
	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				c.finishGuardedTurn(fmt.Errorf("internal error: %v", r), completion)
			}
		}()
		err := body(ctx)
		c.finishGuardedTurn(explainError(err), completion)
	}()
}

// finishGuardedTurn keeps admission closed while TurnDone is delivered. The
// sink fan-out may detach per-turn transports; allowing a replacement turn in
// after running=false but before that fan-out completed let the old completion
// clear or inherit the replacement turn's transport.
//
// When the window closes, the oldest parked turn (if any) is started under the
// SAME critical section that clears finishing: opening the gate first and then
// re-admitting would let an unrelated submit slip in ahead and bounce the
// parked turn back to a drop. Remaining parked turns drain one per
// finishGuardedTurn, preserving FIFO order. Rotation cannot interleave here:
// beginRotation refuses while running or finishing, and the drain flips
// finishing directly into running.
func (c *Controller) finishGuardedTurn(err error, completion *guardedTurnCompletion) {
	c.memory.clearAutoRemember()
	c.mu.Lock()
	cancelRequested := c.gate.canceling
	c.gate.end()
	// A live controller keeps admission closed until TurnDone fan-out finishes.
	// Close has already sealed admission permanently, so a late completion must
	// not resurrect a finishing state after teardown.
	c.gate.finishing = !c.gate.closed
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.gate.finishing = false
		if c.gate.closed {
			c.mu.Unlock()
			return
		}
		if len(c.parkedTurns) == 0 {
			c.mu.Unlock()
			// No parked compatibility body: admit the next durable inbox item.
			c.maybeDispatchInbox()
			return
		}
		next := c.parkedTurns[0]
		c.parkedTurns = c.parkedTurns[1:]
		ctx, cancel := context.WithCancel(extension.ContextWithRuntimeOwner(context.Background(), c.runtimeOwner))
		c.gate.begin(cancel)
		c.mu.Unlock()
		c.spawnGuardedTurn(ctx, cancel, next)
	}()
	c.inbox.mu.Lock()
	// Prefer a single representative id for the wire event (first active).
	// Full multi-item ack happens in onInboxTurnDone via activeItemIDs.
	activeInboxID := ""
	for id := range c.inbox.activeItemIDs {
		activeInboxID = id
		break
	}
	c.inbox.mu.Unlock()
	done := event.Event{
		Kind:           event.TurnDone,
		Err:            err,
		Cancelled:      cancelRequested && (err == nil || errors.Is(err, context.Canceled)),
		Outcome:        turnOutcome(err),
		CheckpointTurn: c.validatedCheckpointTurn(completion),
		Receipt:        c.executor.CompletionReceipt(),
		ItemID:         activeInboxID,
	}
	var readinessErr *agent.FinalReadinessError
	if errors.As(err, &readinessErr) {
		done.Readiness = &event.FinalReadiness{Attempts: readinessErr.Attempts, Missing: append([]string(nil), readinessErr.Missing...)}
	}
	// Ack active durable items before exposing TurnDone. Frontends commonly
	// refresh the inbox from that event and must not observe already-consumed
	// steers in the completed turn. Dispatch still waits for finishing to clear.
	c.onInboxTurnDone()
	c.sink.Emit(done)
}

// Cancel aborts the in-flight turn. A goroutine blocked awaiting approval
// unblocks via the cancelled context.
func (c *Controller) Cancel() {
	c.cancelTurn(causeStop)
}

func (c *Controller) cancelTurn(cause cancelCause) {
	c.mu.Lock()
	cancel := c.gate.requestCancel(cause)
	c.mu.Unlock()
	if cancel != nil {
		c.approval.clearAll()
		cancel()
		return
	}
	if c.goals.active() {
		c.stopGoal(GoalStatusStopped)
	}
}

// CancelRequested reports whether Cancel has been requested for the active turn.
func (c *Controller) CancelRequested() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gate.canceling
}

// stripInterruptedSyntheticTurnMessagesAfter relocates a synthetic turn after
// an in-turn compaction has rewritten the pre-turn message index, then drops
// that whole controller-created turn.
func (c *Controller) stripInterruptedSyntheticTurnMessagesAfter(idx int) {
	if c.executor == nil {
		return
	}
	msgs := c.executor.Session().Snapshot()
	startedAt := c.inFlightTurnStartedAt()
	if start, ok := resolveInterruptedTurnStart(msgs, idx, false, startedAt, provider.Message{}); ok {
		idx = start
	}
	c.stripTurnMessagesAfter(idx)
}

// stripCancelledVisibleTurnMessagesAfterWithFallback preserves the real user
// prompt and fully paired tool rounds from a cancelled visible turn. Unsafe
// assistant/tool fragments are retained as provider-excluded display history.
// It also covers coordinator
// cancellation before the executor has appended the visible user message. The
// orchestrator owns that input, so it supplies the exact message rather than
// letting cancellation infer the current turn from older transcript history.
func (c *Controller) stripCancelledVisibleTurnMessagesAfterWithFallback(idx int, fallback provider.Message) {
	c.stripCancelledVisibleTurnMessagesAfterWithFallbackAt(idx, fallback, c.inFlightTurnStartedAt())
}

func (c *Controller) stripCancelledVisibleTurnMessagesAfterWithFallbackAt(idx int, fallback provider.Message, startedAt time.Time) {
	if c.executor == nil {
		return
	}
	msgs := c.executor.Session().Snapshot()
	if start, ok := resolveInterruptedTurnStart(msgs, idx, true, startedAt, fallback); ok {
		idx = start
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len(msgs) {
		idx = len(msgs)
	}
	next := append([]provider.Message{}, msgs[:idx]...)
	keptUser := false
	userEnd := idx
	for i, m := range msgs[idx:] {
		if m.Role != provider.RoleUser {
			continue
		}
		if IsSyntheticUserMessage(m.Content) {
			continue
		}
		if _, ok := sessionstore.SteerText(m.Content); ok {
			continue
		}
		m.Content = StripComposePrefixes(m.Content)
		next = append(next, m)
		keptUser = true
		userEnd = idx + i + 1
		break
	}
	if !keptUser && fallback.Role == provider.RoleUser {
		fallback.Content = StripComposePrefixes(fallback.Content)
		if strings.TrimSpace(fallback.Content) != "" {
			fallback.Images = append([]string(nil), fallback.Images...)
			next = append(next, fallback)
			keptUser = true
			userEnd = idx
		}
	}
	if !keptUser && len(msgs) <= idx {
		return
	}
	recovery := &provider.InterruptedTurnRecovery{Pending: true}
	localIndexes := make([]int, 0, 1)
	for i := userEnd; i < len(msgs); {
		m := msgs[i]
		if m.LocalOnly {
			m.Role = provider.RoleTool
			m.ToolCallID = provider.LocalOnlyToolID
			m.Name = provider.LocalOnlyToolName
			m.InterruptedTurn = nil
			m.ToolCalls = displayOnlyToolCalls(m.ToolCalls)
			next = append(next, m)
			localIndexes = append(localIndexes, len(next)-1)
			recovery.DroppedPartialText = recovery.DroppedPartialText || strings.TrimSpace(m.Content) != ""
			recovery.DroppedPartialReasoning = recovery.DroppedPartialReasoning || strings.TrimSpace(m.ReasoningContent) != ""
			for _, call := range m.ToolCalls {
				recovery.InterruptedTools = appendUniqueString(recovery.InterruptedTools, call.Name)
			}
			i++
			continue
		}
		// Auto-compaction can install a digest between the pinned current user
		// message and its recent tool tail. It summarizes pre-turn/current work
		// that is no longer present verbatim, so keep it provider-visible rather
		// than silently dropping context during recovery.
		if agent.IsCompactionSummary(m) {
			next = append(next, m)
			i++
			continue
		}
		if end, ok := completeToolTurnEnd(msgs, i); ok {
			next = append(next, msgs[i:end]...)
			for k, call := range m.ToolCalls {
				if toolResultWasInterrupted(msgs[i+1+k].Content) {
					recovery.InterruptedTools = appendUniqueString(recovery.InterruptedTools, call.Name)
					continue
				}
				recovery.CompletedTools = append(recovery.CompletedTools, interruptedToolSummary(call))
			}
			i = end
			continue
		}
		switch m.Role {
		case provider.RoleAssistant:
			local := m
			local.Role = provider.RoleTool
			local.LocalOnly = true
			local.ToolCallID = provider.LocalOnlyToolID
			local.Name = provider.LocalOnlyToolName
			local.InterruptedTurn = nil
			local.ReasoningSignature = ""
			local.ToolCalls = displayOnlyToolCalls(local.ToolCalls)
			next = append(next, local)
			localIndexes = append(localIndexes, len(next)-1)
			recovery.DroppedPartialText = recovery.DroppedPartialText || strings.TrimSpace(local.Content) != ""
			recovery.DroppedPartialReasoning = recovery.DroppedPartialReasoning || strings.TrimSpace(local.ReasoningContent) != ""
			for _, call := range local.ToolCalls {
				recovery.InterruptedTools = appendUniqueString(recovery.InterruptedTools, call.Name)
			}
		case provider.RoleTool:
			local := m
			local.LocalOnly = true
			local.ToolCalls = []provider.ToolCall{{ID: m.ToolCallID, Name: m.Name}}
			recovery.InterruptedTools = appendUniqueString(recovery.InterruptedTools, m.Name)
			local.ToolCallID = provider.LocalOnlyToolID
			local.Name = provider.LocalOnlyToolName
			next = append(next, local)
			localIndexes = append(localIndexes, len(next)-1)
		}
		i++
	}
	if len(localIndexes) == 0 {
		next = append(next, provider.Message{
			Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID,
			Name: provider.LocalOnlyToolName, LocalOnly: true,
		})
		localIndexes = append(localIndexes, len(next)-1)
	}
	next[localIndexes[len(localIndexes)-1]].InterruptedTurn = recovery
	c.replaceSessionAfterCancel(next)
}

func (c *Controller) hasInterruptedDisplayAfter(idx int, fallback provider.Message) bool {
	if c.executor == nil {
		return false
	}
	msgs := c.executor.Session().Snapshot()
	if start, ok := resolveInterruptedTurnStart(msgs, idx, true, c.inFlightTurnStartedAt(), fallback); ok {
		idx = start
	}
	idx = max(0, min(idx, len(msgs)))
	for _, m := range msgs[idx:] {
		if m.LocalOnly && m.InterruptedTurn != nil {
			return true
		}
	}
	return false
}

func (c *Controller) replaceSessionAfterCancel(msgs []provider.Message) {
	// The whole cleanup is a save/recovery handoff like snapshot's: hold
	// snapshotMu from the in-memory truncation onward. Truncating outside the
	// lock would let an in-flight save capture the shortened transcript, read
	// the longer partial autosave on disk as a stale-prefix conflict, and
	// adopt it back into the executor — silently undoing the cancel cleanup
	// before the flush below could persist it.
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	c.executor.Session().Replace(append([]provider.Message(nil), msgs...))
	// Rebuild canonical todo state from the truncated transcript so
	// Controller.Todos(), goal readiness, and the task panel no longer see
	// the in_progress items written by the cancelled turn.
	c.executor.RebuildTodoState()
	// The mid-turn autosave may have already written a partial transcript to
	// disk. snapshotActivityIfChanged skips the write when messageCount()
	// returns to startMessages, so flush the cleaned transcript here. SaveRewrite
	// still checks that this controller owns the current on-disk baseline before
	// overwriting it, and also covers the edge case where the strip leaves only a
	// system message (HasContent() == false). The path is read under the lock so
	// an in-flight recovery retarget cannot leave it stale.
	c.mu.Lock()
	path := c.sessionPath
	c.mu.Unlock()
	if path != "" {
		if err := c.executor.Session().SaveRewrite(path); err != nil {
			if errors.Is(err, sessionstore.ErrSessionSnapshotConflict) {
				if _, outcome, recoverErr := c.recoverSnapshotConflict(path, err, true); recoverErr != nil {
					slog.Warn("controller: post-cancel transcript recovery", "err", recoverErr)
				} else if outcome == conflictDropped {
					slog.Warn("controller: post-cancel transcript dropped after conflict", "path", path)
				}
			} else {
				slog.Warn("controller: post-cancel transcript flush", "err", err)
			}
		}
	}
}

func (c *Controller) markInFlightTurn(startMessageIndex int, preserveUser bool) sessionstore.InFlightTurnMeta {
	path := c.SessionPath()
	if path == "" {
		return sessionstore.InFlightTurnMeta{}
	}
	marker, err := sessionstore.BeginSessionInFlightTurn(path, startMessageIndex, preserveUser)
	if err != nil {
		slog.Warn("controller: mark in-flight turn", "err", err)
		return sessionstore.InFlightTurnMeta{}
	}
	return c.withInheritedInterruptions(marker)
}

// finishInFlightTurn persists the completed transcript before removing the
// crash marker. A crash can therefore leave either a recoverable marker or a
// durable completed transcript, never an unmarked in-memory-only suffix.
func (c *Controller) finishInFlightTurn(startMessages int, marker sessionstore.InFlightTurnMeta) {
	commitPrepared := marker.ID == ""
	if marker.ID != "" && c.executor != nil {
		digest, digestErr := c.executor.Session().ContentDigest()
		if digestErr != nil {
			slog.Warn("controller: compute completed turn digest", "err", digestErr)
		} else if prepared, matched, prepareErr := sessionstore.PrepareSessionInFlightTurnCommit(c.SessionPath(), marker, digest); prepareErr != nil {
			slog.Warn("controller: prepare in-flight turn commit", "err", prepareErr)
		} else if matched {
			marker = prepared
			commitPrepared = true
		}
	}
	durable, err := c.snapshotActivityIfChanged(startMessages)
	if err != nil && !durable {
		// Keep the marker when the transcript did not become durable. Resume can
		// then retry recovery instead of treating an in-memory-only tail as done.
		slog.Warn("controller: keeping in-flight marker after failed turn snapshot", "err", err)
		return
	}
	if err != nil {
		slog.Warn("controller: turn transcript saved before metadata update failed", "err", err)
	}
	if !commitPrepared {
		// Do not clear an unprepared marker: a crash between the snapshot and this
		// point would otherwise leave recovery without exact commit evidence.
		slog.Warn("controller: keeping in-flight marker without commit digest", "marker_id", marker.ID)
		return
	}
	c.clearInFlightTurn(marker)
}

// transplantInFlightTurnMarker moves a pending in-flight-turn marker from the
// session path a recovery fork abandoned onto the branch the turn continues
// on. Left behind, the stale marker would fire recoverInterruptedTurn on the
// next open of the original branch and strip messages from a turn that in
// fact kept running on the recovery branch; missing from the recovery branch,
// a crash before turn end would leave its partial tail unmarked.
func (c *Controller) transplantInFlightTurnMarker(fromPath, toPath string) {
	if strings.TrimSpace(fromPath) == "" || strings.TrimSpace(toPath) == "" || fromPath == toPath {
		return
	}
	meta, ok, err := sessionstore.LoadBranchMeta(fromPath)
	if err != nil || !ok || meta.InFlightTurn == nil {
		if err != nil {
			slog.Warn("controller: load in-flight turn marker for transplant", "path", fromPath, "err", err)
		}
		return
	}
	marker := meta.InFlightTurn
	if err := sessionstore.SetSessionInFlightTurn(toPath, *marker); err != nil {
		// Keep the original marker: a turn boundary on the wrong branch beats
		// no boundary anywhere if the runtime dies before the turn completes.
		slog.Warn("controller: transplant in-flight turn marker", "path", toPath, "err", err)
		return
	}
	if _, err := sessionstore.ClearSessionInFlightTurnIfMatch(fromPath, *marker); err != nil {
		slog.Warn("controller: clear in-flight turn marker on forked-from branch", "path", fromPath, "err", err)
	}
}

// interruptedTurnCrossesLaterTurn detects the data-loss shape where an old
// marker survived while one or more later turns were durably appended. A
// compaction summary and mid-turn steer are not new foreground turn boundaries.
func interruptedTurnCrossesLaterTurn(msgs []provider.Message, start int) bool {
	if start < 0 || start >= len(msgs) {
		return false
	}
	turns := 0
	for _, msg := range msgs[start:] {
		if msg.Role != provider.RoleUser || agent.IsCompactionSummary(msg) {
			continue
		}
		if _, ok := sessionstore.SteerText(msg.Content); ok {
			continue
		}
		turns++
		if turns > 1 {
			return true
		}
	}
	return false
}

// interruptedTurnContinuedOnRecoveryBranch reports whether a recovery branch
// forked off path after its in-flight-turn marker was set. Markers only exist
// while a turn runs and recovery forks happen on saves, so a child recovery
// branch younger than the marker means the marked turn itself moved there —
// the marker is a leftover from a runtime that switched paths mid-turn, not a
// crashed turn whose partial tail needs stripping. A marker without a start
// time is treated as continued whenever any recovery child exists: erring
// toward keeping messages is the data-safe direction.
func interruptedTurnContinuedOnRecoveryBranch(path string, marker *sessionstore.InFlightTurnMeta) bool {
	if marker == nil {
		return false
	}
	branches, err := sessionstore.ListBranches(filepath.Dir(path))
	if err != nil {
		return false
	}
	id := sessionstore.BranchID(path)
	for _, b := range branches {
		if b.Recovered && b.ParentID == id && b.CreatedAt.After(marker.StartedAt) {
			return true
		}
	}
	return false
}

// stripTurnMessagesAfter truncates the executor's session to keep only messages
// before the given index, discarding an incomplete synthetic turn (the synthetic
// user prompt plus every assistant/tool message that followed).
func (c *Controller) stripTurnMessagesAfter(idx int) {
	if c.executor == nil {
		return
	}
	msgs := c.executor.Session().Snapshot()
	if len(msgs) <= idx {
		return
	}
	c.replaceSessionAfterCancel(msgs[:idx])
}

func (c *Controller) inFlightTurnStartedAt() time.Time {
	path := c.SessionPath()
	if path == "" {
		return time.Time{}
	}
	meta, ok, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !ok || meta.InFlightTurn == nil {
		return time.Time{}
	}
	return meta.InFlightTurn.StartedAt
}

// resolveInterruptedTurnStart turns the pre-run array index into a stable
// boundary after compaction. New user messages carry a creation timestamp set
// after the marker, and graceful cleanup also has the exact composed prompt as
// a fallback. We only fall back to the legacy index when it still points at a
// plausible turn-start user message, keeping recovery data-safe for older
// sidecars without timestamps.
func resolveInterruptedTurnStart(msgs []provider.Message, idx int, preserveUser bool, startedAt time.Time, fallback provider.Message) (int, bool) {
	fallbackContent := ""
	if fallback.Role == provider.RoleUser {
		fallbackContent = StripComposePrefixes(fallback.Content)
	}
	matchesKind := func(m provider.Message) bool {
		if m.Role != provider.RoleUser {
			return false
		}
		if preserveUser {
			if IsSyntheticUserMessage(m.Content) {
				return false
			}
			if _, ok := sessionstore.SteerText(m.Content); ok {
				return false
			}
			if fallbackContent != "" && StripComposePrefixes(m.Content) != fallbackContent {
				return false
			}
		}
		return true
	}
	startedMillis := startedAt.UnixMilli()
	if !startedAt.IsZero() {
		for i, m := range msgs {
			if matchesKind(m) && m.CreatedAt >= startedMillis {
				return i, true
			}
		}
	}
	// Tests/headless runners may not persist an in-flight sidecar. The exact
	// graceful fallback still distinguishes the current visible turn; search
	// backward so a repeated prompt selects the newest occurrence.
	if fallbackContent != "" {
		for i, msg := range slices.Backward(msgs) {
			if matchesKind(msg) {
				return i, true
			}
		}
	}
	if idx >= 0 && idx < len(msgs) && matchesKind(msgs[idx]) {
		return idx, true
	}
	return 0, false
}

func completeToolTurnEnd(msgs []provider.Message, i int) (int, bool) {
	if i < 0 || i >= len(msgs) {
		return i, false
	}
	m := msgs[i]
	if m.LocalOnly || m.Role != provider.RoleAssistant || len(m.ToolCalls) == 0 {
		return i, false
	}
	end := i + 1
	for end < len(msgs) && msgs[end].Role == provider.RoleTool && !msgs[end].LocalOnly {
		end++
	}
	results := msgs[i+1 : end]
	if len(results) != len(m.ToolCalls) {
		return i, false
	}
	for k, call := range m.ToolCalls {
		if strings.TrimSpace(call.Name) == "" || (call.Arguments != "" && !json.Valid([]byte(call.Arguments))) {
			return i, false
		}
		if results[k].ToolCallID != call.ID || results[k].Name != call.Name {
			return i, false
		}
	}
	return end, true
}

func toolResultWasInterrupted(content string) bool {
	content = strings.ToLower(strings.TrimSpace(content))
	return strings.HasPrefix(content, "cancelled:") || strings.Contains(content, "context canceled") || strings.Contains(content, "context cancelled")
}

func displayOnlyToolCalls(calls []provider.ToolCall) []provider.ToolCall {
	out := make([]provider.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, provider.ToolCall{ID: call.ID, Name: strings.TrimSpace(call.Name)})
	}
	return out
}

func appendUniqueString(dst []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return dst
	}
	if slices.Contains(dst, value) {
		return dst
	}
	return append(dst, value)
}

func interruptedToolSummary(call provider.ToolCall) provider.InterruptedToolSummary {
	summary := provider.InterruptedToolSummary{
		ID: call.ID, Name: strings.TrimSpace(call.Name), Added: call.Added, Removed: call.Removed,
	}
	addFile := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || path == "/dev/null" || len(summary.Files) >= 8 {
			return
		}
		if slices.Contains(summary.Files, path) {
			return
		}
		summary.Files = append(summary.Files, path)
	}
	var args map[string]any
	if json.Unmarshal([]byte(call.Arguments), &args) == nil {
		for _, key := range []string{"path", "file", "file_path", "filename"} {
			if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
				addFile(value)
			}
		}
	}
	for line := range strings.SplitSeq(call.Diff, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			addFile(strings.TrimPrefix(line, "+++ b/"))
		case strings.HasPrefix(line, "--- a/"):
			addFile(strings.TrimPrefix(line, "--- a/"))
		case strings.HasPrefix(line, "*** Update File: "):
			addFile(strings.TrimPrefix(line, "*** Update File: "))
		case strings.HasPrefix(line, "*** Add File: "):
			addFile(strings.TrimPrefix(line, "*** Add File: "))
		case strings.HasPrefix(line, "*** Delete File: "):
			addFile(strings.TrimPrefix(line, "*** Delete File: "))
		}
	}
	return summary
}
