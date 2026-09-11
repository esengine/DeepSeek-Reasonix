package completion

import "strings"

// AttentionInput is the content-free summary both frontends use to decide
// whether a turn should interrupt the user.
type AttentionInput struct {
	Verdict            string
	ChecksFailed       int
	GapKinds           []string
	Floor              string
	RequiredSuppressed bool
}

// NeedsAttention reports a user-facing quality interrupt. Scratch writes,
// unreviewed workspace files on the standard floor, and unavailable review
// do not qualify.
func NeedsAttention(in AttentionInput) bool {
	if strings.EqualFold(strings.TrimSpace(in.Verdict), "blocked") || in.ChecksFailed > 0 || in.RequiredSuppressed {
		return true
	}
	kinds := make(map[string]bool, len(in.GapKinds))
	for _, kind := range in.GapKinds {
		kinds[strings.ToLower(strings.TrimSpace(kind))] = true
	}
	if kinds["unbacked_claim"] || kinds["failed_verification"] {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(in.Floor), "delivery") {
		return kinds["unverified_change"] || kinds["missing_check"] || kinds["stale_verification"] || kinds["unproven_criterion"]
	}
	return false
}
