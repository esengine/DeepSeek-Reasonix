package checkpoint

import (
	"fmt"
	"log/slog"
	"os"
	"time"
)

// LastUndoTransactionID returns the committed transaction id available for undo.
func (s *Store) LastUndoTransactionID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastUndo == nil || s.lastUndo.State != TxCommitted {
		return ""
	}
	return s.lastUndo.ID
}

// InvalidateUndo clears the last undo slot (new turn / new mutation / new rewind).
func (s *Store) InvalidateUndo() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.invalidateUndoLocked()
	s.mu.Unlock()
}

func (s *Store) invalidateUndoLocked() {
	if s.lastUndo == nil {
		return
	}
	tx := *s.lastUndo
	tx.State = TxInvalidated
	tx.UpdatedAt = time.Now()
	if err := s.persistTransaction(&tx); err != nil {
		slog.Warn("checkpoint: persist undo invalidation failed", "transaction", tx.ID, "err", err)
	}
	s.lastUndo = nil
}

// AvailableUndo reads the undo offer from backend state, including disk validation.
func (s *Store) AvailableUndo() (*RewindUndo, error) {
	if s == nil {
		return nil, nil
	}
	if s.barrier.Busy() {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.lastUndo
	if tx == nil || tx.State != TxCommitted || tx.Kind != "rewind" || len(s.activeWriters) > 0 {
		return nil, nil
	}
	conflicts, err := s.undoFileConflicts(tx)
	if err != nil || len(conflicts) > 0 {
		return nil, err
	}
	return &RewindUndo{TransactionID: tx.ID, Turn: tx.Turn, Files: len(tx.Targets)}, nil
}

func (s *Store) undoFileConflicts(tx *TransactionManifest) ([]RewindConflict, error) {
	for _, t := range tx.Targets {
		fp, err := FingerprintPath(s.root, t.AbsPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("fingerprint %s before undo: %w", t.Path, err)
		}
		// After commit, disk should match restore image. If it doesn't, refuse.
		if t.Action == "delete" {
			if fp.Existed {
				return []RewindConflict{{
					Path: t.Path, Reason: ConflictManualEdit, CurrentSHA: fp.SHA256,
				}}, nil
			}
		} else {
			if !fingerprintMatches(fp, t.RestoreExisted, t.RestoreSHA, t.RestoreMode) {
				restoreExisted := t.RestoreExisted
				return []RewindConflict{{
					Path: t.Path, Reason: CompareIdentity(fp, t.RestoreSHA, &restoreExisted, t.RestoreMode),
					CurrentSHA: fp.SHA256, LastOwnedSHA: t.RestoreSHA, CurrentMode: fp.Mode, CheckpointMode: t.RestoreMode,
				}}, nil
			}
		}
	}
	return nil, nil
}

func (s *Store) restoreUndoSlot(latest *TransactionManifest, undoneParents map[string]bool) {
	s.mu.Lock()
	s.lastUndo = nil
	if latest != nil && latest.State == TxCommitted && !undoneParents[latest.ID] {
		s.lastUndo = latest
	}
	s.mu.Unlock()
}
