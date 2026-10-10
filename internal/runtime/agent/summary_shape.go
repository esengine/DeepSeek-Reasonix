package agent

import (
	"errors"
	"strings"
)

// summaryClosingInstruction ends the summarizer's user turn. The transcript
// before it ends on the last tool output, which reads as a conversation
// waiting for the agent's next move.
const summaryClosingInstruction = "\n\n---\nThe transcript above is complete. Do not continue the work and do not call tools. Reply now with the briefing, under the required headings."

var errSummaryNotDigest = errors.New("summarizer did not return a briefing under the required headings")

// hasDigestHeading reports whether text has an ATX heading line (one to six
// hashes), the shape the summary prompt asks for in level 2. A request that
// offers no tools can still be answered with the agent's next move; that
// answer has no such line.
func hasDigestHeading(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if hashes := len(line) - len(strings.TrimLeft(line, "#")); hashes >= 1 && hashes <= 6 && strings.HasPrefix(line[hashes:], " ") {
			return true
		}
	}
	return false
}
