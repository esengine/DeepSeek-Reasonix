package taskmonitor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// RuntimeStarter starts one fresh runtime for a queued task. Implementations
// own the process/session wiring; the scheduler only records the resulting
// runtime identity and lease.
type RuntimeStarter interface {
	StartTask(ctx context.Context, projectDir string, task TaskSnapshot) (jobID string, leaseUntil time.Time, err error)
}

type runtimeStartError struct{ cause error }

func truncateSummary(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxErrorSummaryLen {
		return s[:maxErrorSummaryLen]
	}
	return s
}

func (e runtimeStartError) Error() string {
	if e.cause == nil {
		return ErrTaskRuntimeStartFailed
	}
	return fmt.Sprintf("%s: %s", ErrTaskRuntimeStartFailed, e.cause)
}

// Scheduler consumes queued snapshots. It is deliberately small and
// side-effect free apart from RuntimeStarter: the store remains the single
// source of truth and IdempotencyClaimer provides the cross-process claim.
type Scheduler struct {
	store         WriteStore
	starter       RuntimeStarter
	mu            sync.Mutex
	maxConcurrent int
}

func NewScheduler(store WriteStore, starter RuntimeStarter) *Scheduler {
	return &Scheduler{store: store, starter: starter}
}

// SetMaxConcurrent limits how many live runtimes this consumer may own for a
// project. A non-positive value means no scheduler-side limit; the host may
// still enforce its own subagent/runtime limit.
func (s *Scheduler) SetMaxConcurrent(limit int) {
	if s != nil {
		s.maxConcurrent = limit
	}
}

// Run keeps consuming the persisted queue until ctx is cancelled. Runtime
// ownership remains with RuntimeStarter; this loop only retries queued work.
func (s *Scheduler) Run(ctx context.Context, projectDir string, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	if _, err := s.RunOnce(ctx, projectDir); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.RunOnce(ctx, projectDir); err != nil {
				return err
			}
		}
	}
}

// RunOnce claims and attempts every queued task visible in projectDir. A
// single task failure is recorded and does not prevent other queued tasks from
// being considered.
func (s *Scheduler) RunOnce(ctx context.Context, projectDir string) (int, error) {
	if s == nil || s.store == nil || s.starter == nil {
		return 0, fmt.Errorf("task scheduler is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.store.ListTasks(ctx, projectDir)
	if err != nil {
		return 0, err
	}
	started := 0
	var firstErr error
	alive := 0
	for _, task := range tasks {
		if task.RuntimeState.Effective() == RuntimeStateAlive && !task.State.Terminal() {
			alive++
		}
	}
	for _, task := range tasks {
		if task.State != TaskStateQueued || task.RuntimeState.Effective() == RuntimeStateAlive {
			continue
		}
		if s.maxConcurrent > 0 && alive >= s.maxConcurrent {
			break
		}
		if err := s.scheduleOne(ctx, projectDir, task); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		started++
		alive++
	}
	return started, firstErr
}

// ScheduleTask is the single-task form used by Requeue. It fetches the latest
// snapshot so a stale client cannot schedule an older attempt.
func (s *Scheduler) ScheduleTask(ctx context.Context, projectDir, taskID string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("task scheduler is not configured")
	}
	task, err := s.store.GetTask(ctx, projectDir, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("task %s not found", taskID)
	}
	if task.State != TaskStateQueued {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scheduleOne(ctx, projectDir, *task)
}

func (s *Scheduler) scheduleOne(ctx context.Context, projectDir string, task TaskSnapshot) error {
	key := fmt.Sprintf("scheduler:%s:%d", task.TaskID, task.Version)
	claimer, ok := s.store.(IdempotencyClaimer)
	if !ok {
		return fmt.Errorf("task scheduler requires idempotency support")
	}
	rec, err := claimer.ClaimIdempotency(ctx, projectDir, IdempotencyRecord{Key: key, Op: "schedule", TaskID: task.TaskID, Version: task.Version})
	if err != nil {
		return err
	}
	if rec != nil {
		return nil
	}
	jobID, lease, startErr := s.starter.StartTask(ctx, projectDir, task)
	if startErr == nil && jobID == "" {
		startErr = fmt.Errorf("runtime returned empty job id")
	}
	now := timeNow()
	next := task
	next.Version++
	next.UpdatedAt = now
	next.Attempt++
	if next.Attempt == 0 {
		next.Attempt = 1
	}
	if startErr != nil {
		next.State = TaskStateFailed
		next.RuntimeState = RuntimeStateExited
		next.RuntimeLeaseUntil = time.Time{}
		next.ErrorCode = ErrTaskRuntimeStartFailed
		next.ErrorSummary = truncateSummary(startErr.Error())
	} else {
		next.State = TaskStateRunning
		next.RuntimeState = RuntimeStateAlive
		next.RuntimeLeaseUntil = lease
		next.JobID = jobID
		next.ErrorCode = ""
		next.ErrorSummary = ""
	}
	if err := s.store.SaveTask(ctx, projectDir, next); err != nil {
		return err
	}
	ev := TaskEvent{Timestamp: now, EventType: "scheduler_" + map[bool]string{true: "start", false: "failed"}[startErr == nil], TaskID: next.TaskID, SessionID: next.SessionID, State: next.State, RuntimeState: next.RuntimeState, JobID: next.JobID, ParentTaskID: next.ParentTaskID, ParentSessionID: next.ParentSessionID, Kind: next.Kind, Depth: next.Depth, Attempt: next.Attempt, ErrorCode: next.ErrorCode, ErrorSummary: next.ErrorSummary}
	if err := s.store.AppendAuditEvent(ctx, projectDir, ev); err != nil {
		return err
	}
	if err := claimer.FinalizeIdempotency(ctx, projectDir, IdempotencyRecord{Key: key, Op: "schedule", TaskID: task.TaskID, Version: task.Version}); err != nil {
		return err
	}
	if startErr != nil {
		return runtimeStartError{cause: startErr}
	}
	return nil
}
