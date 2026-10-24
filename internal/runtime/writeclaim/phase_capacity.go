package writeclaim

import (
	"crypto/rand"
	"fmt"
	"math"
	"slices"
	"sync"

	"reasonix/internal/state/mutation"
)

// PhaseCapacity names independent pools shared by every runtime of one root.
// Model waits and file waits cannot consume one another's capacity.
type PhaseCapacity struct {
	Model       []mutation.Capacity
	Mutation    []mutation.Capacity
	Preparation []mutation.Capacity
}

const maxResidentExecutions = 64

type phaseExecutions struct{ writers, resident int }

type ResidentLimitError struct{ Limit int }

func (e *ResidentLimitError) Error() string {
	return fmt.Sprintf("root execution has %d resident subagents; collect or cancel existing work before retrying", e.Limit)
}

func (*ResidentLimitError) RefusalCode() string { return "execution.resident_limit" }

// ReserveResident bounds prepared and parked children independently of running
// phases. Refusal never queues a nested child behind its own resident parent.
func (s *SubagentScheduler) ReserveResident() (func(), error) {
	if s == nil {
		return func() {}, nil
	}
	s.mu.Lock()
	limit := max(maxResidentExecutions, s.maxTotal)
	if s.phaseLive.resident >= limit {
		s.mu.Unlock()
		return nil, &ResidentLimitError{Limit: limit}
	}
	s.phaseLive.resident++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.phaseLive.resident--
			s.mu.Unlock()
		})
	}, nil
}

// TrackExecution records live writer capability without reserving phase capacity.
func (s *SubagentScheduler) TrackExecution(writer bool) func() {
	if s == nil || !writer {
		return func() {}
	}
	s.mu.Lock()
	s.phaseLive.writers++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.phaseLive.writers--
			s.mu.Unlock()
		})
	}
}

func newPhaseCapacity(total, writers int) PhaseCapacity {
	id := "execution_" + rand.Text()
	return PhaseCapacity{
		Model: phasePool(id+".model", total), Mutation: phasePool(id+".mutation", writers),
		Preparation: phasePool(id+".preparation", total),
	}
}

func phasePool(id string, limit int) []mutation.Capacity {
	if limit <= 0 {
		return nil
	}
	return []mutation.Capacity{{ID: id, Limit: uint32(min(uint64(limit), math.MaxUint32)), Units: 1}}
}

func (p PhaseCapacity) Clone() PhaseCapacity {
	return PhaseCapacity{Model: slices.Clone(p.Model), Mutation: slices.Clone(p.Mutation), Preparation: slices.Clone(p.Preparation)}
}

func (s *SubagentScheduler) PhaseCapacity() PhaseCapacity {
	if s == nil {
		return PhaseCapacity{}
	}
	return s.phases.Clone()
}
