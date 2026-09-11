package control

import (
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func (c *Controller) markInFlightTurn(startMessageIndex int, preserveUser bool) agent.InFlightTurnMeta {
	path := c.SessionPath()
	if path == "" {
		return agent.InFlightTurnMeta{}
	}
	if s := c.loggedTurnSession(); s != nil {
		id := agent.NewMessageID()
		if s.QueueTurnBegin(id, preserveUser) {
			ref, _ := s.Head()
			return agent.InFlightTurnMeta{ID: id, StartMessageIndex: startMessageIndex, PreserveUser: preserveUser, StartedAt: time.Now().UTC(), HeadID: ref.HeadID}
		}
	}
	marker, err := agent.BeginSessionInFlightTurn(path, startMessageIndex, preserveUser)
	if err != nil {
		slog.Warn("controller: mark in-flight turn", "err", err)
		return agent.InFlightTurnMeta{}
	}
	return marker
}

func (c *Controller) clearInFlightTurn(marker agent.InFlightTurnMeta) {
	path := c.SessionPath()
	if path == "" || marker.ID == "" {
		return
	}
	if marker.HeadID != "" {
		if s := c.loggedTurnSession(); s != nil {
			s.QueueTurnEnd(marker.ID)
		}
		return
	}
	if _, err := agent.ClearSessionInFlightTurnIfMatch(path, marker); err != nil {
		slog.Warn("controller: clear in-flight turn", "err", err)
	}
}

// finishInFlightTurn persists the completed transcript before removing the
// crash marker. A crash can therefore leave either a recoverable marker or a
// durable completed transcript, never an unmarked in-memory-only suffix.
func (c *Controller) finishInFlightTurn(startMessages int, marker agent.InFlightTurnMeta) {
	if marker.HeadID != "" {
		c.finishLoggedTurn(startMessages, marker)
		return
	}
	commitPrepared := marker.ID == ""
	if marker.ID != "" && c.executor != nil {
		digest, digestErr := c.executor.Session().ContentDigest()
		if digestErr != nil {
			slog.Warn("controller: compute completed turn digest", "err", digestErr)
		} else if prepared, matched, prepareErr := agent.PrepareSessionInFlightTurnCommit(c.SessionPath(), marker, digest); prepareErr != nil {
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
	meta, ok, err := agent.LoadBranchMeta(fromPath)
	if err != nil || !ok || meta.InFlightTurn == nil {
		if err != nil {
			slog.Warn("controller: load in-flight turn marker for transplant", "path", fromPath, "err", err)
		}
		return
	}
	marker := meta.InFlightTurn
	if err := agent.SetSessionInFlightTurn(toPath, *marker); err != nil {
		// Keep the original marker: a turn boundary on the wrong branch beats
		// no boundary anywhere if the runtime dies before the turn completes.
		slog.Warn("controller: transplant in-flight turn marker", "path", toPath, "err", err)
		return
	}
	if _, err := agent.ClearSessionInFlightTurnIfMatch(fromPath, *marker); err != nil {
		slog.Warn("controller: clear in-flight turn marker on forked-from branch", "path", fromPath, "err", err)
	}
}

func (c *Controller) recoverInterruptedTurn(path string) {
	if c.executor == nil || path == "" {
		return
	}
	c.emitHeadEvents()
	if s := c.loggedTurnSession(); s != nil {
		if turn, open := s.OpenTurn(); open {
			c.recoverLoggedInterruptedTurn(s, turn)
			return
		}
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || meta.InFlightTurn == nil {
		if err != nil {
			slog.Warn("controller: load in-flight turn marker", "err", err)
		}
		return
	}
	marker := meta.InFlightTurn
	if interruptedTurnContinuedOnRecoveryBranch(path, marker) {
		// A recovery branch forked after the marker was set: the turn kept
		// running there, and older runtimes left the marker behind here.
		// Stripping would truncate a transcript the completed turn superseded.
		if _, err := agent.ClearSessionInFlightTurnIfMatch(path, *marker); err != nil {
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
			if _, err := agent.ClearSessionInFlightTurnIfMatch(path, *marker); err != nil {
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
		if _, err := agent.ClearSessionInFlightTurnIfMatch(path, *marker); err != nil {
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
	if _, err := agent.ClearSessionInFlightTurnIfMatch(path, *marker); err != nil {
		slog.Warn("controller: clear stale in-flight turn", "err", err)
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
		if !agent.IsUserAuthoredTurnMessage(msg) {
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
func interruptedTurnContinuedOnRecoveryBranch(path string, marker *agent.InFlightTurnMeta) bool {
	if marker == nil {
		return false
	}
	branches, err := agent.ListBranches(filepath.Dir(path))
	if err != nil {
		return false
	}
	id := agent.BranchID(path)
	for _, b := range branches {
		if b.Recovered && b.ParentID == id && b.CreatedAt.After(marker.StartedAt) {
			return true
		}
	}
	return false
}

// loggedTurnSession returns the executor session when its turn markers live
// in a schema-2 log, nil for schema-1 sessions that still use the sidecar.
func (c *Controller) loggedTurnSession() *agent.Session {
	if c.executor == nil {
		return nil
	}
	s := c.executor.Session()
	if s == nil {
		return nil
	}
	if _, ok := s.Head(); !ok {
		return nil
	}
	return s
}

// finishLoggedTurn queues the end marker before the completed transcript is
// saved, so the tail and its turn_end land in one appended batch. A failed
// save leaves the marker queued for the next one.
func (c *Controller) finishLoggedTurn(startMessages int, marker agent.InFlightTurnMeta) {
	if s := c.loggedTurnSession(); s != nil {
		s.QueueTurnEnd(marker.ID)
	}
	if durable, err := c.snapshotActivityIfChanged(startMessages); err != nil && !durable {
		slog.Warn("controller: keeping open turn marker after failed turn snapshot", "err", err)
	} else if err != nil {
		slog.Warn("controller: turn transcript saved before metadata update failed", "err", err)
	}
}

// recoverLoggedInterruptedTurn closes a turn a previous runtime left open in
// the schema-2 log. Its tail is dropped through a rewind marker (never a
// truncation) unless the turn visibly continued: on another head, or through
// later completed turns on this one.
func (c *Controller) recoverLoggedInterruptedTurn(s *agent.Session, turn agent.SessionOpenTurn) {
	finish := func() {
		s.QueueTurnEnd(turn.TurnID)
		if err := c.snapshot(false, true, false); err != nil {
			slog.Warn("controller: post-interrupted-turn snapshot", "err", err)
		}
	}
	if s.TurnContinuedOnOtherHead(turn.LeafID) {
		finish()
		return
	}
	msgs := s.Snapshot()
	start := 0
	if turn.LeafID != "" {
		if idx := s.IndexOfID(turn.LeafID); idx >= 0 {
			start = idx + 1
		} else {
			start = len(msgs)
		}
	}
	if interruptedTurnCrossesLaterTurn(msgs, start) {
		slog.Warn("controller: preserving transcript after stale open turn marker",
			"turn", turn.TurnID, "messages", len(msgs), "resolved_index", start)
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
			Text: "Session recovery found completed turns after a stale interruption marker; the full history was preserved."})
		finish()
		return
	}
	if len(msgs) > start {
		if turn.PreserveUser {
			c.stripCancelledVisibleTurnMessagesAfterWithFallbackAt(start, provider.Message{}, turn.StartedAt)
		} else {
			c.stripTurnMessagesAfter(start)
		}
	}
	finish()
}

// emitHeadEvents turns the head facts a save or load recorded into notices.
func (c *Controller) emitHeadEvents() {
	if c.executor == nil {
		return
	}
	s := c.executor.Session()
	if s == nil {
		return
	}
	for _, ev := range s.DrainHeadEvents() {
		switch ev.Kind {
		case agent.HeadEventForkedConcurrent:
			c.sink.Emit(sessionRecoveryNotice(event.NoticeCodeSessionConcurrentWriter,
				"another Reasonix window or process added to this conversation; its content is kept as a separate version"))
		case agent.HeadEventMultipleRecentHeads:
			c.sink.Emit(sessionRecoveryNotice(event.NoticeCodeSessionHeadSwitched,
				"opened the newest version of this conversation; other saved versions are available in View versions"))
		}
	}
}
