package sessioninbox

import "time"

// LookupEnvelopeReceipt checks semantic identity before sources are read again.
func (s *Store) LookupEnvelopeReceipt(key string, env PromptEnvelope) (InboxReceipt, bool, error) {
	if key == "" {
		return InboxReceipt{}, false, nil
	}
	hash, err := idempotencyRequestHash(env)
	if err != nil {
		return InboxReceipt{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return InboxReceipt{}, false, err
	}
	defer release()
	return s.idempotentReceiptLocked(key, hash)
}

// LookupReceipt reads the existing bounded idempotency records without
// creating or replaying a write. Used after an uncertain transport outcome.
func (s *Store) LookupReceipt(key string) (InboxReceipt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		return InboxReceipt{}, false
	}
	if id, ok := s.man.Idempotency[key]; ok {
		if _, found := s.man.item(id); found {
			return InboxReceipt{ItemID: id, Disposition: DispositionIdempotentHit, Position: s.man.indexOf(id) + 1, Paused: s.man.Paused, Idempotent: true}, true
		}
	}
	if receipt, ok := s.man.Receipts[key]; ok && time.Since(receipt.CompletedAt) <= idempotencyReceiptTTL {
		return InboxReceipt{ItemID: receipt.ItemID, Disposition: DispositionIdempotentHit, Paused: s.man.Paused, Idempotent: true}, true
	}
	return InboxReceipt{}, false
}

func (s *Store) idempotentReceiptLocked(key, requestHash string) (InboxReceipt, bool, error) {
	if key == "" {
		return InboxReceipt{}, false, nil
	}
	if id, ok := s.man.Idempotency[key]; ok {
		if item, found := s.man.item(id); found {
			if previous := s.man.IdempotencyHashes[key]; previous != "" && previous != requestHash {
				return InboxReceipt{}, false, ErrIdempotencyConflict
			}
			return InboxReceipt{
				ItemID: item.ID, Disposition: DispositionIdempotentHit,
				Position: s.man.indexOf(item.ID) + 1, Paused: s.man.Paused,
				Capacity: s.snapshotLocked().Capacity, Idempotent: true,
			}, true, nil
		}
	}
	receipt, ok := s.man.Receipts[key]
	if !ok || time.Since(receipt.CompletedAt) > idempotencyReceiptTTL {
		return InboxReceipt{}, false, nil
	}
	if receipt.RequestHash != requestHash {
		return InboxReceipt{}, false, ErrIdempotencyConflict
	}
	return InboxReceipt{
		ItemID: receipt.ItemID, Disposition: DispositionIdempotentHit,
		Paused: s.man.Paused, Capacity: s.snapshotLocked().Capacity, Idempotent: true,
	}, true, nil
}

func (s *Store) idempotentAliasReplayLocked(key, requestHash, itemID string) (bool, error) {
	if key == "" {
		return false, nil
	}
	if existingID, ok := s.man.Idempotency[key]; ok {
		if existingHash := s.man.IdempotencyHashes[key]; existingHash != "" && existingHash != requestHash {
			return false, ErrIdempotencyConflict
		}
		if existingID != itemID {
			return false, ErrIdempotencyConflict
		}
		return true, nil
	}
	receipt, ok := s.man.Receipts[key]
	if !ok || time.Since(receipt.CompletedAt) > idempotencyReceiptTTL {
		return false, nil
	}
	if receipt.RequestHash != requestHash || receipt.ItemID != itemID {
		return false, ErrIdempotencyConflict
	}
	return true, nil
}

func bindIdempotency(m *manifest, key, itemID, requestHash string) {
	if key == "" {
		return
	}
	if m.Idempotency == nil {
		m.Idempotency = map[string]string{}
	}
	if m.IdempotencyHashes == nil {
		m.IdempotencyHashes = map[string]string{}
	}
	m.Idempotency[key] = itemID
	m.IdempotencyHashes[key] = requestHash
}
