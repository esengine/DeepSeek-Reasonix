package sessionstore

import (
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

func (s *Session) Composition() *agentcomposition.Binding {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.Messages {
		if m.Role == provider.RoleSystem && m.AgentComposition != nil {
			return agentcomposition.Clone(m.AgentComposition)
		}
	}
	return nil
}

func (s *Session) BindComposition(binding *agentcomposition.Binding) {
	if s == nil || binding == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Messages {
		if s.Messages[i].Role == provider.RoleSystem {
			s.Messages[i].AgentComposition = agentcomposition.Clone(binding)
			s.rewriteVersion++
			s.version++
			return
		}
	}
	s.Messages = append([]provider.Message{{Role: provider.RoleSystem, AgentComposition: agentcomposition.Clone(binding)}}, s.Messages...)
	s.rewriteVersion++
	s.version++
}

func (s *Session) markCompositionStartedLocked() {
	for i := range s.Messages {
		b := s.Messages[i].AgentComposition
		if b == nil || b.Started {
			continue
		}
		b = agentcomposition.Clone(b)
		b.Started = true
		s.Messages[i].AgentComposition = b
		s.rewriteVersion++
		return
	}
}

// Transcript rewrites cannot replace or unlock the session's admitted binding.
func (s *Session) preserveCompositionLocked(msgs []provider.Message) []provider.Message {
	var binding *agentcomposition.Binding
	for _, m := range s.Messages {
		if m.AgentComposition != nil {
			binding = m.AgentComposition
			break
		}
	}
	if binding == nil {
		return msgs
	}
	msgs = append([]provider.Message(nil), msgs...)
	for i := range msgs {
		msgs[i].AgentComposition = nil
	}
	for i := range msgs {
		if msgs[i].Role == provider.RoleSystem {
			msgs[i].AgentComposition = agentcomposition.Clone(binding)
			return msgs
		}
	}
	return append([]provider.Message{{Role: provider.RoleSystem, AgentComposition: agentcomposition.Clone(binding)}}, msgs...)
}
