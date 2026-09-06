package agent

import (
	"reasonix/internal/provider"
	"slices"
)

func (s *Session) updateBatchGuardResults(calls []provider.ToolCall, results []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	end := len(s.Messages) - 1
	changed, durableChange := false, false
	for i, call := range slices.Backward(calls) {
		for j := end; j >= 0; j-- {
			m := s.Messages[j]
			if m.Role != provider.RoleTool || m.ToolCallID != call.ID || m.Name != call.Name {
				continue
			}
			end = j - 1
			if m.Content != results[i] {
				s.Messages[j].Content = results[i]
				changed = true
				durableChange = durableChange || j < len(s.persistedMessages)
			}
			break
		}
	}
	if changed {
		s.version++
	}
	if durableChange {
		s.rewriteVersion++
		s.pendingContentReasons = append(s.pendingContentReasons, "tool_guard_before_sampling")
	}
}
