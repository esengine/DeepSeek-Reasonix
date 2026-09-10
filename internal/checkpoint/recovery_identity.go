package checkpoint

import "reasonix/internal/provider"

// RecoveryIdentity correlates the first writer's preimages with its durable
// action record. It does not claim that the external effect committed.
type RecoveryIdentity struct {
	Action           provider.ActionIdentity `json:"action"`
	TranscriptDigest string                  `json:"transcriptDigest"`
}

func (s *Store) BindRecoveryIdentity(identity RecoveryIdentity) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil || s.cur.Recovery != nil {
		return nil
	}
	s.cur.Recovery = &identity
	if err := s.persist(s.cur); err != nil {
		s.cur.Recovery = nil
		return err
	}
	return nil
}
