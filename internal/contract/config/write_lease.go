package config

import "strings"

// Write-lease modes, set in [agent] write_lease. They differ only in how far
// the cross-session lease reaches writers whose extent the host cannot read:
// strict serializes every such writer, optimistic excludes only declared
// write_paths from each other, off takes no cross-session lease at all.
const (
	WriteLeaseStrict     = "strict"
	WriteLeaseOptimistic = "optimistic"
	WriteLeaseOff        = "off"
)

// NormalizeWriteLeaseMode folds an unrecognised value onto strict: a mode this
// build does not know must never widen the lease on its own.
func NormalizeWriteLeaseMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case WriteLeaseOptimistic:
		return WriteLeaseOptimistic
	case WriteLeaseOff:
		return WriteLeaseOff
	default:
		return WriteLeaseStrict
	}
}

// WriteLeaseMode is the setting with the default folded in: an absent or empty
// value reads as strict.
func (a AgentConfig) WriteLeaseMode() string {
	if strings.TrimSpace(a.WriteLease) != "" {
		return NormalizeWriteLeaseMode(a.WriteLease)
	}
	return WriteLeaseStrict
}

// RelaxedWriteLease reports whether a writer that declares no write_paths
// stops holding the whole workspace, while declared paths still exclude
// other sessions.
func (a AgentConfig) RelaxedWriteLease() bool {
	return a.WriteLeaseMode() == WriteLeaseOptimistic
}

// SkipWriteLease reports whether this session takes no cross-session write lease.
func (a AgentConfig) SkipWriteLease() bool {
	return a.WriteLeaseMode() == WriteLeaseOff
}
