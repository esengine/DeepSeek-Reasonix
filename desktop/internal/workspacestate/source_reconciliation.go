package workspacestate

import (
	"context"
	"reflect"
	"slices"
	"strings"
)

// Archive reservations own unpublished identities. Neither an ordinary
// preparation nor a second source can publish an archive child active.
func validateArchiveImportReservation(state *State, op Operation) error {
	if op.Kind != "import" && op.Kind != "archive-import" && !(op.Kind == "restore" && op.Mapping != nil) {
		return nil
	}
	for _, other := range state.PendingOperations {
		if other.Phase == "committed" || (other.Kind != "import" && other.Kind != "archive-import") {
			continue
		}
		if op.Kind != "archive-import" && other.Kind != "archive-import" {
			continue
		}
		for _, id := range op.SessionIDs {
			if slices.Contains(other.SessionIDs, id) {
				return ErrMutationConflict
			}
		}
	}
	return nil
}

func (s *Store) RecordSource(ctx context.Context, mapping SourceMapping, presentation Presentation) error {
	return s.mutate(ctx, func(state *State) error {
		if old, ok := state.SourceMappings[mapping.SourceKey]; ok {
			if old.SessionID != mapping.SessionID || old.Fingerprint != mapping.Fingerprint {
				debugConflict("RecordSource.mapping-exists", "sourceKey", mapping.SourceKey,
					"oldSession", old.SessionID, "newSession", mapping.SessionID,
					"oldFingerprint", old.Fingerprint, "newFingerprint", mapping.Fingerprint)
				return ErrMutationConflict
			}
			return nil
		}
		state.SourceMappings[mapping.SourceKey] = mapping
		adoptOrganizationSource(state, mapping)
		if _, exists := state.Presentation[mapping.SessionID]; !exists {
			state.Presentation[mapping.SessionID] = presentation
		}
		return nil
	})
}

// RecordRecoveredSource repairs a missing current receipt without changing the
// destination's content, presentation or lifecycle. The caller proves adoption
// from frozen content; the transaction fences competing owners and lifecycle.
func (s *Store) RecordRecoveredSource(ctx context.Context, mapping SourceMapping, generation uint64) error {
	return s.mutate(ctx, func(state *State) error {
		if state.Generation != generation || mapping.SourceKey == "" || mapping.Fingerprint == "" {
			return ErrMutationConflict
		}
		if existing, found := state.SourceMappings[mapping.SourceKey]; found {
			if existing.SessionID != mapping.SessionID || existing.Fingerprint != mapping.Fingerprint {
				return ErrMutationConflict
			}
			return nil
		}
		lifecycle := state.SessionStates[mapping.SessionID].Lifecycle
		if !validLifecycle(lifecycle) {
			return ErrMutationConflict
		}
		if lifecycle != Deleted {
			if owner, found := sessionOwner(*state, mapping.SessionID); !found || owner != mapping.WorkspaceID {
				return ErrMutationConflict
			}
		}
		for _, old := range state.SourceMappings {
			if !slices.Contains(state.SourceKeys(old.SourceKey), mapping.SourceKey) {
				continue
			}
			if old.SessionID == mapping.SessionID && old.WorkspaceID == mapping.WorkspaceID && old.Fingerprint == mapping.Fingerprint {
				continue
			} else if state.SessionStates[old.SessionID].Lifecycle != Deleted {
				return ErrMutationConflict
			}
		}
		for _, op := range state.PendingOperations {
			if op.Phase != "committed" && op.Mapping != nil && slices.Contains(state.SourceKeys(op.Mapping.SourceKey), mapping.SourceKey) {
				return ErrMutationConflict
			}
		}
		if err := backupHistoricalVersionRegistry(s.path); err != nil {
			return err
		}
		state.SourceMappings[mapping.SourceKey] = mapping
		return nil
	})
}

// RecordRetiredSource repairs a missing receipt without recreating membership
// or presentation. The caller proves source content while holding its read lock;
// the generation fence protects the observed lifecycle and competing mappings.
func (s *Store) RecordRetiredSource(ctx context.Context, mapping SourceMapping, generation uint64) error {
	return s.mutate(ctx, func(state *State) error {
		if state.Generation != generation || mapping.SourceKey == "" || mapping.Fingerprint == "" {
			return ErrMutationConflict
		}
		lifecycle := state.SessionStates[mapping.SessionID].Lifecycle
		if lifecycle != Deleted && lifecycle != Archived {
			return ErrMutationConflict
		}
		if old, ok, err := state.ResolveSource(mapping.SourceKey); err != nil {
			return err
		} else if ok {
			if old.SessionID != mapping.SessionID || old.Fingerprint != mapping.Fingerprint {
				return ErrMutationConflict
			}
			return nil
		}
		for _, op := range state.PendingOperations {
			if op.Phase != "committed" && op.Mapping != nil && op.Mapping.SourceKey == mapping.SourceKey {
				return ErrMutationConflict
			}
		}
		if err := backupHistoricalVersionRegistry(s.path); err != nil {
			return err
		}
		state.SourceMappings[mapping.SourceKey] = mapping
		return nil
	})
}

// StageHistoricalArchive transfers an unpublished import to an explicit archive
// request. Reusing its reservation prevents recovery from publishing it active.
func (s *Store) StageHistoricalArchive(ctx context.Context, observed Operation, generation uint64, destinations ...string) error {
	return s.mutate(ctx, func(state *State) error {
		op, ok := state.PendingOperations[observed.ID]
		if !ok || state.Generation != generation || !reflect.DeepEqual(op, observed) || op.Kind != "import" ||
			(op.Phase != "prepared" && op.Phase != "content_ready") || op.Mapping == nil || len(op.SessionIDs) != 1 {
			return ErrMutationConflict
		}
		id := op.SessionIDs[0]
		if op.Mapping.SessionID != id || op.Mapping.WorkspaceID != op.WorkspaceID || op.RecoveryEntryID != "" || len(op.Dependencies) != 0 {
			return ErrMutationConflict
		}
		if _, exists := state.SessionStates[id]; exists {
			return ErrMutationConflict
		}
		if _, attached := sessionOwner(*state, id); attached {
			return ErrMutationConflict
		}
		for key, other := range state.PendingOperations {
			if key != op.ID && other.Phase != "committed" && (slices.Contains(other.SessionIDs, id) || slices.Contains(other.Dependencies, op.ID)) {
				return ErrMutationConflict
			}
		}
		if len(destinations) > 0 && destinations[0] != op.Mapping.SourceKey {
			var err error
			op, err = rekeyHistoricalArchive(state, op, destinations[0])
			if err != nil {
				return err
			}
		}
		if err := backupHistoricalVersionRegistry(s.path); err != nil {
			return err
		}
		op.Kind, op.Lifecycle = "archive-import", Archived
		state.PendingOperations[op.ID] = op
		return nil
	})
}

// rekeyHistoricalArchive keeps a changed source version separate from its
// original receipt while preserving the unpublished target reservation.
func rekeyHistoricalArchive(state *State, op Operation, key string) (Operation, error) {
	base := strings.TrimSuffix(key, ":review:"+op.Mapping.Fingerprint)
	if base == key || !slices.Contains(state.SourceKeys(op.Mapping.SourceKey), base) {
		return op, ErrMutationConflict
	}
	if _, exists, err := state.ResolveSource(key); exists || err != nil {
		return op, ErrMutationConflict
	}
	for otherID, other := range state.PendingOperations {
		if otherID != op.ID && other.Phase != "committed" && other.Mapping != nil && other.Mapping.SourceKey == key {
			return op, ErrMutationConflict
		}
	}
	mapping := *op.Mapping
	mapping.SourceKey = key
	op.Mapping = &mapping
	return op, nil
}
