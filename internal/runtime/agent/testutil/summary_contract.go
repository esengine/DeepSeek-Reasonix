package testutil

import (
	"strings"

	"reasonix/internal/contract/provider"
)

// SummaryReply is what a double that follows the summary contract answers: the
// summarizer prompt requires headings, and the host refuses a reply with
// none. A plain reply to a summary request gets one, so a test that only cares
// about the digest's text keeps reading it.
func SummaryReply(req provider.Request, text string) string {
	if !req.Summary {
		return text
	}
	for line := range strings.SplitSeq(text, "\n") {
		if hashes := len(line) - len(strings.TrimLeft(line, "#")); hashes >= 1 && hashes <= 6 && strings.HasPrefix(line[hashes:], " ") {
			return text
		}
	}
	return "## Summary\n" + text
}
