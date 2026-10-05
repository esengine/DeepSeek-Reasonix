package event

import "strings"

// CompletionSummaryInfo is the content-free quality summary on CompletionSummary
// events. It never carries user prompts, file contents, command args, or
// reviewer reasoning.
type CompletionSummaryInfo struct {
	Preset           string // light | balanced | delivery
	Verdict          string // complete | partial | blocked | continue
	Mutations        int
	ChecksPassed     int
	ChecksFailed     int
	ChecksSuppressed int
	Review           string // none | passed | warned | failed | unavailable
	GapKinds         []string
	// CriteriaRewritten names existing tests the turn rewrote or removed.
	// A suite green after its checks moved is not the suite that was green
	// before, so this is reported rather than folded into the pass count.
	CriteriaRewritten []string
}

// NeedsAttention reports whether this turn ended with something the user should
// know about. It lives here so every frontend answers it the same way: a turn
// the headless runner reports as finished while the chat TUI would have flagged
// it is the same turn described two ways.
func (c *CompletionSummaryInfo) NeedsAttention() bool {
	if c == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(c.Verdict)) {
	case "partial", "blocked":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(c.Review)) {
	case "warned", "failed", "unavailable":
		return true
	}
	return c.ChecksFailed > 0 || c.ChecksSuppressed > 0 || len(c.GapKinds) > 0 ||
		len(c.CriteriaRewritten) > 0
}

// Blocked distinguishes the turn that could not finish from the one that
// finished with gaps, which frontends word differently.
func (c *CompletionSummaryInfo) Blocked() bool {
	return c != nil && strings.EqualFold(strings.TrimSpace(c.Verdict), "blocked")
}
