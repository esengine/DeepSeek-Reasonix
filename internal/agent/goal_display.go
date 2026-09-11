package agent

import (
	"fmt"
	"strings"
)

// StripGoalMarkers removes goal status markers like [goal:complete],
// [goal:continue], and [goal:blocked:...] from display text so users see
// natural language instead of protocol markers. Exported for use by frontends.
// The markers are still kept in the session history for controller parsing.
func StripGoalMarkers(text string) string {
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[goal:complete]" || trimmed == "[goal:continue]" {
			continue
		}
		if strings.HasPrefix(trimmed, "[goal:blocked:") && strings.HasSuffix(trimmed, "]") {
			reason := strings.TrimPrefix(trimmed, "[goal:blocked:")
			reason = strings.TrimSuffix(reason, "]")
			if reason != "" {
				cleaned = append(cleaned, fmt.Sprintf("\u26a0\ufe0f Blocked: %s", reason))
			}
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

const (
	autoResearchEvidenceOpen  = "<autoresearch-evidence>"
	autoResearchEvidenceClose = "</autoresearch-evidence>"
)

// StripAutoResearchEvidenceBlocks removes historical <autoresearch-evidence>
// blocks at display boundaries only. Raw transcripts remain unchanged; current
// Goal semantics never parse these blocks or write state from them.
func StripAutoResearchEvidenceBlocks(text string) string {
	var b strings.Builder
	rest := text
	for {
		start := strings.Index(rest, autoResearchEvidenceOpen)
		if start < 0 {
			b.WriteString(rest)
			return strings.TrimSpace(b.String())
		}
		b.WriteString(rest[:start])
		afterOpen := rest[start+len(autoResearchEvidenceOpen):]
		_, after, ok := strings.Cut(afterOpen, autoResearchEvidenceClose)
		if !ok {
			return strings.TrimSpace(b.String())
		}
		rest = after
	}
}

// DisplayAssistantText is the single display filter for assistant answer text:
// it removes every protocol artifact ([goal:*] markers, autoresearch evidence
// blocks) before the text reaches a sink. Session history keeps the raw text —
// apply this at emission or render boundaries only.
func DisplayAssistantText(text string) string {
	return StripGoalMarkers(StripAutoResearchEvidenceBlocks(text))
}
