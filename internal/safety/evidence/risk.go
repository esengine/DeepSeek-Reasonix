package evidence

import (
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
)

// RiskLevel classifies the latest post-mutation change set for adaptive review.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// privilegedMutationTools are the built-in surfaces that install code the host
// did not write. They are exact names, not fragments.
var privilegedMutationTools = []string{"install_source", "install_skill"}

// ClassifyMutationRisk scores the change set after the latest mutation.
// Low: docs/tests/i18n only. Medium: ordinary production code. High: a path the
// project declared sensitive, 10+ paths, or an opaque write — one the host
// proved happened but cannot name a path for. A command it merely failed to
// prove read-only names no change set at all, so it scores nothing.
func ClassifyMutationRisk(receipts []Receipt, after int, sensitive []string) RiskLevel {
	start := max(after+1, 0)
	var paths []string
	seen := map[string]bool{}
	opaque := false
	hasProd := false
	onlyLow := true

	collect := func(r Receipt) bool {
		if !r.Success || !r.Mutation {
			return false
		}
		if len(r.Paths) == 0 && r.MutationEvidence == MutationProven {
			opaque = true
		}
		if toolIsPrivilegedMutation(r.ToolName) {
			return true
		}
		for _, p := range r.Paths {
			// A repository store is not part of any change set: `git commit`
			// writes one, and reading that as a change made the tree's own
			// bookkeeping the thing a reviewer was sent to inspect.
			if ClassifyPath(p) == PathVCSStore || seen[p] {
				continue
			}
			seen[p] = true
			paths = append(paths, p)
		}
		return false
	}

	// Include the mutation receipt itself.
	if after >= 0 && after < len(receipts) {
		if collect(receipts[after]) {
			return RiskHigh
		}
	}
	for i := start; i < len(receipts); i++ {
		if collect(receipts[i]) {
			return RiskHigh
		}
	}
	if opaque {
		return RiskHigh
	}
	if len(paths) == 0 {
		return RiskLow
	}
	if len(paths) >= 10 {
		return RiskHigh
	}
	for _, p := range paths {
		if pathIsDeclaredSensitive(p, sensitive) {
			return RiskHigh
		}
		if ClassifyPath(p) == PathProduction {
			onlyLow = false
			hasProd = true
		}
	}
	if onlyLow && !hasProd {
		return RiskLow
	}
	return RiskMedium
}

// MutationRiskAfter classifies risk from the ledger using the latest mutation.
func (l *Ledger) MutationRiskAfter(after int, sensitive []string) RiskLevel {
	if l == nil {
		return RiskLow
	}
	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()
	return ClassifyMutationRisk(receipts, after, sensitive)
}

// PathsSince returns distinct paths from successful mutation/write receipts at
// or after the given index (inclusive of the mutation itself when after >= 0).
func (l *Ledger) PathsSince(after int) []string {
	if l == nil {
		return nil
	}
	start := max(after, 0)
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for i := start; i < len(l.receipts); i++ {
		r := l.receipts[i]
		if !r.Success || (!r.Mutation && !r.Write) {
			continue
		}
		for _, p := range r.Paths {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// pathIsDeclaredSensitive reports whether the project itself named this path.
// The predecessor guessed from spelling and could not tell `internal/auth` from
// `session_write_authority.go`, or a trace file from a data race.
func pathIsDeclaredSensitive(path string, sensitive []string) bool {
	for _, glob := range sensitive {
		if fileutil.MatchSlashGlob(path, glob) {
			return true
		}
	}
	return false
}

// toolIsPrivilegedMutation matches identifiers the host itself defines: the
// MCP namespace prefix it mints (plugin servers included, as
// mcp__plugin_<pkg>_<server>__) and two exact built-in names. Substring
// matching used to stand in for this and made any tool whose name merely
// contained "plugin" or "tool" a privileged surface.
func toolIsPrivilegedMutation(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(lower, "mcp__") {
		return true
	}
	return slices.Contains(privilegedMutationTools, lower)
}
