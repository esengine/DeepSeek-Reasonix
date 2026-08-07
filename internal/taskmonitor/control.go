package taskmonitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// JobKiller routes a control request to the runtime that owns a task. SessionID
// is part of the target because local job IDs are only unique within a
// controller. Stop and cancel fail closed when no owner accepts the request.
type JobKiller interface {
	Kill(sessionID, jobID string) bool
}

// runtimeJobID returns the jobs.Manager-local ID for runtime control. JobID is
// present on new snapshots. The prefix fallback keeps snapshots written by the
// short-lived namespaced-ID implementation controllable after an upgrade, while
// legacy snapshots continue to use their unnamespaced TaskID.
func runtimeJobID(snap *TaskSnapshot) string {
	if snap == nil {
		return ""
	}
	if snap.JobID != "" {
		return snap.JobID
	}
	if snap.SessionID != "" {
		prefix := monitorTaskID(snap.SessionID, "")
		if strings.HasPrefix(snap.TaskID, prefix) && len(snap.TaskID) > len(prefix) {
			return strings.TrimPrefix(snap.TaskID, prefix)
		}
	}
	return snap.TaskID
}

// ControlResult is the unified response for all task control operations.
type ControlResult struct {
	SchemaVersion int          `json:"schema_version"`
	Command       string       `json:"command"`
	TaskID        string       `json:"task_id"`
	SessionID     string       `json:"session_id"`
	State         TaskState    `json:"state"`
	RuntimeState  RuntimeState `json:"runtime_state,omitempty"`
	Version       uint64       `json:"version"`
	Accepted      bool         `json:"accepted"`
	Idempotent    bool         `json:"idempotent"`
	Error         *CtrlError   `json:"error,omitempty"`
}

// CtrlError carries a stable machine-readable code and message.
type CtrlError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	ErrTaskNotFound            = "task_not_found"
	ErrTaskScopeMismatch       = "task_scope_mismatch"
	ErrTaskVersionConflict     = "task_version_conflict"
	ErrTaskInvalidTransition   = "task_invalid_transition"
	ErrTaskNotRequeueable      = "task_not_requeueable"
	ErrTaskAlreadyTerminal     = "task_already_terminal"
	ErrTaskInProgress          = "task_operation_in_progress"
	ErrTaskPermissionDenied    = "task_permission_denied"
	ErrTaskIdempotencyConflict = "task_idempotency_conflict"
	ErrTaskAuditFailed         = "task_audit_failed"
	ErrTaskRuntimeUnavailable  = "task_runtime_unavailable"
)

// ControlService provides atomic control operations on tasks.
type ControlService struct {
	mu    sync.Mutex
	store WriteStore
}

// NewControlService returns a ControlService backed by store.
func NewControlService(store WriteStore) *ControlService {
	return &ControlService{store: store}
}

func (cs *ControlService) StopTask(ctx context.Context, projectDir, taskID string, expectedVersion uint64, reason, idemKey string) (ControlResult, error) {
	return cs.StopTaskWithKiller(ctx, projectDir, taskID, expectedVersion, reason, idemKey, nil)
}

// StopTaskWithKiller binds the live runtime target to this control operation.
// Keeping the killer call-scoped prevents concurrent clients from overwriting a
// shared killer and cancelling a same-named job in another session.
func (cs *ControlService) StopTaskWithKiller(ctx context.Context, projectDir, taskID string, expectedVersion uint64, reason, idemKey string, killer JobKiller) (ControlResult, error) {
	return cs.controlOp(ctx, projectDir, taskID, expectedVersion, "stop", TaskStateCancelled, reason, idemKey, killer)
}

func (cs *ControlService) CancelTask(ctx context.Context, projectDir, taskID string, expectedVersion uint64, reason, idemKey string) (ControlResult, error) {
	return cs.CancelTaskWithKiller(ctx, projectDir, taskID, expectedVersion, reason, idemKey, nil)
}

// CancelTaskWithKiller is the call-scoped-killer form of CancelTask.
func (cs *ControlService) CancelTaskWithKiller(ctx context.Context, projectDir, taskID string, expectedVersion uint64, reason, idemKey string, killer JobKiller) (ControlResult, error) {
	return cs.controlOp(ctx, projectDir, taskID, expectedVersion, "cancel", TaskStateCancelled, reason, idemKey, killer)
}

// RequeueTask moves a failed or stale task back to queued. It does not start a
// new runtime; RuntimeState therefore remains exited (or unknown for legacy
// data) until a scheduler starts the task and records a new lifecycle.
func (cs *ControlService) RequeueTask(ctx context.Context, projectDir, taskID string, expectedVersion uint64, idemKey string) (ControlResult, error) {
	return cs.controlOp(ctx, projectDir, taskID, expectedVersion, "requeue", TaskStateQueued, "", idemKey, nil)
}

func (cs *ControlService) OpenTaskSession(ctx context.Context, projectDir, taskID string) (ControlResult, error) {
	snap, err := cs.store.GetTask(ctx, projectDir, taskID)
	if err != nil {
		return ControlResult{}, err
	}
	if snap == nil {
		return ControlResult{
			SchemaVersion: 1, Command: "open_session", TaskID: taskID,
			Error: &CtrlError{Code: ErrTaskNotFound, Message: "task not found"},
		}, nil
	}
	return ControlResult{
		SchemaVersion: 1, Command: "open_session",
		TaskID: snap.TaskID, SessionID: snap.SessionID,
		State: snap.State, RuntimeState: snap.RuntimeState,
		Version: snap.Version, Accepted: true,
	}, nil
}

func (cs *ControlService) controlOp(ctx context.Context, projectDir, taskID string, expectedVersion uint64, cmd string, targetState TaskState, reason, idemKey string, killer JobKiller) (ControlResult, error) {
	if err := ctx.Err(); err != nil {
		return ControlResult{}, err
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	var claimer IdempotencyClaimer
	claimed := false
	releaseClaim := func() {
		if claimed && claimer != nil {
			_ = claimer.ReleaseIdempotency(ctx, projectDir, idemKey)
			claimed = false
		}
	}

	// ── idempotency check (persisted) ──
	if idemKey != "" {
		var rec *IdempotencyRecord
		var err error
		if c, ok := cs.store.(IdempotencyClaimer); ok {
			claimer = c
			rec, err = claimer.ClaimIdempotency(ctx, projectDir, IdempotencyRecord{Key: idemKey, Op: cmd, TaskID: taskID, Version: expectedVersion})
			if err != nil {
				return ControlResult{}, fmt.Errorf("claim idempotency: %w", err)
			}
			claimed = rec == nil
		} else {
			rec, err = cs.store.CheckIdempotency(ctx, projectDir, idemKey)
			if err != nil {
				return ControlResult{}, fmt.Errorf("check idempotency: %w", err)
			}
		}
		if rec != nil {
			// Must match exactly
			if rec.Op != cmd || rec.TaskID != taskID || rec.Version != expectedVersion {
				return ControlResult{
					SchemaVersion: 1, Command: cmd, TaskID: taskID,
					Error: &CtrlError{Code: ErrTaskIdempotencyConflict, Message: "idempotency key reused with different parameters"},
				}, nil
			}
			if rec.Pending {
				return ControlResult{SchemaVersion: 1, Command: cmd, TaskID: taskID,
					Error: &CtrlError{Code: ErrTaskInProgress, Message: "idempotency key is in progress"}}, nil
			}
			// Replay: fetch current state
			snap, err := cs.store.GetTask(ctx, projectDir, taskID)
			if err != nil {
				return ControlResult{}, err
			}
			if snap == nil {
				return ControlResult{
					SchemaVersion: 1, Command: cmd, TaskID: taskID,
					Error: &CtrlError{Code: ErrTaskNotFound, Message: "task not found"},
				}, nil
			}
			return ControlResult{
				SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
				State: snap.State, RuntimeState: snap.RuntimeState,
				Version: snap.Version, Accepted: true, Idempotent: true,
			}, nil
		}
	}

	// ── fetch + validate ──
	snap, err := cs.store.GetTask(ctx, projectDir, taskID)
	if err != nil {
		releaseClaim()
		return ControlResult{}, err
	}
	if snap == nil {
		releaseClaim()
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID,
			Error: &CtrlError{Code: ErrTaskNotFound, Message: "task not found"},
		}, nil
	}
	if expectedVersion != snap.Version {
		releaseClaim()
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
			State: snap.State, RuntimeState: snap.RuntimeState, Version: snap.Version,
			Error: &CtrlError{Code: ErrTaskVersionConflict, Message: "version mismatch"},
		}, nil
	}
	requeue := cmd == "requeue"
	requeueable := requeue && (snap.State == TaskStateFailed || snap.State == TaskStateStale)
	if requeue && !requeueable {
		releaseClaim()
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
			State: snap.State, RuntimeState: snap.RuntimeState, Version: snap.Version,
			Error: &CtrlError{Code: ErrTaskNotRequeueable, Message: "task is not failed or stale"},
		}, nil
	}
	if requeueable && snap.RuntimeState.Effective() == RuntimeStateAlive {
		releaseClaim()
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
			State: snap.State, RuntimeState: snap.RuntimeState, Version: snap.Version,
			Error: &CtrlError{Code: ErrTaskInProgress, Message: "task runtime is still alive"},
		}, nil
	}
	if snap.State.Terminal() && !requeueable {
		releaseClaim()
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
			State: snap.State, RuntimeState: snap.RuntimeState, Version: snap.Version,
			Error: &CtrlError{Code: ErrTaskAlreadyTerminal, Message: "task is terminal"},
		}, nil
	}
	if !requeueable && !snap.State.ValidTransition(targetState) {
		releaseClaim()
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
			State: snap.State, RuntimeState: snap.RuntimeState, Version: snap.Version,
			Error: &CtrlError{Code: ErrTaskInvalidTransition, Message: "invalid transition"},
		}, nil
	}
	runtimeControl := cmd == "stop" || cmd == "cancel"
	if runtimeControl && killer == nil {
		releaseClaim()
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
			State: snap.State, RuntimeState: snap.RuntimeState, Version: snap.Version,
			Error: &CtrlError{Code: ErrTaskRuntimeUnavailable, Message: "task runtime owner is unavailable"},
		}, nil
	}
	if runtimeControl && !killer.Kill(snap.SessionID, runtimeJobID(snap)) {
		releaseClaim()
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
			State: snap.State, RuntimeState: snap.RuntimeState, Version: snap.Version,
			Error: &CtrlError{Code: ErrTaskRuntimeUnavailable, Message: "task runtime owner rejected control request"},
		}, nil
	}

	if runtimeControl {
		// The runtime owner accepted the request. From this point the idempotency
		// claim must not be released: a retry must never repeat an admitted runtime
		// side effect merely because persistence or audit reporting failed.
		claimed = false
	}

	// ── 1. SaveTask (state mutation) ──
	// Kill admission races the recorder's terminal completion and the runtime
	// heartbeat. Retry those expected version advances. If RecordDone already
	// persisted the requested terminal state, use that snapshot as the result.
	const maxControlSaveAttempts = 4
	for attempt := range maxControlSaveAttempts {
		next := *snap
		next.Version++
		next.State = targetState
		next.UpdatedAt = timeNow()
		if requeueable {
			next.RuntimeLeaseUntil = time.Time{}
			next.RuntimeOwnerID = ""
		}
		if runtimeControl && next.RuntimeState.Effective() == RuntimeStateAlive && next.RuntimeLeaseUntil.IsZero() {
			// A successful kill request is only an admission signal: the runtime
			// may still be exiting. Preserve an existing owner lease, and give
			// legacy lease-less snapshots a bounded deadline so observers can
			// eventually reconcile alive to exited if RecordDone never arrives.
			next.RuntimeLeaseUntil = next.UpdatedAt.Add(runtimeLeaseTTL)
		}
		if err := cs.store.SaveTask(ctx, projectDir, next); err == nil {
			snap = &next
			claimed = false
			break
		} else if !errors.Is(err, ErrStoreVersionConflict) {
			releaseClaim()
			return ControlResult{}, fmt.Errorf("save task control state: %w", err)
		}

		latest, getErr := cs.store.GetTask(ctx, projectDir, taskID)
		if getErr != nil {
			releaseClaim()
			return ControlResult{}, getErr
		}
		if latest == nil {
			releaseClaim()
			return ControlResult{}, fmt.Errorf("save task control state: task disappeared")
		}
		if latest.State == targetState {
			snap = latest
			claimed = false
			break
		}
		if latest.State.Terminal() || !latest.State.ValidTransition(targetState) {
			releaseClaim()
			return ControlResult{
				SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: latest.SessionID,
				State: latest.State, RuntimeState: latest.RuntimeState, Version: latest.Version,
				Error: &CtrlError{Code: ErrTaskVersionConflict, Message: "task changed concurrently after runtime accepted control"},
			}, nil
		}
		snap = latest
		if attempt == maxControlSaveAttempts-1 {
			releaseClaim()
			return ControlResult{
				SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: latest.SessionID,
				State: latest.State, RuntimeState: latest.RuntimeState, Version: latest.Version,
				Error: &CtrlError{Code: ErrTaskVersionConflict, Message: "task kept changing after runtime accepted control"},
			}, nil
		}
	}

	// ── 2. AppendAuditEvent (atomic sequence + write) ──
	auditEv := TaskEvent{
		Sequence:     0, // assigned atomically by store
		Timestamp:    timeNow(),
		EventType:    "control_" + cmd,
		TaskID:       taskID,
		SessionID:    snap.SessionID,
		State:        targetState,
		RuntimeState: snap.RuntimeState,
	}
	if err := cs.store.AppendAuditEvent(ctx, projectDir, auditEv); err != nil {
		// State is committed but audit is missing. This is a degraded
		// but not silent state — the caller receives an error.
		return ControlResult{
			SchemaVersion: 1, Command: cmd, TaskID: taskID,
			Error: &CtrlError{Code: ErrTaskAuditFailed, Message: "state saved but audit event failed"},
		}, fmt.Errorf("append audit event: %w", err)
	}

	// ── 3. RecordIdempotency (claim key after successful mutation) ──
	if idemKey != "" {
		rec := IdempotencyRecord{Key: idemKey, Op: cmd, TaskID: taskID, Version: expectedVersion}
		var err error
		if claimer != nil {
			err = claimer.FinalizeIdempotency(ctx, projectDir, rec)
			claimed = false
		} else {
			err = cs.store.RecordIdempotency(ctx, projectDir, rec)
		}
		if err != nil {
			return ControlResult{}, fmt.Errorf("record idempotency: %w", err)
		}
	}

	return ControlResult{
		SchemaVersion: 1, Command: cmd, TaskID: taskID, SessionID: snap.SessionID,
		State: snap.State, RuntimeState: snap.RuntimeState,
		Version: snap.Version, Accepted: true,
	}, nil
}

var timeNow = func() time.Time { return time.Now() }
