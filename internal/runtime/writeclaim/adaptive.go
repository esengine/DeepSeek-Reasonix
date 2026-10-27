package writeclaim

// EffectiveLimit is the current admission ceiling; existing holders keep their slots.
func (s *SubagentScheduler) EffectiveLimit() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effectiveLimitLocked()
}

func (s *SubagentScheduler) effectiveLimitLocked() int {
	if s.adaptiveTotal > 0 {
		return s.adaptiveTotal
	}
	return s.maxTotal
}

// ObserveBackpressure reduces future admissions without revoking live write claims.
func (s *SubagentScheduler) ObserveBackpressure() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ceiling := s.effectiveLimitLocked()
	if ceiling == 0 {
		ceiling = max(2, s.activeTotal)
		s.recoveryTotal = ceiling
	}
	s.adaptiveTotal = max(1, ceiling/2)
	s.healthyRuns = 0
}

// ObserveSuccess grows by one after a full window of successful completions.
func (s *SubagentScheduler) ObserveSuccess() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ceiling := s.maxTotal
	if ceiling == 0 {
		ceiling = s.recoveryTotal
	}
	if s.adaptiveTotal == 0 || s.adaptiveTotal >= ceiling {
		return
	}
	s.healthyRuns++
	if s.healthyRuns >= s.adaptiveTotal {
		s.adaptiveTotal++
		if s.maxTotal == 0 && s.adaptiveTotal >= ceiling {
			s.adaptiveTotal = 0
		}
		s.healthyRuns = 0
		s.pumpWaitersLocked()
	}
}
