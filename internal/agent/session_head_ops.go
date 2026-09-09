package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"reasonix/internal/store"
)

// ErrSessionHeadUnknown reports a head id that the log does not contain.
var ErrSessionHeadUnknown = errors.New("session head not found")

// ErrSessionNotDAG reports a head operation on a schema-1 session.
var ErrSessionNotDAG = errors.New("session log is not schema 2")

// ForkHead starts a new head at fromMessageID (empty means the root) and moves
// this session onto it: the in-memory transcript becomes that prefix and the
// next save appends behind it. kind is HeadKindFork or HeadKindRewind; the
// previous head keeps its full chain as a version. It returns the new head id.
func (s *Session) ForkHead(path, fromMessageID, kind, name string) (string, error) {
	if kind == "" {
		kind = HeadKindFork
	}
	newHead := NewHeadID()
	var st *sessionDAGState
	err := s.withSessionSaveLocks(path, func() error {
		var err error
		st, err = s.dagStateForHeadOp(path)
		if err != nil {
			return err
		}
		ref, _ := s.Head()
		current := ref.HeadID
		if current == "" || st.heads[current] == nil {
			current = st.selectedHead()
		}
		if fromMessageID != "" && st.nodes[fromMessageID] == nil {
			return fmt.Errorf("fork head: message %s: %w", fromMessageID, ErrSessionHeadUnknown)
		}
		now := time.Now().UTC()
		entries := []sessionDAGEntry{
			{Type: sessionDAGTypeFork, Head: current, NewHead: newHead, From: fromMessageID, Kind: kind, Name: strings.TrimSpace(name), At: now},
			{Type: sessionDAGTypeSelect, Head: newHead, Reason: kind, At: now},
		}
		return appendHeadEntries(path, st, entries)
	})
	if err != nil {
		return "", err
	}
	s.adoptHead(st, newHead, path)
	return newHead, nil
}

// SwitchHead moves this session onto an existing head and records the choice
// with a select marker, so a later open lands on it too.
func (s *Session) SwitchHead(path, headID string) error {
	var st *sessionDAGState
	err := s.withSessionSaveLocks(path, func() error {
		var err error
		st, err = s.dagStateForHeadOp(path)
		if err != nil {
			return err
		}
		h := st.heads[headID]
		if h == nil {
			return fmt.Errorf("switch head %s: %w", headID, ErrSessionHeadUnknown)
		}
		if h.retired {
			return fmt.Errorf("switch head %s: head is retired", headID)
		}
		if st.selected == headID {
			return nil
		}
		return appendHeadEntries(path, st, []sessionDAGEntry{{Type: sessionDAGTypeSelect, Head: headID, Reason: "switch", At: time.Now().UTC()}})
	})
	if err != nil {
		return err
	}
	s.adoptHead(st, headID, path)
	return nil
}

// SelectSessionHead records the default head of a session that is not open
// in this process (the versions UI acting on a closed conversation).
func SelectSessionHead(path, headID string) error {
	return appendSessionHeadMarker(path, headID, func(st *sessionDAGState) (sessionDAGEntry, error) {
		if st.heads[headID].retired {
			return sessionDAGEntry{}, fmt.Errorf("select head %s: head is retired", headID)
		}
		return sessionDAGEntry{Type: sessionDAGTypeSelect, Head: headID, Reason: "select"}, nil
	})
}

// RetireSessionHead marks a head as cleaned up. Its exclusive entries are
// dropped at the next single-writer rotation; until then it is hidden.
func RetireSessionHead(path, headID string) error {
	return appendSessionHeadMarker(path, headID, func(st *sessionDAGState) (sessionDAGEntry, error) {
		if st.selectedHead() == headID {
			return sessionDAGEntry{}, fmt.Errorf("retire head %s: it is the selected head", headID)
		}
		return sessionDAGEntry{Type: sessionDAGTypeRetire, Head: headID}, nil
	})
}

// RenameSessionHead sets the display name of a head.
func RenameSessionHead(path, headID, name string) error {
	return appendSessionHeadMarker(path, headID, func(*sessionDAGState) (sessionDAGEntry, error) {
		return sessionDAGEntry{Type: sessionDAGTypeRename, Head: headID, Name: strings.TrimSpace(name)}, nil
	})
}

func appendSessionHeadMarker(path, headID string, build func(*sessionDAGState) (sessionDAGEntry, error)) error {
	if strings.TrimSpace(path) == "" || headID == "" {
		return fmt.Errorf("session head marker: path and head id are required")
	}
	unlock := lockSessionSavePath(path)
	defer unlock()
	unlockFile, err := lockSessionFile(path)
	if err != nil {
		return fmt.Errorf("lock session file: %w", err)
	}
	defer unlockFile()
	st, err := replayDAGForHeadOp(path)
	if err != nil {
		return err
	}
	if st.heads[headID] == nil {
		return fmt.Errorf("head %s: %w", headID, ErrSessionHeadUnknown)
	}
	entry, err := build(st)
	if err != nil {
		return err
	}
	entry.At = time.Now().UTC()
	return appendHeadEntries(path, st, []sessionDAGEntry{entry})
}

// dagStateForHeadOp returns the replayed graph for a head operation on an
// open session, reusing the cached state like a save does.
func (s *Session) dagStateForHeadOp(path string) (*sessionDAGState, error) {
	if _, ok := s.Head(); !ok {
		return nil, ErrSessionNotDAG
	}
	return s.dagStateForSave(context.Background(), path, time.Now().UTC())
}

func replayDAGForHeadOp(path string) (*sessionDAGState, error) {
	probe, err := probeSessionEventLog(path)
	if err != nil {
		return nil, err
	}
	if !probe.dag {
		return nil, ErrSessionNotDAG
	}
	st, err := replaySessionDAG(context.Background(), store.SessionEventLog(path), defaultSessionReplayLimits)
	if err != nil {
		return nil, err
	}
	if st.damaged {
		if err := settleDAGTail(context.Background(), path, st, time.Now().UTC()); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// appendHeadEntries lands marker entries, folds them into st, and refreshes
// the head index and meta mirror so listings see the change at once.
func appendHeadEntries(path string, st *sessionDAGState, entries []sessionDAGEntry) error {
	tail := st.lastGoodEnd
	if _, err := appendSessionDAGEntries(path, entries, true); err != nil {
		return err
	}
	if err := st.replayFrom(context.Background(), tail, defaultSessionReplayLimits); err != nil {
		return err
	}
	if st.damaged {
		return fmt.Errorf("session log %s: head markers did not replay", path)
	}
	if err := writeSessionDAGIndex(context.Background(), path, st); err != nil {
		slog.Warn("session: head index write after head marker failed", "path", path, "err", err)
	}
	selected := st.selectedHead()
	if err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		meta.HeadID = selected
		meta.HeadCount = len(st.heads)
		meta.LogSchema = sessionDAGSchemaVersion
		meta.LogGeneration = st.generation
		return nil
	}); err != nil {
		slog.Warn("session: head metadata update after head marker failed", "path", path, "err", err)
	}
	return nil
}

// adoptHead points the live session at headID: the transcript becomes the
// head's materialized chain and the persisted baseline is that chain, so the
// next save diffs against it instead of the previous head.
func (s *Session) adoptHead(st *sessionDAGState, headID, path string) {
	msgs, _ := st.materialize(headID)
	digest, err := digestSessionMessages(msgs)
	s.mu.Lock()
	s.Messages = msgs
	s.version++
	s.rewriteVersion++
	version, rewriteVersion := s.version, s.rewriteVersion
	s.head.ref = HeadRef{HeadID: headID, LeafID: st.heads[headID].leaf, LogGeneration: st.generation, LogOffset: st.lastGoodEnd}
	s.head.dag = true
	s.head.state = st
	s.head.headCount = len(st.heads)
	s.head.openTurn = st.heads[headID].openTurn
	s.mu.Unlock()
	if err == nil {
		revision, _, _ := sessionContentRevision(path)
		s.setPersistedBaseline(path, digest, version, revision, true, true, rewriteVersion, msgs)
	}
}
