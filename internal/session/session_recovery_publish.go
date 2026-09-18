package session

type recoveryPublishState struct {
	checkpoint recoveryCheckpoint
	operations map[string]operationRecord
}

func (s *Session) recoveryForDurable(durable uint64) (recoveryPublishState, bool) {
	if s == nil || s.recovery == nil {
		return recoveryPublishState{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if durable+1 != s.next {
		return recoveryPublishState{}, false
	}
	store, _ := s.binding.handle.(*Store)
	if store == nil {
		return recoveryPublishState{}, false
	}
	store.mu.Lock()
	tip := store.tip
	store.mu.Unlock()
	projection := cloneProjection(s.projection)
	projection.Messages = nil
	checkpoint := recoveryCheckpoint{
		Version: recoveryFormatVersion, SessionID: s.id, StorageGeneration: s.storageGeneration,
		StorageRevision: s.manifest.StorageRevision, DurableSequence: durable, LogOffset: tip.LogOffset,
		AnchorOffset: tip.AnchorOffset, AnchorFirst: tip.AnchorFirst,
		AnchorCommitID: tip.AnchorCommitID, AnchorHash: tip.AnchorHash,
		ProjectionVersion: recoveryProjectionVersion, Projection: projection,
		RecentMessages: detachMessages(s.recentMessages), CatalogPreview: s.catalogPreview,
	}
	operations := make(map[string]operationRecord, len(s.commits))
	for _, commit := range s.commits {
		if commit.LastSequence() <= durable {
			operations[commit.OperationID] = compactOperationRecord(commit)
		}
	}
	return recoveryPublishState{checkpoint: checkpoint, operations: operations}, true
}

func (s *Session) recoveryPublished(durable uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if durable+1 == s.next {
		s.durableRecent = detachMessages(s.recentMessages)
	}
	cut := 0
	for cut < len(s.commits) && s.commits[cut].LastSequence() <= durable {
		delete(s.operations, s.commits[cut].OperationID)
		cut++
	}
	if cut > 0 {
		s.commits = append([]Commit(nil), s.commits[cut:]...)
	}
	if s.externalHistory {
		s.projection.Messages = nil
	}
}
