package delegation

import (
	"errors"
	"reasonix/internal/state/sessionstore"
	"time"
)

func (s *SubagentStore) saveTerminal(run *SubagentRun, status sessionstore.SubagentStatus, reason string) error {
	if s == nil || run == nil || run.Ref == "" {
		return nil
	}
	if s.parentDestroyed(run) {
		return nil
	}
	branchErr := s.ensureBranchCreatedAt(run)
	var sessionErr error
	if run.Session != nil {
		sessionErr = run.Session.Save(s.sessionPath(run.Ref))
	}
	meta := run.Meta
	meta.Status, meta.TerminalReason = status, reason
	meta.UpdatedAt = time.Now().UTC()
	run.Meta = meta
	return errors.Join(branchErr, sessionErr, s.saveMeta(meta))
}
