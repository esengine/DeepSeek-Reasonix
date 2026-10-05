package extension

import (
	"fmt"
	"sync"
	"time"
)

// LifecycleRegistry tracks component lifecycle states for one published
// generation. It is diagnostic-first: doctor and UI read it; activation code
// advances states through Transition.
type LifecycleRegistry struct {
	mu         sync.Mutex
	generation uint64
	states     map[ComponentID]*ComponentStatus
}

// NewLifecycleRegistry returns an empty registry for generation.
func NewLifecycleRegistry(generation uint64) *LifecycleRegistry {
	return &LifecycleRegistry{
		generation: generation,
		states:     make(map[ComponentID]*ComponentStatus),
	}
}

// Generation returns the bound generation.
func (r *LifecycleRegistry) Generation() uint64 {
	if r == nil {
		return 0
	}
	return r.generation
}

// Ensure registers id in Inactive if missing.
func (r *LifecycleRegistry) Ensure(id ComponentID) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.states[id]; !ok {
		r.states[id] = &ComponentStatus{
			ID:         id,
			State:      ComponentInactive,
			Generation: r.generation,
			UpdatedAt:  time.Now().UTC(),
		}
	}
}

// Transition advances a component to next when the transition is legal.
// Illegal transitions return an error and leave state unchanged.
func (r *LifecycleRegistry) Transition(id ComponentID, next ComponentState, diag string) error {
	if r == nil {
		return fmt.Errorf("extension: nil lifecycle registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.states[id]
	if !ok {
		cur = &ComponentStatus{ID: id, State: ComponentInactive, Generation: r.generation}
		r.states[id] = cur
	}
	if !legalTransition(cur.State, next) {
		return fmt.Errorf("extension: illegal lifecycle transition %s: %s -> %s", id, cur.State, next)
	}
	cur.State = next
	cur.UpdatedAt = time.Now().UTC()
	if diag != "" {
		cur.Diagnostics = append(cur.Diagnostics, diag)
	}
	if next == ComponentFailed && diag != "" {
		cur.Error = diag
	}
	return nil
}

// Fail marks the component Failed with error text.
func (r *LifecycleRegistry) Fail(id ComponentID, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_ = r.Transition(id, ComponentFailed, msg)
}

// Status returns a copy of one component status.
func (r *LifecycleRegistry) Status(id ComponentID) (ComponentStatus, bool) {
	if r == nil {
		return ComponentStatus{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.states[id]
	if !ok {
		return ComponentStatus{}, false
	}
	cp := *s
	cp.Diagnostics = append([]string(nil), s.Diagnostics...)
	return cp, true
}

// All returns a snapshot of every component status.
func (r *LifecycleRegistry) All() []ComponentStatus {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ComponentStatus, 0, len(r.states))
	for _, s := range r.states {
		cp := *s
		cp.Diagnostics = append([]string(nil), s.Diagnostics...)
		out = append(out, cp)
	}
	return out
}

// RuntimeStatus builds the host-facing status document.
func (r *LifecycleRegistry) RuntimeStatus(plan *RuntimePlan, receipts []EffectReceipt) *RuntimeStatus {
	if r == nil {
		return nil
	}
	return &RuntimeStatus{
		PublishedGeneration: r.generation,
		Components:          r.All(),
		Plan:                PlanView(plan),
		Receipts:            receipts,
	}
}

func legalTransition(from, to ComponentState) bool {
	if from == to {
		return true
	}
	switch from {
	case ComponentInactive:
		return to == ComponentPreparing || to == ComponentFailed
	case ComponentPreparing:
		return to == ComponentActive || to == ComponentFailed || to == ComponentInactive
	case ComponentActive:
		return to == ComponentDraining || to == ComponentFailed
	case ComponentDraining:
		return to == ComponentInactive || to == ComponentFailed
	case ComponentFailed:
		return to == ComponentInactive || to == ComponentPreparing
	default:
		return false
	}
}
