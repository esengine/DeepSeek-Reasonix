package schedule

import "errors"

// Callers tell these apart with errors.Is; the wrapping text is display only.
var (
	ErrNotFound           = errors.New("schedule: not found")
	ErrSchemaReadonly     = errors.New("schedule: manifest schema is newer and read-only")
	ErrPausedAll          = errors.New("schedule: all schedules are paused")
	ErrNotHuman           = errors.New("schedule: creation requires a human confirmation")
	ErrInvalid            = errors.New("schedule: invalid schedule")
	ErrIntervalBelowFloor = errors.New("schedule: interval is below the minimum")
	ErrGrantExceedsCeil   = errors.New("schedule: grant exceeds the host tool ceiling")
	ErrPolicyInvalid      = errors.New("schedule: invalid policy value")
	ErrTooMany            = errors.New("schedule: schedule limit reached")
	ErrNotActive          = errors.New("schedule: schedule is not active")
	ErrExpired            = errors.New("schedule: schedule has expired")
	ErrDigestMismatch     = errors.New("schedule: confirmed content changed after confirmation")
	ErrAlreadyClaimed     = errors.New("schedule: trigger already claimed")
	ErrNotDue             = errors.New("schedule: slot is not a slot of this schedule")
	ErrConcurrency        = errors.New("schedule: another scheduled run is in flight")
	ErrBudget             = errors.New("schedule: budget does not admit another run")
	ErrRunNotFound        = errors.New("schedule: run not found")
	ErrSlotStale          = errors.New("schedule: slot is not the latest slot of this schedule")
	ErrPolicyTightened    = errors.New("schedule: policy was tightened after this schedule was confirmed")
	ErrLifetimeSpent      = errors.New("schedule: lifetime budget is spent")
	ErrRunSettled         = errors.New("schedule: run already settled")
	ErrRunStarted         = errors.New("schedule: run already started")
	ErrRunToken           = errors.New("schedule: start token does not match this run")
	ErrRunHeld            = errors.New("schedule: run is held by another executor")
	ErrResultNotFound     = errors.New("schedule: no result recorded for this run")
)
