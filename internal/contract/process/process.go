package process

import (
	"slices"
	"time"
)

// State is where a unit of work stands, in terms every owner can be mapped onto.
type State string

const (
	Pending     State = "pending"
	Queued      State = "queued"
	Running     State = "running"
	Waiting     State = "waiting"
	Completed   State = "completed"
	Failed      State = "failed"
	Stopped     State = "stopped"
	Interrupted State = "interrupted"
	Skipped     State = "skipped"
)

// Cause says why a unit is in its State. Empty means the owner does not record
// one, which is not the same as there being none.
type Cause string

const (
	CauseNone    Cause = ""
	CauseSlots   Cause = "slots"
	CauseWriters Cause = "writers"
	CauseClaim   Cause = "claim"
	CauseHeld    Cause = "held"
	CauseAdopted Cause = "adopted"
	CauseBudget  Cause = "budget"
	CauseParked  Cause = "parked"
)

// Class is one owner state projected onto the shared vocabulary.
type Class struct {
	State State
	Cause Cause
}

// States lists every State.
var States = []State{Pending, Queued, Running, Waiting, Completed, Failed, Stopped, Interrupted, Skipped}

// Terminal reports whether a unit in this state will not change state again.
func (s State) Terminal() bool {
	switch s {
	case Completed, Failed, Stopped, Interrupted, Skipped:
		return true
	}
	return false
}

// Valid reports whether s is one of States.
func (s State) Valid() bool { return slices.Contains(States, s) }

// Kind is what sort of work a Process is. It is the owner that produced it.
type Kind string

const (
	KindSubagent Kind = "subagent"
	KindJob      Kind = "job"
)

// Process is one running unit as a reader sees it.
type Process struct {
	// ID is "<kind>:<owner id>", stable for as long as the owner keeps the unit.
	ID   string
	Kind Kind
	// Parent is the ID of the process that started this one; empty for a root.
	Parent  string
	Name    string
	State   State
	Cause   Cause
	Started time.Time
}

// ID builds the process id for an owner's own id.
func ID(k Kind, owner string) string { return string(k) + ":" + owner }
