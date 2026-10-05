package taskmonitor

import (
	"context"
	"errors"
	"time"
)

// ErrStoreVersionConflict reports that a snapshot CAS lost to another writer.
// Callers may re-read and retry a derived update, or return a stable client
// conflict without parsing implementation-specific error text.
var ErrStoreVersionConflict = errors.New("task store version conflict")

// Store is the read-only query surface for task monitoring.
type Store interface {
	ListTasks(ctx context.Context, projectDir string) ([]TaskSnapshot, error)
	GetTask(ctx context.Context, projectDir string, taskID string) (*TaskSnapshot, error)
	ListEvents(ctx context.Context, projectDir string, taskID string, afterSequence int) ([]TaskEvent, error)
}

// ProjectionSink receives post-commit hints. Implementations must enqueue and
// return immediately; FileStore remains the only authority for task control.
type ProjectionSink interface {
	SnapshotChanged(projectRoot, taskID string)
	EventsChanged(projectRoot, taskID string)
}

// IdempotencyRecord captures the binding between an idempotency key and the
// operation it was used for.
type IdempotencyRecord struct {
	Key       string    `json:"key"`
	Op        string    `json:"op"`
	TaskID    string    `json:"task_id"`
	Version   uint64    `json:"version"`
	Pending   bool      `json:"pending,omitempty"`
	ClaimedAt time.Time `json:"claimed_at,omitempty"`
}

// IdempotencyClaimer atomically reserves a key before a control operation
// performs any side effect. Pending claims can be finalized or released.
type IdempotencyClaimer interface {
	ClaimIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) (*IdempotencyRecord, error)
	FinalizeIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) error
	ReleaseIdempotency(ctx context.Context, projectDir, key string) error
}

// WriteStore extends Store with atomic write operations for control
// commands, persistent idempotency, and event sequencing.
//
// Transaction ordering for control operations:
//  1. ClaimIdempotency — reserve the key before runtime/state side effects
//  2. SaveTask         — persist state with version CAS
//  3. AppendAuditEvent — atomically assign sequence + write event
//  4. FinalizeIdempotency — mark the claim complete
//
// Steps 2-3 failures after a successful SaveTask leave the task in the new
// state with a potentially incomplete audit log. This is acceptable for a
// file-based store; a transactional store would provide stronger guarantees.
type WriteStore interface {
	Store

	// SaveTask atomically persists snap with version-based CAS.
	SaveTask(ctx context.Context, projectDir string, snap TaskSnapshot) error

	// RenewRuntimeLease extends an alive task lease only when ownerID still
	// owns the persisted runtime generation. Implementations must read the raw
	// stored snapshot rather than a liveness-reconciled observation.
	RenewRuntimeLease(ctx context.Context, projectDir, taskID, ownerID string, leaseUntil time.Time) (bool, error)

	// AppendAuditEvent atomically assigns the next monotonic sequence
	// number and appends the event to taskID's event log. Implementations
	// must be safe for concurrent use across processes.
	AppendAuditEvent(ctx context.Context, projectDir string, ev TaskEvent) error

	// CheckIdempotency returns the recorded key if it exists, or nil.
	CheckIdempotency(ctx context.Context, projectDir string, key string) (*IdempotencyRecord, error)

	// RecordIdempotency atomically claims key for r. If key already exists
	// with identical parameters, it is a no-op. If key exists with different
	// parameters, it must return an error. Implementations must be safe
	// across process restarts.
	RecordIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) error
}
