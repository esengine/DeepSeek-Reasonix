package browser

import (
	"context"
	"slices"
)

// CloseTab closes one tab; the most recently opened remaining tab becomes active.
func (s *Session) CloseTab(ctx context.Context, tabID string) error {
	ctx, cancel := context.WithTimeout(ctx, closeTimeout)
	defer cancel()
	t, err := s.tab(tabID)
	if err != nil {
		if CodeOf(err) == CodeTabClosed {
			s.mu.Lock()
			if tabID == "" {
				clear(s.lost)
			} else {
				delete(s.lost, tabID)
			}
			s.mu.Unlock()
			return nil
		}
		return err
	}
	if s.targetGone(t) {
		s.forgetClosed(t)
		return nil
	}
	var result struct {
		Success bool `json:"success"`
	}
	err = t.eng.conn.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.targetID}, &result)
	if err != nil || !result.Success {
		if s.confirmTargetGone(ctx, t) {
			s.forgetClosed(t)
			return nil
		}
		if err != nil {
			return engineFailure(err)
		}
		return fail(CodeEngineFailed, "browser refused to close tab %s", tabID)
	}
	s.forgetClosed(t)
	return nil
}

func (s *Session) forgetClosed(t *tab) {
	s.removeTab(t)
	s.mu.Lock()
	delete(s.lost, t.id)
	s.mu.Unlock()
}

func (s *Session) targetGone(t *tab) bool {
	if !t.eng.alive() {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !slices.Contains(s.tabs, t)
}

// A protocol refusal alone cannot prove absence; the browser's target list can.
func (s *Session) confirmTargetGone(ctx context.Context, t *tab) bool {
	if s.targetGone(t) {
		return true
	}
	var result struct {
		Targets []struct {
			ID string `json:"targetId"`
		} `json:"targetInfos"`
	}
	if err := t.eng.conn.call(ctx, "", "Target.getTargets", nil, &result); err != nil {
		return s.targetGone(t)
	}
	if result.Targets == nil {
		return false
	}
	for _, target := range result.Targets {
		if target.ID == t.targetID {
			return false
		}
	}
	return true
}
