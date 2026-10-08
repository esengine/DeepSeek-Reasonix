package checkpoint

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrUndoRecoveryPending blocks new mutations until compensation completes.
var ErrUndoRecoveryPending = errors.New("rewind recovery is pending")

type undoState struct {
	current *TransactionManifest
	pending *TransactionManifest
}

func (s *Store) requireUndoRecoveryComplete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.undo.pending != nil {
		return ErrUndoRecoveryPending
	}
	return nil
}

func (s *Store) trackUndoTransaction(tx *TransactionManifest) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.undo.pending != nil {
		return nil, ErrUndoRecoveryPending
	}
	cp := *tx
	cp.State = TxCommitting
	s.undo.pending = &cp
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if tx.State == TxCommitted || tx.State == TxAborted {
			s.undo.pending = nil
		} else {
			cp := *tx
			s.undo.pending = &cp
		}
	}, nil
}

// LastUndoTransactionID returns the committed transaction id available for undo.
func (s *Store) LastUndoTransactionID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.undo.pending != nil || s.undo.current == nil || s.undo.current.State != TxCommitted {
		return ""
	}
	return s.undo.current.ID
}

// InvalidateUndo clears the last undo slot (new turn / new mutation / new rewind).
func (s *Store) InvalidateUndo() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.invalidateUndoLocked()
}

func (s *Store) invalidateUndoLocked() error {
	if s.undo.pending != nil {
		return ErrUndoRecoveryPending
	}
	if s.undo.current == nil {
		return nil
	}
	tx := *s.undo.current
	tx.State = TxInvalidated
	tx.UpdatedAt = time.Now()
	if err := s.persistTransaction(&tx); err != nil {
		return fmt.Errorf("persist undo invalidation %s: %w", tx.ID, err)
	}
	s.undo.current = nil
	return nil
}

// AvailableUndo reads a saved offer; UndoRewind validates files before executing.
func (s *Store) AvailableUndo() (*RewindUndo, error) {
	if s == nil {
		return nil, nil
	}
	if s.barrier.Busy() {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.undo.current
	if s.undo.pending != nil || tx == nil || tx.State != TxCommitted || tx.Kind != "rewind" || len(s.activeWriters) > 0 {
		return nil, nil
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

func (s *Store) restoreUndoSlot(latest *TransactionManifest, undoneParents map[string]bool, recovering []*TransactionManifest) {
	s.mu.Lock()
	s.undo = undoState{}
	for _, tx := range recovering {
		if tx.State == TxCommitting {
			cp := *tx
			s.undo.pending = &cp
			break
		}
	}
	if latest != nil && latest.State == TxCommitted && !undoneParents[latest.ID] {
		s.undo.current = latest
	}
	s.mu.Unlock()
}
