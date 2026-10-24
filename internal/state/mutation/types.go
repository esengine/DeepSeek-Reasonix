// Package mutation coordinates short filesystem transactions across runtimes.
package mutation

import (
	"errors"
	"time"
)

type Effect string

const (
	ReadOnly     Effect = "read_only"
	NamedPaths   Effect = "named_paths"
	Workspace    Effect = "workspace"
	GitMetadata  Effect = "git_metadata"
	External     Effect = "external"
	CapacityOnly Effect = "capacity"
)

var (
	ErrInvalidIntent       = errors.New("invalid mutation intent")
	ErrVersionConflict     = errors.New("mutation input changed")
	ErrWaitTimeout         = errors.New("mutation wait exhausted")
	ErrCapacityWaitTimeout = errors.New("execution capacity wait exhausted")
	ErrProtocol            = errors.New("unsupported mutation coordination record")
	ErrCallback            = errors.New("mutation coordination callback failed")
	ErrBusy                = errors.New("mutation resources are busy")
)

type Intent struct {
	tryOnly     bool
	Effect      Effect
	Paths       []string
	SessionID   string
	ExecutionID string
	CallID      string
	Expected    map[string]ContentVersion
	Recovery    bool
	Capacities  []Capacity
}

type Outcome string

const (
	NotExecuted Outcome = "not_executed"
	Committed   Outcome = "committed"
	Partial     Outcome = "partial"
	Unknown     Outcome = "unknown"
)

type Notice struct {
	Effect              Effect
	BlockedOn           Effect
	Capacities          []Capacity
	TransactionID       string
	SessionID           string
	ExecutionID         string
	CallID              string
	Paths               []string
	HolderSessionID     string
	HolderExecutionID   string
	HolderTransactionID string
	HolderPaths         []string
	State               string
	Reason              string
	StartedAt           time.Time
	Elapsed             time.Duration
	Outcome             Outcome
}

type WaitError struct {
	Effect        Effect
	Capacities    []Capacity
	TransactionID string
	Paths         []string
	Cause         error
}

func (e *WaitError) Error() string {
	if e.Effect == CapacityOnly {
		return "execution capacity is busy (transaction " + e.TransactionID + "); reschedule the request: " + e.Cause.Error()
	}
	return "mutation resources are busy (transaction " + e.TransactionID + "); reschedule the operation: " + e.Cause.Error()
}
func (e *WaitError) Unwrap() error { return e.Cause }
func (e *WaitError) Is(target error) bool {
	if e.Effect == CapacityOnly {
		return target == ErrCapacityWaitTimeout
	}
	return target == ErrWaitTimeout
}
func (e *WaitError) RefusalCode() string {
	if e.Effect == CapacityOnly {
		return "execution.capacity_wait_timeout"
	}
	return "workspace.mutation_wait_timeout"
}

type VersionConflict struct{ Path string }

func (e *VersionConflict) Error() string      { return "file changed before the mutation: " + e.Path }
func (*VersionConflict) Is(target error) bool { return target == ErrVersionConflict }
func (*VersionConflict) RefusalCode() string  { return "workspace.mutation_version_conflict" }
