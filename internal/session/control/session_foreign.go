package control

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/state/sessionstore"
)

// continueForeignSession gives a conversation opened from a 1.x event log a
// session this build owns, the first time it has something to save. The 1.x
// files stay untouched, so 1.x still opens the conversation as it left it;
// the new session carries the same title so the person finds it again.
func (c *Controller) continueForeignSession(path string) (string, error) {
	s := c.executor.Session()
	newPath := sessionstore.NewSessionPath(filepath.Dir(path), c.Label())
	preview, turns := sessionstore.SessionPreviewFromMessages(s.Snapshot())
	meta := sessionstore.BranchMeta{Preview: preview, Turns: turns, SchemaVersion: sessionstore.BranchMetaCountsVersion}
	if old, ok, err := sessionstore.LoadBranchMeta(path); err == nil && ok {
		meta.Model = old.Model
		meta.CustomTitle = strings.TrimSpace(old.CustomTitle)
		if meta.CustomTitle == "" {
			meta.CustomTitle = strings.TrimSpace(old.TopicTitle)
		}
	}
	// The write authority has to move to the new path before anything is
	// written there, so the switch comes first and the save follows it.
	if err := c.commitRecoveredSession(path, "continued from a 1.x session", sessionstore.RecoveryBranchInfo{Path: newPath, Meta: meta}); err != nil {
		return "", err
	}
	if err := s.SaveIfAbsent(newPath); err != nil {
		return "", fmt.Errorf("continue 1.x session: %w", err)
	}
	if sessionstore.InheritCompactionState(path, newPath, s.Snapshot()) {
		c.bindExecutorProjection(newPath, true)
	}
	if err := sessionstore.SaveBranchMeta(newPath, meta); err != nil {
		slog.Warn("controller: meta for continued 1.x session", "path", newPath, "err", err)
	}
	c.sink.Emit(event.Event{
		Kind:  event.Notice,
		Level: event.LevelInfo,
		Code:  event.NoticeCodeSessionContinuedFrom1x,
		Text:  "this conversation was saved by Reasonix 1.x, which 2.x reads but does not write; it continues in a new 2.x session, and 1.x still has the original",
	})
	return newPath, nil
}

// leaveForeignSession moves a conversation opened from a 1.x log into a
// session of its own before a turn starts: a turn writes checkpoints, usage
// and wire records beside its transcript, and none of them belong beside 1.x's.
func (c *Controller) leaveForeignSession() error {
	path := c.SessionPath()
	if path == "" || c.executor == nil || !sessionstore.IsForeignSessionLog(path) {
		return nil
	}
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	_, err := c.continueForeignSession(path)
	return err
}

// settleForeignSave carries on from a save that met a 1.x log: an unchanged
// transcript stops the save with nothing written, and more than 1.x holds
// moves to a session of its own and saves there.
func (c *Controller) settleForeignSave(path string, err error) (string, bool, error) {
	switch {
	case errors.Is(err, sessionstore.ErrSessionLogUnchanged):
		return path, true, nil
	case errors.Is(err, sessionstore.ErrSessionLogForeign):
		moved, err := c.continueForeignSession(path)
		return moved, err != nil, err
	}
	return path, false, err
}
