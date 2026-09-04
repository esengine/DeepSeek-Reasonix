package agent

import (
	"errors"
	"time"
)

// SaveCancelled records a run the caller stopped. It persists everything a
// failure does — the transcript is as worth keeping when work is cut short as
// when it goes wrong — and differs only in what it says happened.
func (s *SubagentStore) SaveCancelled(run *SubagentRun, reason string) error {
	return s.saveTerminal(run, SubagentCancelled, reason)
}

func (s *SubagentStore) saveTerminal(run *SubagentRun, status SubagentStatus, reason string) error {
	if s == nil || run == nil || run.Ref == "" {
		return nil
	}
	if s.parentDestroyed(run) {
		return nil
	}
	// Terminal status is independent from transcript persistence. Keep going so
	// a sidecar failure cannot leave a settled run marked as running on disk.
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
