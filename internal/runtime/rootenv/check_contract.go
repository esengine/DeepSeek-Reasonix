package rootenv

import (
	"slices"

	"reasonix/internal/state/instruction"
)

// CheckContract is selected before the scene is writable. Workspace changes
// cannot replace its commands when the session is rebuilt or reopened.
type CheckContract struct {
	Version int                       `json:"version"`
	Checks  []instruction.VerifyCheck `json:"checks"`
}

func (c *CheckContract) clone() *CheckContract {
	if c == nil {
		return nil
	}
	return &CheckContract{Version: c.Version, Checks: slices.Clone(c.Checks)}
}

func (s *Store) WithVerificationChecks(checks []instruction.VerifyCheck) *Store {
	s.checks = &CheckContract{Version: 1, Checks: append([]instruction.VerifyCheck{}, checks...)}
	return s
}

func supportedRecord(r Record) bool {
	return (r.Version == 1 && r.CheckContract == nil) || (r.Version == 2 && r.CheckContract != nil && r.CheckContract.Version == 1)
}
