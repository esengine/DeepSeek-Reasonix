// Package taskmonitor defines the unified Task Monitor domain model and
// read-only query interfaces for observing background tasks. It provides
// TaskSnapshot, TaskEvent, TaskState, RuntimeState and a Store abstraction.
//
// The package does not read private session files, does not parse internal
// Reasonix state files, and does not implement a second state machine — it
// is a pure observation layer that reuses the existing jobs.Manager as its
// source of truth.
package taskmonitor

import (
	"encoding/json"
	"fmt"
	"time"
)

// TaskState enumerates the observable lifecycle states of a background task.
type TaskState string

// RuntimeState reports whether a task still has live execution behind its
// persisted lifecycle state. It is intentionally independent from TaskState:
// for example, a requeued task is queued but its previous runtime has exited.
// The empty value is accepted for snapshots written before this field existed
// and is interpreted as unknown.
type RuntimeState string

const (
	TaskStateQueued    TaskState = "queued"
	TaskStateRunning   TaskState = "running"
	TaskStateWaiting   TaskState = "waiting"
	TaskStateSucceeded TaskState = "succeeded"
	TaskStateFailed    TaskState = "failed"
	TaskStateCancelled TaskState = "cancelled"
	TaskStateStale     TaskState = "stale"

	RuntimeStateUnknown RuntimeState = "unknown"
	RuntimeStateAlive   RuntimeState = "alive"
	RuntimeStateExited  RuntimeState = "exited"

	// maxFieldLen is the maximum byte length for free-form string fields
	// (TaskID, SessionID, ErrorCode, EventType).  It prevents memory-
	// exhaustion attacks from unbounded JSON input.
	maxFieldLen = 256

	// maxErrorSummaryLen is the maximum byte length for ErrorSummary.
	maxErrorSummaryLen = 1024
)

// Effective returns unknown for legacy snapshots and events that predate the
// runtime_state field.
func (s RuntimeState) Effective() RuntimeState {
	if s == "" {
		return RuntimeStateUnknown
	}
	return s
}

// IsKnown reports whether s is one of the well-known runtime states. The empty
// legacy value is treated as the known unknown state.
func (s RuntimeState) IsKnown() bool {
	switch s.Effective() {
	case RuntimeStateUnknown, RuntimeStateAlive, RuntimeStateExited:
		return true
	default:
		return false
	}
}

// reconcileRuntime marks an alive snapshot stale when its owner lease has
// expired. It is deliberately pure; callers decide whether to persist the
// reconciled value.
func reconcileRuntime(snap *TaskSnapshot, now time.Time) {
	if snap == nil || snap.RuntimeState.Effective() != RuntimeStateAlive || snap.RuntimeLeaseUntil.IsZero() {
		return
	}
	if now.Before(snap.RuntimeLeaseUntil) {
		return
	}
	snap.RuntimeState = RuntimeStateExited
	if !snap.State.Terminal() {
		snap.State = TaskStateStale
	}
}

// ValidTaskStates is the set of well-known states.
var ValidTaskStates = map[TaskState]bool{
	TaskStateQueued:    true,
	TaskStateRunning:   true,
	TaskStateWaiting:   true,
	TaskStateSucceeded: true,
	TaskStateFailed:    true,
	TaskStateCancelled: true,
	TaskStateStale:     true,
}

// IsKnown reports whether s is one of the well-known states.
func (s TaskState) IsKnown() bool { return ValidTaskStates[s] }

// Terminal reports whether s is a terminal state.
func (s TaskState) Terminal() bool {
	switch s {
	case TaskStateSucceeded, TaskStateFailed, TaskStateCancelled, TaskStateStale:
		return true
	default:
		return false
	}
}

// ValidTransition reports whether moving from current to next is legitimate.
func (s TaskState) ValidTransition(next TaskState) bool {
	if s == next {
		return false
	}
	// Terminal states cannot transition to anything, not even unknown states.
	if s.Terminal() {
		return false
	}
	// Unknown next states are allowed (forward-compat) provided current is
	// not terminal (guarded above).
	if !next.IsKnown() {
		return true
	}
	switch s {
	case TaskStateQueued:
		return next == TaskStateRunning || next == TaskStateCancelled ||
			next == TaskStateStale
	case TaskStateRunning:
		return next == TaskStateWaiting || next == TaskStateSucceeded ||
			next == TaskStateFailed || next == TaskStateCancelled ||
			next == TaskStateStale
	case TaskStateWaiting:
		return next == TaskStateRunning || next == TaskStateSucceeded ||
			next == TaskStateFailed || next == TaskStateCancelled ||
			next == TaskStateStale
	case TaskStateSucceeded, TaskStateFailed, TaskStateCancelled, TaskStateStale:
		return false
	default:
		return true // forward-compat
	}
}

// UnmarshalJSON preserves unknown state values as-is.
func (s *TaskState) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = TaskState(v)
	return nil
}

// TaskSnapshot is a sanitised snapshot of a single task.  It intentionally
// omits prompt text, tool arguments, tool results, and reasoning traces.
type TaskSnapshot struct {
	SchemaVersion int    `json:"schema_version"`
	TaskID        string `json:"task_id"`
	// JobID is the jobs.Manager-local runtime identifier. TaskID is the
	// project-wide monitor identity and may be namespaced by session, so runtime
	// control must not pass TaskID directly to jobs.Manager.
	JobID string `json:"job_id,omitempty"`
	// SessionID is the session the task was created in; it may be empty when
	// the recorder attached before a session path was resolved.
	SessionID         string       `json:"session_id"`
	State             TaskState    `json:"state"`
	RuntimeState      RuntimeState `json:"runtime_state,omitempty"`
	RuntimeLeaseUntil time.Time    `json:"runtime_lease_until,omitempty"`
	// RuntimeOwnerID identifies the recorder generation that owns the live
	// runtime lease. It prevents a delayed heartbeat from an older controller
	// from renewing a newer lifecycle that reused the same session/job IDs.
	RuntimeOwnerID string    `json:"runtime_owner_id,omitempty"`
	Version        uint64    `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ErrorCode      string    `json:"error_code,omitempty"`
	ErrorSummary   string    `json:"error_summary,omitempty"`
}

// Validate returns a non-nil error if required fields are missing or
// inconsistent, or if any free-form field exceeds its length limit.
func (ts TaskSnapshot) Validate() error {
	if ts.TaskID == "" {
		return fmt.Errorf("TaskSnapshot.TaskID is required")
	}
	if ts.State == "" {
		return fmt.Errorf("TaskSnapshot.State is required")
	}
	if ts.CreatedAt.IsZero() {
		return fmt.Errorf("TaskSnapshot.CreatedAt is required")
	}
	if ts.UpdatedAt.IsZero() {
		return fmt.Errorf("TaskSnapshot.UpdatedAt is required")
	}
	if ts.UpdatedAt.Before(ts.CreatedAt) {
		return fmt.Errorf("TaskSnapshot.UpdatedAt (%v) is before CreatedAt (%v)",
			ts.UpdatedAt, ts.CreatedAt)
	}
	if ts.SchemaVersion <= 0 {
		return fmt.Errorf("TaskSnapshot.SchemaVersion must be positive, got %d",
			ts.SchemaVersion)
	}
	if len(ts.TaskID) > maxFieldLen {
		return fmt.Errorf("TaskSnapshot.TaskID exceeds max length %d", maxFieldLen)
	}
	if len(ts.JobID) > maxFieldLen {
		return fmt.Errorf("TaskSnapshot.JobID exceeds max length %d", maxFieldLen)
	}
	if len(ts.SessionID) > maxFieldLen {
		return fmt.Errorf("TaskSnapshot.SessionID exceeds max length %d", maxFieldLen)
	}
	if len(ts.ErrorCode) > maxFieldLen {
		return fmt.Errorf("TaskSnapshot.ErrorCode exceeds max length %d", maxFieldLen)
	}
	if len(ts.RuntimeState) > maxFieldLen {
		return fmt.Errorf("TaskSnapshot.RuntimeState exceeds max length %d", maxFieldLen)
	}
	if len(ts.RuntimeOwnerID) > maxFieldLen {
		return fmt.Errorf("TaskSnapshot.RuntimeOwnerID exceeds max length %d", maxFieldLen)
	}
	if !ts.RuntimeLeaseUntil.IsZero() && ts.RuntimeLeaseUntil.Before(ts.CreatedAt) {
		return fmt.Errorf("TaskSnapshot.RuntimeLeaseUntil is before CreatedAt")
	}
	if len(ts.ErrorSummary) > maxErrorSummaryLen {
		return fmt.Errorf("TaskSnapshot.ErrorSummary exceeds max length %d",
			maxErrorSummaryLen)
	}
	return nil
}

// TaskEvent is a single sanitised event in a task's lifecycle.
type TaskEvent struct {
	Sequence     int          `json:"sequence"`
	Timestamp    time.Time    `json:"timestamp"`
	EventType    string       `json:"event_type"`
	TaskID       string       `json:"task_id"`
	SessionID    string       `json:"session_id"`
	State        TaskState    `json:"state"`
	RuntimeState RuntimeState `json:"runtime_state,omitempty"`
	ErrorCode    string       `json:"error_code,omitempty"`
	ErrorSummary string       `json:"error_summary,omitempty"`
}

// Validate returns a non-nil error on required-field violations.
func (te TaskEvent) Validate() error {
	if te.Sequence <= 0 {
		return fmt.Errorf("TaskEvent.Sequence must be positive, got %d", te.Sequence)
	}
	if te.TaskID == "" {
		return fmt.Errorf("TaskEvent.TaskID is required")
	}
	if te.State == "" {
		return fmt.Errorf("TaskEvent.State is required")
	}
	if te.EventType == "" {
		return fmt.Errorf("TaskEvent.EventType is required")
	}
	if te.Timestamp.IsZero() {
		return fmt.Errorf("TaskEvent.Timestamp is required")
	}
	if len(te.TaskID) > maxFieldLen {
		return fmt.Errorf("TaskEvent.TaskID exceeds max length %d", maxFieldLen)
	}
	if len(te.SessionID) > maxFieldLen {
		return fmt.Errorf("TaskEvent.SessionID exceeds max length %d", maxFieldLen)
	}
	if len(te.EventType) > maxFieldLen {
		return fmt.Errorf("TaskEvent.EventType exceeds max length %d", maxFieldLen)
	}
	if len(te.ErrorCode) > maxFieldLen {
		return fmt.Errorf("TaskEvent.ErrorCode exceeds max length %d", maxFieldLen)
	}
	if len(te.RuntimeState) > maxFieldLen {
		return fmt.Errorf("TaskEvent.RuntimeState exceeds max length %d", maxFieldLen)
	}
	if len(te.ErrorSummary) > maxErrorSummaryLen {
		return fmt.Errorf("TaskEvent.ErrorSummary exceeds max length %d",
			maxErrorSummaryLen)
	}
	return nil
}
