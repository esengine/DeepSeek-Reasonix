package workspacestate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"reasonix/internal/fileutil"
)

const (
	Active   = "active"
	Archived = "archived"
	Deleted  = "deleted"
)

type SessionState struct {
	Lifecycle  string `json:"lifecycle"`
	Generation uint64 `json:"generation"`
	ArchivedAt int64  `json:"archivedAt,omitempty"`
	extra      map[string]json.RawMessage
}
type SourceMapping struct {
	RetainedArtifacts []string `json:"retainedArtifacts,omitempty"`
	SourceKey         string   `json:"sourceKey"`
	Path              string   `json:"path"`
	HeadID            string   `json:"headId,omitempty"`
	Format            string   `json:"format"`
	Fingerprint       string   `json:"fingerprint"`
	SessionID         string   `json:"sessionId"`
	WorkspaceID       string   `json:"workspaceId"`
	extra             map[string]json.RawMessage
}
type Presentation struct {
	TopicID   string `json:"topicId,omitempty"`
	Title     string `json:"title,omitempty"`
	Pinned    bool   `json:"pinned,omitempty"`
	SortOrder int    `json:"sortOrder"`
	extra     map[string]json.RawMessage
}
type RecoveryEntry struct {
	ID            string `json:"id"`
	SourceKey     string `json:"sourceKey"`
	Path          string `json:"path,omitempty"`
	HeadID        string `json:"headId,omitempty"`
	SessionID     string `json:"sessionId,omitempty"`
	WorkspaceID   string `json:"workspaceId,omitempty"`
	Scope         string `json:"scope,omitempty"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	Format        string `json:"format"`
	Reason        string `json:"reason"`
	Status        string `json:"status"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	extra         map[string]json.RawMessage
}
type Operation struct {
	RequestFingerprint string          `json:"requestFingerprint,omitempty"`
	Request            json.RawMessage `json:"request,omitempty"`
	Result             json.RawMessage `json:"result,omitempty"`
	ID                 string          `json:"id"`
	Kind               string          `json:"kind"`
	Phase              string          `json:"phase"`
	SessionIDs         []string        `json:"sessionIds"`
	WorkspaceID        string          `json:"workspaceId,omitempty"`
	Lifecycle          string          `json:"lifecycle"`
	ExpectedGeneration uint64          `json:"expectedGeneration"`
	ResultGeneration   uint64          `json:"resultGeneration,omitempty"`
	RecoveryEntryID    string          `json:"recoveryEntryId,omitempty"`
	Mapping            *SourceMapping  `json:"mapping,omitempty"`
	Presentation       *Presentation   `json:"presentation,omitempty"`
	Dependencies       []string        `json:"dependencies,omitempty"`
	extra              map[string]json.RawMessage
}

type PurgeState uint8

const (
	PurgeAbsent PurgeState = iota
	PurgePrepared
	PurgePreparedStale
	PurgeTombstoned
	PurgeContentRemoved
	PurgeCommitted
	PurgeInvalid
)

func validLifecycle(value string) bool {
	return value == Active || value == Archived || value == Deleted
}
func setLifecycle(state *State, id, lifecycle string) {
	current := state.SessionStates[id]
	if lifecycle == Archived && current.Lifecycle != Archived {
		current.ArchivedAt = time.Now().UnixMilli()
	}
	current.Lifecycle, current.Generation = lifecycle, state.Generation+1
	state.SessionStates[id] = current
}
func validateLifecycleState(state State) error {
	for id, item := range state.SessionStates {
		if strings.TrimSpace(id) == "" || !validLifecycle(item.Lifecycle) {
			return fmt.Errorf("%w: session lifecycle", ErrUnsupportedVersion)
		}
	}
	for key, item := range state.SourceMappings {
		if key == "" || item.SourceKey != key || item.SessionID == "" || item.Fingerprint == "" {
			return errors.New("invalid source mapping")
		}
	}
	for key, item := range state.PendingOperations {
		if key == "" || item.ID != key || !validLifecycle(item.Lifecycle) {
			return errors.New("invalid session operation")
		}
		switch item.Kind {
		case "import", "archive-import", "archive", "purge", "restore", "command":
		default:
			return fmt.Errorf("%w: operation kind", ErrUnsupportedVersion)
		}
		switch item.Phase {
		case "prepared", "content_ready", "committed":
		case "tombstoned", "content_removed":
			if item.Kind != "purge" {
				return ErrUnsupportedVersion
			}
		default:
			return fmt.Errorf("%w: operation phase", ErrUnsupportedVersion)
		}
	}
	for key, item := range state.RecoveryEntries {
		if key == "" || item.ID != key || item.SourceKey == "" {
			return errors.New("invalid recovery entry")
		}
		switch item.Status {
		case "pending", "failed", "restored":
		default:
			return fmt.Errorf("%w: recovery status", ErrUnsupportedVersion)
		}
	}
	return nil
}

func (s *Store) SetLifecycle(ctx context.Context, ids []string, lifecycle string) error {
	return s.mutate(ctx, func(state *State) error {
		if !validLifecycle(lifecycle) {
			return ErrUnsupportedVersion
		}
		for _, id := range ids {
			if state.SessionStates[id].Lifecycle == Deleted {
				debugConflict("SetLifecycle.deleted", "session", id)
				return ErrMutationConflict
			}
			if _, ok := sessionOwner(*state, id); !ok {
				return ErrSessionNotFound
			}
			if err := validateLifecycleSupersedesPreparedPurge(*state, id); err != nil {
				return err
			}
		}
		for _, id := range ids {
			cancelPreparedPurge(state, id)
			setLifecycle(state, id, lifecycle)
			if lifecycle == Active {
				owner, _ := sessionOwner(*state, id)
				workspace := state.Workspaces[owner]
				workspace.Visible = true
				state.Workspaces[owner] = workspace
			}
		}
		return nil
	})
}

func (s *Store) BeginOperation(ctx context.Context, op Operation) error {
	return s.mutate(ctx, func(state *State) error {
		for _, id := range op.SessionIDs {
			if state.SessionStates[id].Lifecycle == Deleted {
				return ErrMutationConflict
			}
		}
		if op.ID == "" || !validLifecycle(op.Lifecycle) {
			return ErrMutationConflict
		}
		if old, ok := state.PendingOperations[op.ID]; ok {
			if old.Kind != op.Kind || old.RecoveryEntryID != op.RecoveryEntryID || old.Lifecycle != op.Lifecycle || old.WorkspaceID != op.WorkspaceID || !slices.Equal(old.Dependencies, op.Dependencies) || (len(op.SessionIDs) != 0 && !slices.Equal(old.SessionIDs, op.SessionIDs)) {
				return ErrMutationConflict
			}
			return nil
		}
		if op.ExpectedGeneration != 0 && op.ExpectedGeneration != state.Generation {
			debugConflict("BeginOperation.generation", "op", op.ID, "expected", op.ExpectedGeneration, "actual", state.Generation)
			return ErrMutationConflict
		}
		if err := validateArchiveImportReservation(state, op); err != nil {
			return err
		}
		// Bind child admission to the original command's observed state,
		// not to a newer snapshot taken after content/ownership validation.
		if split := strings.LastIndex(op.ID, "-"); split > 0 {
			parent := state.PendingOperations[op.ID[:split]]
			for _, id := range op.SessionIDs {
				if parent.Kind == "command" && state.SessionStates[id].Generation > parent.ExpectedGeneration {
					debugConflict("BeginOperation.parentGeneration", "op", op.ID, "session", id,
						"sessionGen", state.SessionStates[id].Generation, "parentExpected", parent.ExpectedGeneration)
					return ErrMutationConflict
				}
			}
		}
		op.ExpectedGeneration = state.Generation
		op.Phase = "prepared"
		if op.SessionIDs == nil {
			op.SessionIDs = []string{}
		}
		state.PendingOperations[op.ID] = op
		return nil
	})
}

func (s *Store) ReserveOperationTargets(ctx context.Context, id string, ids []string, sources ...*SourceMapping) error {
	return s.mutate(ctx, func(state *State) error {
		op, ok := state.PendingOperations[id]
		if !ok || len(ids) == 0 {
			return ErrMutationConflict
		}
		if len(op.SessionIDs) != 0 && !slices.Equal(op.SessionIDs, ids) {
			return ErrMutationConflict
		}
		if len(sources) > 0 && sources[0] != nil {
			mapping := sources[0]
			if op.Mapping != nil && (op.Mapping.SourceKey != mapping.SourceKey || op.Mapping.Fingerprint != mapping.Fingerprint || op.Mapping.SessionID != mapping.SessionID) {
				return ErrMutationConflict
			}
			if op.Phase == "prepared" {
				op.Mapping = mapping
			}
		}
		op.SessionIDs = append([]string{}, ids...)
		state.PendingOperations[id] = op
		return nil
	})
}

func (s *Store) PrepareOperationContent(ctx context.Context, id string, ids []string, mapping *SourceMapping, presentation *Presentation) error {
	return s.mutate(ctx, func(state *State) error {
		op, ok := state.PendingOperations[id]
		if !ok {
			return ErrMutationConflict
		}
		if op.Phase != "prepared" {
			if !slices.Equal(op.SessionIDs, ids) || !reflect.DeepEqual(op.Mapping, mapping) || !reflect.DeepEqual(op.Presentation, presentation) {
				return ErrMutationConflict
			}
			return nil
		}
		if len(op.SessionIDs) != 0 && !slices.Equal(op.SessionIDs, ids) {
			return ErrMutationConflict
		}
		op.Phase, op.SessionIDs, op.Mapping, op.Presentation = "content_ready", append([]string{}, ids...), mapping, presentation
		state.PendingOperations[id] = op
		return nil
	})
}

// CommitOperation publishes membership, lifecycle and provenance in one durable
// registry replacement. File publication must have been validated beforehand.
func (s *Store) CommitOperation(ctx context.Context, id string) error {
	return s.mutate(ctx, func(state *State) error {
		if op, ok := state.PendingOperations[id]; ok && op.Kind == "archive-import" {
			debugConflict("CommitOperation.archive-import", "op", id, "phase", op.Phase)
			return ErrMutationConflict
		}
		if err := validateTopicRemovalArchive(*state, state.PendingOperations[id]); err != nil {
			return err
		}
		return commitOperation(state, id, map[string]bool{})
	})
}

// CommitHistoricalArchive publishes a standalone legacy trash import without
// admitting arbitrary archive-import children intended for an atomic batch.
func (s *Store) CommitHistoricalArchive(ctx context.Context, id string, archivedAt ...int64) error {
	return s.mutate(ctx, func(state *State) error {
		op, ok := state.PendingOperations[id]
		if !ok || op.Kind != "archive-import" || op.Lifecycle != Archived || op.Mapping == nil {
			return ErrMutationConflict
		}
		if op.Phase == "committed" {
			return nil
		}
		if err := commitOperation(state, id, map[string]bool{}); err != nil {
			return err
		}
		// No proven archive timestamp is available for these old records.
		for _, sessionID := range op.SessionIDs {
			status := state.SessionStates[sessionID]
			status.ArchivedAt = 0
			if len(archivedAt) > 0 && archivedAt[0] > 0 {
				status.ArchivedAt = archivedAt[0]
			}
			state.SessionStates[sessionID] = status
		}
		return nil
	})
}

func commitOperation(state *State, id string, visiting map[string]bool) error {
	if visiting[id] {
		return ErrMutationConflict
	}
	visiting[id] = true
	defer delete(visiting, id)
	op, ok := state.PendingOperations[id]
	if !ok {
		debugConflict("commitOperation.missing-op", "op", id)
		return ErrMutationConflict
	}
	if op.Phase == "committed" {
		return nil
	}
	if op.Phase != "content_ready" || len(op.SessionIDs) == 0 {
		debugConflict("commitOperation.not-ready", "op", id, "phase", op.Phase, "sessions", len(op.SessionIDs))
		return ErrMutationConflict
	}
	for _, dependency := range op.Dependencies {
		child, ok := state.PendingOperations[dependency]
		if !ok || child.Kind != "archive-import" || child.Phase != "content_ready" {
			debugConflict("commitOperation.dependency", "op", id, "dep", dependency)
			return ErrMutationConflict
		}
		for _, target := range child.SessionIDs {
			if !slices.Contains(op.SessionIDs, target) {
				return ErrMutationConflict
			}
		}
		if err := commitOperation(state, dependency, visiting); err != nil {
			return err
		}
	}
	for _, sessionID := range op.SessionIDs {
		if err := validateLifecycleSupersedesPreparedPurge(*state, sessionID); err != nil {
			return err
		}
		if state.SessionStates[sessionID].Lifecycle == Deleted {
			debugConflict("commitOperation.session-deleted", "op", id, "session", sessionID)
			return ErrMutationConflict
		}
		if current, exists := state.SessionStates[sessionID]; exists && current.Generation > op.ExpectedGeneration && current.Generation != state.Generation+1 {
			debugConflict("commitOperation.generation", "op", id, "session", sessionID,
				"sessionGen", current.Generation, "expected", op.ExpectedGeneration, "stateGen", state.Generation)
			return ErrMutationConflict
		}
		owner, attached := sessionOwner(*state, sessionID)
		if attached && op.WorkspaceID != "" && owner != op.WorkspaceID {
			return ErrMutationConflict
		}
		if !attached {
			workspace, exists := state.Workspaces[op.WorkspaceID]
			if !exists {
				return ErrWorkspaceNotFound
			}
			workspace.SessionIDs = insertBefore(workspace.SessionIDs, sessionID, "")
			// Source adoption replaces the imported source slot below. Publishing
			// a canonical default first would incorrectly override that choice.
			if op.Mapping == nil {
				attachOrganizationSession(&workspace, sessionID, "")
				mirrorOrganizationOrder(&workspace)
			}
			workspace.UpdatedAt = time.Now().UTC()
			state.Workspaces[workspace.ID] = workspace
			owner = workspace.ID
		}
		cancelPreparedPurge(state, sessionID)
		setLifecycle(state, sessionID, op.Lifecycle)
		if op.Lifecycle == Active {
			workspace := state.Workspaces[owner]
			workspace.Visible = true
			state.Workspaces[owner] = workspace
		}
		if op.Presentation != nil {
			state.Presentation[sessionID] = *op.Presentation
		}
	}
	if err := commitSourceMapping(state, op.Mapping); err != nil {
		return err
	}
	if op.RecoveryEntryID != "" {
		entry, exists := state.RecoveryEntries[op.RecoveryEntryID]
		if !exists {
			return ErrMutationConflict
		}
		entry.Status, entry.SessionID = "restored", op.SessionIDs[0]
		state.RecoveryEntries[entry.ID] = entry
	}
	op.Phase, op.ResultGeneration = "committed", state.Generation+1
	state.PendingOperations[id] = op
	return nil
}

func ClassifyPurge(state State, id string) PurgeState {
	key := "purge-" + id
	op, exists := state.PendingOperations[key]
	if !exists {
		return PurgeAbsent
	}
	status, known := state.SessionStates[id]
	if !known || op.ID != key || op.Kind != "purge" || op.Lifecycle != Deleted || len(op.SessionIDs) != 1 || op.SessionIDs[0] != id {
		return PurgeInvalid
	}
	switch op.Phase {
	case "prepared":
		if status.Lifecycle == Deleted {
			return PurgeInvalid
		}
		if status.Lifecycle != Archived || status.Generation > op.ExpectedGeneration {
			return PurgePreparedStale
		}
		return PurgePrepared
	case "tombstoned":
		if status.Lifecycle == Deleted {
			return PurgeTombstoned
		}
	case "content_removed":
		if status.Lifecycle == Deleted {
			return PurgeContentRemoved
		}
	case "committed":
		if status.Lifecycle == Deleted {
			return PurgeCommitted
		}
	}
	return PurgeInvalid
}

func validateLifecycleSupersedesPreparedPurge(state State, id string) error {
	switch ClassifyPurge(state, id) {
	case PurgeAbsent, PurgePrepared, PurgePreparedStale:
		return nil
	default:
		return ErrMutationConflict
	}
}

func cancelPreparedPurge(state *State, id string) {
	key := "purge-" + id
	if op, exists := state.PendingOperations[key]; exists && op.Kind == "purge" && op.Phase == "prepared" {
		delete(state.PendingOperations, key)
	}
}

func samePurgeIdentity(left, right Operation) bool {
	return left.ID == right.ID && left.Kind == "purge" && right.Kind == "purge" && left.Lifecycle == right.Lifecycle &&
		left.ExpectedGeneration == right.ExpectedGeneration && slices.Equal(left.SessionIDs, right.SessionIDs) && bytes.Equal(left.Request, right.Request)
}

// BeginPurge atomically validates the archived generation, publishes the
// deletion tombstone and records the resumable purge operation.
func (s *Store) BeginPurge(ctx context.Context, id string, expected uint64) error {
	return s.beginOrResumePurge(ctx, id, expected, nil, false)
}

// ResumePurge continues only the observed operation. It cannot recreate a
// deletion intent after a restore superseded that operation.
func (s *Store) ResumePurge(ctx context.Context, id string, observed Operation) error {
	return s.beginOrResumePurge(ctx, id, observed.ExpectedGeneration, &observed, false)
}

// ResumePurgeForRequest keeps both the request snapshot and the observed
// transaction identity. Neither may be refreshed while waiting for locks.
func (s *Store) ResumePurgeForRequest(ctx context.Context, id string, expected uint64, observed Operation) error {
	return s.beginOrResumePurge(ctx, id, expected, &observed, false)
}

func (s *Store) beginOrResumePurge(ctx context.Context, id string, expected uint64, observed *Operation, cleanupSources bool) error {
	stale := false
	err := s.mutate(ctx, func(state *State) error {
		key := "purge-" + id
		current, exists := state.PendingOperations[key]
		if observed != nil && (!exists || !samePurgeIdentity(current, *observed)) {
			return fmt.Errorf("%w: observed purge was removed or replaced", ErrMutationConflict)
		}
		switch ClassifyPurge(*state, id) {
		case PurgePreparedStale:
			delete(state.PendingOperations, key)
			stale = true
			return nil
		case PurgePrepared:
			if observed == nil {
				return ErrMutationConflict
			}
			status := state.SessionStates[id]
			if status.Generation > expected {
				return fmt.Errorf("%w: purge generation %d exceeds request %d", ErrMutationConflict, status.Generation, expected)
			}
			setLifecycle(state, id, Deleted)
			current.Phase = "tombstoned"
			state.PendingOperations[key] = current
			return nil
		case PurgeTombstoned, PurgeContentRemoved, PurgeCommitted:
			return nil
		case PurgeInvalid:
			return fmt.Errorf("%w: inconsistent purge state", ErrMutationConflict)
		case PurgeAbsent:
			if observed != nil {
				return ErrMutationConflict
			}
			status, known := state.SessionStates[id]
			if !known || status.Lifecycle != Archived || status.Generation > expected {
				return ErrMutationConflict
			}
			op := Operation{ID: key, Kind: "purge", Phase: "tombstoned", Lifecycle: Deleted, SessionIDs: []string{id}, ExpectedGeneration: status.Generation}
			if cleanupSources {
				var err error
				op.Request, err = purgeSourceCleanupRequest(*state, id)
				if err != nil {
					return err
				}
			}
			state.PendingOperations[key] = op
			setLifecycle(state, id, Deleted)
			return nil
		default:
			return ErrMutationConflict
		}
	})
	if err != nil {
		return err
	}
	if stale {
		return fmt.Errorf("%w: stale purge preparation removed", ErrMutationConflict)
	}
	return nil
}

func (s *Store) AdvancePurge(ctx context.Context, id, phase string) error {
	return s.mutate(ctx, func(state *State) error {
		key := "purge-" + id
		op, ok := state.PendingOperations[key]
		if !ok || op.Kind != "purge" {
			return ErrMutationConflict
		}
		if ClassifyPurge(*state, id) == PurgeInvalid {
			return ErrMutationConflict
		}
		if op.Phase == "committed" || op.Phase == phase || op.Phase == "content_removed" {
			return nil
		}
		if phase != "content_removed" || op.Phase != "tombstoned" {
			return ErrMutationConflict
		}
		op.Phase = phase
		state.PendingOperations[key] = op
		return nil
	})
}

func (s *Store) CompletePurge(ctx context.Context, id string) error {
	return s.mutate(ctx, func(state *State) error {
		key := "purge-" + id
		op := state.PendingOperations[key]
		switch ClassifyPurge(*state, id) {
		case PurgeCommitted:
			return nil
		case PurgeContentRemoved:
		default:
			return ErrMutationConflict
		}
		// Keep only the consumed topic identity in the existing purge receipt.
		// Canonical sessions and runtime-adopted sources may have no import
		// journal from which a future display reader could recover that identity.
		if topicID := state.Presentation[id].TopicID; topicID != "" {
			op.WorkspaceID, _ = sessionOwner(*state, id)
			if op.Presentation == nil {
				op.Presentation = &Presentation{TopicID: topicID}
			}
		}
		for key, workspace := range state.Workspaces {
			if slices.Contains(workspace.SessionIDs, id) {
				op.WorkspaceID = workspace.ID
			}
			workspace.SessionIDs = remove(workspace.SessionIDs, id)
			state.Workspaces[key] = workspace
		}
		// The content and presentation can go, but their topic ownership must
		// survive: otherwise a residual metadata row looks like a new topic.
		if topicID := state.Presentation[id].TopicID; topicID != "" {
			if op.Presentation == nil {
				op.Presentation = &Presentation{}
			}
			op.Presentation.TopicID = topicID
		}
		delete(state.Presentation, id)
		op.Phase, op.ResultGeneration = "committed", state.Generation+1
		state.PendingOperations[key] = op
		return nil
	})
}

func (s *Store) UpdatePresentation(ctx context.Context, ids []string, title *string, pinned *bool) error {
	return s.mutate(ctx, func(state *State) error {
		for _, id := range ids {
			if _, ok := sessionOwner(*state, id); !ok {
				return ErrSessionNotFound
			}
			value, exists := state.Presentation[id]
			if !exists {
				value.SortOrder = -1
			}
			if title != nil {
				value.Title = *title
			}
			if pinned != nil {
				value.Pinned = *pinned
			}
			state.Presentation[id] = value
		}
		return nil
	})
}

// EnsureSessionTopic publishes the initial display group without letting
// later tab rebuilds overwrite a user's persisted presentation.
func (s *Store) EnsureSessionTopic(ctx context.Context, id, topicID, title string) error {
	if id == "" || topicID == "" {
		return nil
	}
	return s.mutate(ctx, func(state *State) error {
		if _, ok := sessionOwner(*state, id); !ok {
			return ErrSessionNotFound
		}
		value := state.Presentation[id]
		if value.TopicID != "" {
			return nil
		}
		value.TopicID = topicID
		if value.Title == "" {
			value.Title = title
		}
		state.Presentation[id] = value
		return nil
	})
}

func (s *Store) RecordRecovery(ctx context.Context, entry RecoveryEntry) error {
	return s.mutate(ctx, func(state *State) error {
		if old, ok := state.RecoveryEntries[entry.ID]; ok {
			if old.Status == "restored" && old.Fingerprint == entry.Fingerprint {
				return nil
			}
			if strings.Contains(old.Reason, "conflict") && !strings.Contains(entry.Reason, "conflict") {
				entry.Reason = old.Reason
			}
			entry.extra = old.extra
		}
		if entry.Status == "" {
			entry.Status = "pending"
		}
		state.RecoveryEntries[entry.ID] = entry
		return nil
	})
}

// ReconcileDiscoveredSession rechecks ownership under the writer lock. A scan
// snapshot can predate a concurrent create, import, archive, or restore.
// Discovery must never classify those published/reserved IDs as orphans.
func (s *Store) ReconcileDiscoveredSession(ctx context.Context, entry RecoveryEntry, workspace *Workspace) error {
	return s.mutate(ctx, func(state *State) error {
		id := entry.SessionID
		if state.SessionStates[id].Lifecycle == Deleted {
			return nil
		}
		if id == "" {
			return errors.New("discovery requires a session id")
		}
		if _, ok := sessionOwner(*state, id); ok {
			return nil
		}
		if _, ok := state.PendingCreates[id]; ok {
			return nil
		}
		for _, op := range state.PendingOperations {
			if op.Phase != "committed" && slices.Contains(op.SessionIDs, id) {
				return nil
			}
		}
		if lifecycle, ok := state.SessionStates[id]; ok && lifecycle.Lifecycle != Active {
			workspace = nil
			entry.Reason = "historical_state_unknown"
		}
		if workspace == nil {
			if old, ok := state.RecoveryEntries[entry.ID]; ok {
				if old.Status == "restored" {
					return nil
				}
				entry.extra = old.extra
			}
			state.RecoveryEntries[entry.ID] = entry
			return nil
		}
		// A discovery snapshot can predate another process registering this
		// directory. Resolve its owner under the writer lock, just like session
		// ownership above, and preserve the owner's title and visibility.
		workspaceID, found, err := ResolveWorkspaceID(*state, workspace.Root)
		if err != nil {
			return err
		}
		if !found {
			workspaceID = workspace.ID
			if existing, exists := state.Workspaces[workspaceID]; exists && existing.Root != workspace.Root {
				return ErrMutationConflict
			}
		}
		value, ok := state.Workspaces[workspaceID]
		if !ok {
			value = *workspace
			state.WorkspaceIDs = append(state.WorkspaceIDs, value.ID)
		}
		value.SessionIDs = append(value.SessionIDs, id)
		value.UpdatedAt = time.Now().UTC()
		state.Workspaces[value.ID] = value
		return nil
	})
}

// backupV1 runs under the same cross-process lock as the schema publication.
// Content-addressed backups are immutable: a repeat never overwrites evidence.
func backupV1(path string) error {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(body, &header); err != nil {
		return err
	}
	if header.Version != 1 {
		return nil
	}
	digest := sha256.Sum256(body)
	backup := filepath.Join(filepath.Dir(path), "upgrade-backups", "workspace-v1-"+hex.EncodeToString(digest[:])+".json")
	if old, err := os.ReadFile(backup); err == nil {
		if sha256.Sum256(old) != digest {
			return errors.New("workspace upgrade backup is corrupt")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(backup, body, 0600)
}

// unknownFields retains future, user-owned metadata during read/modify/write.
func unknownFields(body []byte, names ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	for _, name := range names {
		delete(fields, name)
	}
	return fields, nil
}

func commitSourceMapping(state *State, mapping *SourceMapping) error {
	if mapping != nil {
		mapping := *mapping
		if old, exists := state.SourceMappings[mapping.SourceKey]; exists && (old.SessionID != mapping.SessionID || old.Fingerprint != mapping.Fingerprint) {
			return ErrMutationConflict
		}
		state.SourceMappings[mapping.SourceKey] = mapping
		adoptOrganizationSource(state, mapping)
	}
	return nil
}
