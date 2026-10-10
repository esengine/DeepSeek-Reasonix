package checkpoint

import (
	"fmt"
	"log/slog"
	"slices"
	"time"
)

// syncRestoredOwnership records the state published by a transaction without
// invalidating its undo offer or replacing historical preimages. It also runs
// for undo transactions, whose restore image is the original forward state.
func (s *Store) syncRestoredOwnership(targets []TransactionTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	checkpoints := s.all()
	updates := make(map[*Checkpoint]*Checkpoint)
	for _, target := range targets {
		key := NormalizeRelPath(s.root, target.Path)
		for _, c := range slices.Backward(checkpoints) {
			i := slices.IndexFunc(c.Files, func(f FileSnap) bool {
				return NormalizeRelPath(s.root, f.Path) == key
			})
			if i < 0 {
				continue
			}
			updated := updates[c]
			if updated == nil {
				copy := *c
				copy.Files = slices.Clone(c.Files)
				updated = &copy
				updates[c] = updated
			}
			existed := target.RestoreExisted
			updated.Files[i].AfterExisted = &existed
			updated.Files[i].AfterSHA256 = target.RestoreSHA
			updated.Files[i].AfterMode = target.RestoreMode
			break
		}
	}

	// Save every touched record before publishing memory; compensation also
	// repairs partial disk saves whose in-memory values already match.
	for _, c := range checkpoints {
		if updated := updates[c]; updated != nil {
			if err := s.persist(updated); err != nil {
				return fmt.Errorf("save file ownership for turn %d: %w", c.Turn, err)
			}
		}
	}
	for c, updated := range updates {
		*c = *updated
	}
	return nil
}

// syncForwardOwnership follows successful compensation back to the state the
// transaction observed before publication. No disk contents become trusted here.
func (s *Store) syncForwardOwnership(targets []TransactionTarget) error {
	forward := make([]TransactionTarget, len(targets))
	for i, target := range targets {
		forward[i] = TransactionTarget{
			Path:           target.Path,
			RestoreSHA:     target.ForwardSHA,
			RestoreExisted: target.ForwardExisted,
			RestoreMode:    target.ForwardMode,
		}
	}
	return s.syncRestoredOwnership(forward)
}

func (s *Store) finishOwnedUndo(undo, original *TransactionManifest, applier ConversationApplier, result RewindResult, stages []FileStage) (RewindResult, error) {
	if err := s.syncRestoredOwnership(undo.Targets); err != nil {
		var restoreErr error
		if original.Scope != RewindCode {
			restoreErr = s.restoreOriginalRewind(original, applier)
		}
		err = s.failTransactionAfterStateCompensation(undo, undo.Targets, stages, err, restoreErr)
		result.Error = err.Error()
		result.Files = stages
		return result, err
	}
	undo.State = TxCommitted
	undo.UpdatedAt = time.Now()
	if err := s.persistTransaction(undo); err != nil {
		restoreErr := s.restoreOriginalRewind(original, applier)
		err = s.failTransactionAfterStateCompensation(undo, undo.Targets, stages, err, restoreErr)
		result.OK = false
		result.Error = err.Error()
		result.Files = stages
		return result, err
	}

	s.cleanupCommittedBackups(undo.Targets)

	// Mark original as undone; clear lastUndo.
	original.State = TxUndone
	original.UpdatedAt = time.Now()
	if err := s.persistTransaction(original); err != nil {
		// The committed undo manifest durably names its parent, so startup will
		// suppress the stale parent even if this secondary write failed.
		slog.Warn("checkpoint: persist original transaction as undone", "err", err)
	}
	s.mu.Lock()
	s.lastUndo = nil
	s.mu.Unlock()

	result.OK = true
	result.UndoAvailable = false
	result.Files = stages
	return result, nil
}

func (s *Store) finishOwnedRewind(tx *TransactionManifest, applier ConversationApplier, inject *InjectFail, result RewindResult, stages []FileStage) (RewindResult, error) {
	if err := s.syncRestoredOwnership(tx.Targets); err != nil {
		restoreErr := s.restoreTransactionConversation(tx, applier)
		err = s.failTransactionAfterStateCompensation(tx, tx.Targets, stages, err, restoreErr)
		result.Error = err.Error()
		result.Files = stages
		return result, err
	}
	if inject != nil && inject.Phase == "finalize" {
		err := fmt.Errorf("injected failure at finalize")
		restoreErr := s.restoreTransactionConversation(tx, applier)
		err = s.failTransactionAfterStateCompensation(tx, tx.Targets, stages, err, restoreErr)
		result.OK = false
		result.Error = err.Error()
		result.Files = stages
		return result, err
	}
	if inject != nil && inject.Phase == "after_conversation_before_finalize" {
		// Simulate process death after both conversation mutations are durable but
		// before the transaction can be marked committed. Startup must restore the
		// forward transcript/checkpoints before compensating files.
		err := fmt.Errorf("injected crash after conversation before finalize")
		result.Error = err.Error()
		result.Files = stages
		return result, err
	}

	tx.State = TxCommitted
	tx.UpdatedAt = time.Now()
	if err := s.persistTransaction(tx); err != nil {
		restoreErr := s.restoreTransactionConversation(tx, applier)
		err = s.failTransactionAfterStateCompensation(tx, tx.Targets, stages, err, restoreErr)
		result.OK = false
		result.Error = err.Error()
		result.Files = stages
		return result, err
	}
	s.cleanupCommittedBackups(tx.Targets)
	s.mu.Lock()
	s.lastUndo = tx
	s.mu.Unlock()

	result.OK = true
	result.UndoAvailable = true
	result.Files = stages
	return result, nil
}

func (s *Store) finishOwnedCompensation(targets []TransactionTarget, err error) error {
	if err != nil {
		return err
	}
	return s.syncForwardOwnership(targets)
}
