package planmode

import "sync"

// Phase is where a run sits in the planning workflow.
type Phase uint8

const (
	// Inactive is a run outside the workflow entirely.
	Inactive Phase = iota
	// Planning is gathering context and drafting; side effects wait.
	Planning
	// AwaitingApproval has a plan in front of the user and nothing running.
	AwaitingApproval
	// Executing is the approved plan being carried out.
	Executing
)

func (p Phase) String() string {
	switch p {
	case Planning:
		return "planning"
	case AwaitingApproval:
		return "awaiting_approval"
	case Executing:
		return "executing"
	default:
		return "inactive"
	}
}

// State is the whole lifecycle fact: one phase, one authority epoch. Anything
// derivable — active, marker-carrying — is derived at the point of use, because
// a second stored field is a second truth to keep in step.
type State struct {
	Phase Phase
	Epoch uint64
}

// Active reports whether the run is inside the workflow at all.
func (s State) Active() bool { return s.Phase != Inactive }

// AdmitsSideEffects reports whether a phase lets a call change state outside the
// session. Executing does because the user approved exactly that; Inactive does
// because there is no workflow holding anything back.
func (p Phase) AdmitsSideEffects() bool { return p == Inactive || p == Executing }

// Action names a lifecycle move. These are the only ways the state changes, so
// a caller cannot invent a path from Planning straight to Executing.
type Action uint8

const (
	// Enter opens the workflow.
	Enter Action = iota
	// Submit puts a finished plan in front of the user.
	Submit
	// Start carries an approved plan into execution.
	Start
	// Revise sends an unapproved plan back for more planning.
	Revise
	// Exit leaves the workflow from anywhere.
	Exit
)

func (a Action) String() string {
	switch a {
	case Enter:
		return "enter"
	case Submit:
		return "submit"
	case Start:
		return "start"
	case Revise:
		return "revise"
	default:
		return "exit"
	}
}

// next reports the phase an action leads to, and whether it is legal from here.
func (p Phase) next(a Action) (Phase, bool) {
	switch a {
	case Enter:
		return Planning, p == Inactive
	case Submit:
		return AwaitingApproval, p == Planning
	case Start:
		return Executing, p == AwaitingApproval
	case Revise:
		return Planning, p == AwaitingApproval
	case Exit:
		return Inactive, p != Inactive
	}
	return p, false
}

// Runtime is the canonical holder of the plan lifecycle. Every participant —
// the UI action, the provider loop, the controller — reads and moves the state
// through it, so "which phase, under which authority" has one answer.
type Runtime struct {
	mu    sync.Mutex
	state State
}

// NewRuntime returns a runtime sitting outside the workflow.
func NewRuntime() *Runtime { return &Runtime{} }

// State returns the current lifecycle fact.
func (r *Runtime) State() State {
	if r == nil {
		return State{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Transition moves the workflow when the state still matches what the caller
// last saw, which is what makes a late click harmless. Every accepted move
// raises the epoch, Submit and Revise included: work produced under an
// authority stops being admissible the moment that authority stops being
// current.
func (r *Runtime) Transition(expected State, action Action) (State, bool) {
	if r == nil {
		return State{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transitionLocked(expected, action)
}

func (r *Runtime) transitionLocked(expected State, action Action) (State, bool) {
	if r.state != expected {
		return r.state, false
	}
	phase, ok := r.state.Phase.next(action)
	if !ok {
		return r.state, false
	}
	r.state = State{Phase: phase, Epoch: r.state.Epoch + 1}
	return r.state, true
}

// Apply moves the workflow from wherever it currently is. It is the entry for
// callers with no earlier observation to compare against — a fresh UI toggle,
// session restore — and it is still a real transition, so it raises the epoch
// and refuses an illegal move like every other.
func (r *Runtime) Apply(action Action) (State, bool) {
	if r == nil {
		return State{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transitionLocked(r.state, action)
}

// SetActive is the on/off entry for callers that only know those two words —
// a toggle, a restored session. It enters or leaves the workflow and does
// nothing when already there, so repeating it is not a transition. Callers with
// a lifecycle to drive use Transition instead.
func (r *Runtime) SetActive(on bool) (State, bool) {
	if r == nil {
		return State{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if on == r.state.Active() {
		return r.state, false
	}
	if on {
		return r.transitionLocked(r.state, Enter)
	}
	return r.transitionLocked(r.state, Exit)
}
