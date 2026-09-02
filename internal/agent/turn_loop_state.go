package agent

import (
	"sort"
	"sync"

	"reasonix/internal/tool"
)

// turnLoopState groups per-turn loop-guard maps so parallel tool goroutines
// share one lock instead of unsynchronized maps on turnRuntime.
type turnLoopState struct {
	mu                      sync.Mutex
	schemaErrors            map[string]schemaErrorRecord
	schemaCapabilities      map[string]map[string]struct{}
	dispatchClasses         map[string]tool.CallClass
	resultFingerprints      map[string]string
	acceptedDecisions       map[string]acceptedDecision
	previousErrorCategories map[string]struct{}
	softBudgetNudged        bool
	softBudgetNudgeRound    int
}

func (s *turnLoopState) setDispatchClasses(classes map[string]tool.CallClass) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispatchClasses = classes
}

func (s *turnLoopState) dispatchClass(id string) (tool.CallClass, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	class, ok := s.dispatchClasses[id]
	return class, ok
}

func (s *turnLoopState) incrementSchemaError(sig, capabilityID string) schemaErrorRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schemaErrors == nil {
		s.schemaErrors = map[string]schemaErrorRecord{}
	}
	record := s.schemaErrors[sig]
	record.count++
	s.schemaErrors[sig] = record
	if capabilityID != "" {
		if s.schemaCapabilities == nil {
			s.schemaCapabilities = map[string]map[string]struct{}{}
		}
		if s.schemaCapabilities[capabilityID] == nil {
			s.schemaCapabilities[capabilityID] = map[string]struct{}{}
		}
		s.schemaCapabilities[capabilityID][sig] = struct{}{}
	}
	return record
}

func (s *turnLoopState) markSchemaInspectAttached(sig string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.schemaErrors[sig]
	record.inspectAttached = true
	s.schemaErrors[sig] = record
}

func (s *turnLoopState) clearSchemaErrors(match func(string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sig := range s.schemaErrors {
		if match(sig) {
			delete(s.schemaErrors, sig)
			for id, signatures := range s.schemaCapabilities {
				delete(signatures, sig)
				if len(signatures) == 0 {
					delete(s.schemaCapabilities, id)
				}
			}
		}
	}
}

func (s *turnLoopState) clearSchemaErrorsForCapability(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sig := range s.schemaCapabilities[id] {
		delete(s.schemaErrors, sig)
	}
	delete(s.schemaCapabilities, id)
}

func (s *turnLoopState) rememberFingerprint(fp, callID string) (prev string, seen bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resultFingerprints == nil {
		s.resultFingerprints = map[string]string{}
	}
	prev, seen = s.resultFingerprints[fp]
	if !seen {
		s.resultFingerprints[fp] = callID
	}
	return prev, seen
}

func (s *turnLoopState) rememberDecision(id, question, answer string) {
	s.rememberDecisionAmbiguity(id, question, answer, decisionAmbiguity{})
}

func (s *turnLoopState) rememberDecisionAmbiguity(id, question, answer string, ambiguity decisionAmbiguity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.acceptedDecisions == nil {
		s.acceptedDecisions = map[string]acceptedDecision{}
	}
	s.acceptedDecisions[id] = acceptedDecision{ID: id, Question: question, Answer: answer, Ambiguity: ambiguity}
}

func (s *turnLoopState) decision(id string) (acceptedDecision, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dec, ok := s.acceptedDecisions[id]
	return dec, ok
}

func (s *turnLoopState) snapshotDecisions() []acceptedDecision {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]acceptedDecision, 0, len(s.acceptedDecisions))
	for _, decision := range s.acceptedDecisions {
		out = append(out, decision)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *turnLoopState) advanceErrorCategories(current map[string]int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	hit := false
	next := make(map[string]struct{}, len(current))
	for category, count := range current {
		if count >= 2 {
			hit = true
		}
		if _, repeated := s.previousErrorCategories[category]; repeated {
			hit = true
		}
		next[category] = struct{}{}
	}
	s.previousErrorCategories = next
	return hit
}

func (s *turnLoopState) markSoftBudgetNudged(round int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.softBudgetNudged {
		return false
	}
	s.softBudgetNudged = true
	s.softBudgetNudgeRound = round
	return true
}

func (s *turnLoopState) softBudgetNudgedAt() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.softBudgetNudgeRound
}
