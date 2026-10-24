package delegation

import (
	"context"

	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

type subagentAdmission struct {
	store *SubagentStore
	ref   string
	live  *liveRun
}

func (s *SubagentStore) admissionSaver(sess *sessionstore.Session) agent.AdmissionSaver {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ref, live := range s.live {
		if live.sess == sess {
			return subagentAdmission{store: s, ref: ref, live: live}
		}
	}
	return nil
}

func (a subagentAdmission) SaveAdmittedMessage(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.store.mu.Lock()
	current := a.store.live[a.ref]
	a.store.mu.Unlock()
	if current != a.live {
		return ErrSubagentSettling
	}
	meta, err := a.store.LoadMeta(a.ref)
	if err != nil {
		return err
	}
	if a.store.parentDestroyed(&SubagentRun{Meta: meta}) {
		return ErrSubagentSettling
	}
	return a.live.sess.Save(a.store.sessionPath(a.ref))
}
