package checkpoint

import "fmt"

func shouldTruncateConversation(tx *TransactionManifest) bool {
	if tx == nil || tx.ConversationAction == "fork" {
		return false
	}
	return tx.Scope == RewindConversation || tx.Scope == RewindBoth
}

func (s *Store) PeekPlan(planID string) (RewindPlan, bool) {
	if s == nil {
		return RewindPlan{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pp, ok := s.plans[planID]
	if !ok {
		return RewindPlan{}, false
	}
	return pp.plan, true
}

func (s *Store) MarkPlanConversationFork(planID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pp, ok := s.plans[planID]
	if !ok {
		return
	}
	pp.plan.ConversationAction = "fork"
	s.plans[planID] = pp
}

func (s *Store) DiscardPlan(planID string) error {
	if s == nil {
		return fmt.Errorf("checkpoints unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.plans[planID]; !ok {
		return fmt.Errorf("unknown or expired plan %q", planID)
	}
	delete(s.plans, planID)
	return nil
}
