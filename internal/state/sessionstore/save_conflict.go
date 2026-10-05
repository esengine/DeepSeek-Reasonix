// Snapshot conflicts: what a refused save reports about the two transcripts.
package sessionstore

import (
	"errors"
	"fmt"

	"reasonix/internal/contract/provider"
)

type SessionSnapshotConflictKind string

const (
	SessionSnapshotConflictStalePrefix SessionSnapshotConflictKind = "stale_prefix"
	SessionSnapshotConflictDiverged    SessionSnapshotConflictKind = "diverged"
)

type SessionSnapshotConflictError struct {
	Path             string
	Kind             SessionSnapshotConflictKind
	ExistingMessages int
	SnapshotMessages int
	BaseRevision     int64
	DiskRevision     int64
	// DivergedAt is the first index the two disagree on with the roles standing
	// there, -1 when one is a prefix of the other. A position and a role tell a
	// local reshape from a foreign write; content never leaves in a diagnostic.
	DivergedAt   int
	DiskRole     string
	SnapshotRole string
}

func (e *SessionSnapshotConflictError) Error() string {
	if e == nil {
		return ErrSessionSnapshotConflict.Error()
	}
	switch e.Kind {
	case SessionSnapshotConflictStalePrefix:
		return fmt.Sprintf("%s: %s has %d messages at revision %d; stale snapshot has %d messages from revision %d",
			ErrSessionSnapshotConflict, e.Path, e.ExistingMessages, e.DiskRevision, e.SnapshotMessages, e.BaseRevision)
	default:
		return fmt.Sprintf("%s: %s diverged on disk (%d messages, revision %d) from snapshot (%d messages, revision %d)",
			ErrSessionSnapshotConflict, e.Path, e.ExistingMessages, e.DiskRevision, e.SnapshotMessages, e.BaseRevision)
	}
}

func (e *SessionSnapshotConflictError) Unwrap() error {
	return ErrSessionSnapshotConflict
}

func SnapshotConflictKind(err error) (SessionSnapshotConflictKind, bool) {
	var conflict *SessionSnapshotConflictError
	if errors.As(err, &conflict) && conflict != nil {
		return conflict.Kind, true
	}
	return "", false
}

func snapshotConflict(path string, existing, next []provider.Message, baseRevision, diskRevision int64) error {
	kind := SessionSnapshotConflictDiverged
	if messagesHavePrefix(existing, next) || messagesHavePrefixWithCompatibleSystem(existing, next) {
		kind = SessionSnapshotConflictStalePrefix
	}
	return snapshotConflictOfKind(path, kind, existing, next, baseRevision, diskRevision)
}

func snapshotConflictOfKind(path string, kind SessionSnapshotConflictKind,
	existing, next []provider.Message, baseRevision, diskRevision int64,
) *SessionSnapshotConflictError {
	at, diskRole, snapshotRole := firstStorageDivergence(existing, next)
	return &SessionSnapshotConflictError{
		Path:             path,
		Kind:             kind,
		DivergedAt:       at,
		DiskRole:         diskRole,
		SnapshotRole:     snapshotRole,
		ExistingMessages: len(existing),
		SnapshotMessages: len(next),
		BaseRevision:     baseRevision,
		DiskRevision:     diskRevision,
	}
}

// firstStorageDivergence reports where two transcripts stop agreeing and the
// roles standing there: the shape of a conflict without any of its content.
// -1 means neither contradicts the other and one is simply shorter.
func firstStorageDivergence(disk, snapshot []provider.Message) (int, string, string) {
	for i := range min(len(disk), len(snapshot)) {
		if messagesEqualForStorage(disk[i], snapshot[i]) {
			continue
		}
		return i, string(disk[i].Role), string(snapshot[i].Role)
	}
	return -1, "", ""
}
