package checkpoint

import (
	"errors"
	"fmt"
	"time"
)

func (s *Store) recoverInMemoryUndo(applier ConversationApplier) []string {
	s.mu.Lock()
	if s.undo.pending == nil {
		s.mu.Unlock()
		return nil
	}
	tx := *s.undo.pending
	s.mu.Unlock()
	notes := s.recoverCommittingTransaction(&tx, applier)
	s.mu.Lock()
	defer s.mu.Unlock()
	if tx.State == TxAborted {
		s.undo.pending = nil
	} else {
		s.undo.pending = &tx
	}
	return notes
}

func (s *Store) recoverCommittingTransaction(tx *TransactionManifest, applier ConversationApplier) []string {
	var notes []string
	needsConversation := tx.Scope == RewindConversation || tx.Scope == RewindBoth
	if needsConversation && applier == nil {
		notes = append(notes, fmt.Sprintf("deferred conversation recovery %s", tx.ID))
		return notes
	}
	if needsConversation {
		var restoreErr error
		if tx.Kind != "undo" && tx.HasBoundary && len(tx.ConversationForward) == 0 {
			restoreErr = fmt.Errorf("missing forward conversation payload")
		} else if tx.Kind == "undo" {
			if tx.HasBoundary {
				restoreErr = errors.Join(restoreErr, applier.ApplyConversationTruncate(tx.BoundaryIndex, tx.ConversationForward))
			}
			restoreErr = errors.Join(restoreErr, applier.TruncateCheckpoints(tx.TruncateFrom))
		} else {
			restoreErr = s.restoreTransactionConversation(tx, applier)
		}
		if restoreErr != nil {
			notes = append(notes, fmt.Sprintf("conversation recovery %s pending: %v", tx.ID, restoreErr))
			tx.Error = fmt.Sprintf("crash recovery conversation compensation pending: %v", restoreErr)
			tx.UpdatedAt = time.Now()
			_ = s.persistTransaction(tx)
			return notes
		}
	}
	// Compensate published files back to forward images.
	stages := make([]FileStage, len(tx.Targets))
	for i, t := range tx.Targets {
		stages[i] = FileStage{Path: t.Path, Phase: "compensate"}
	}
	if err := s.compensatePublished(tx.Targets, stages); err != nil {
		notes = append(notes, fmt.Sprintf("compensate %s: %v", tx.ID, err))
		tx.Error = fmt.Sprintf("crash recovery compensation pending: %v", err)
		tx.UpdatedAt = time.Now()
		_ = s.persistTransaction(tx)
	} else {
		notes = append(notes, fmt.Sprintf("compensated committing %s", tx.ID))
		tx.State = TxAborted
		tx.Error = "compensated after crash during commit"
		tx.UpdatedAt = time.Now()
		if err := s.persistTransaction(tx); err != nil {
			tx.State = TxCommitting
			notes = append(notes, fmt.Sprintf("persist compensated transaction %s: %v", tx.ID, err))
		}
	}
	return notes
}
