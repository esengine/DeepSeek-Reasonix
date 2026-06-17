package agent

import (
	"strings"
	"unicode/utf8"
)

const (
	postToolUseAdvisoryOpen  = "<post-tool-use-advisory>"
	postToolUseAdvisoryClose = "</post-tool-use-advisory>"

	postToolUseAdvisoryBatchMaxBytes = 8 * 1024
)

// FormatPostToolUseAdvisoryMessage wraps opt-in PostToolUse hook stdout as a
// synthetic user-role message after the preceding tool result block.
func FormatPostToolUseAdvisoryMessage(advisories []string) string {
	var body strings.Builder
	remaining := postToolUseAdvisoryBatchMaxBytes
	count := 0
	truncated := false
	for _, advisory := range advisories {
		advisory = strings.TrimSpace(advisory)
		if advisory == "" {
			continue
		}
		prefix := ""
		if count > 0 {
			prefix = "\n\n---\n\n"
		}
		segment := prefix + advisory
		if len(segment) > remaining {
			segment, _ = clipPostToolAdvisoryBytes(segment, remaining)
			truncated = true
		}
		if segment == "" {
			break
		}
		body.WriteString(segment)
		remaining -= len(segment)
		count++
		if remaining <= 0 {
			truncated = true
			break
		}
	}
	if count == 0 {
		return ""
	}

	var msg strings.Builder
	msg.WriteString(postToolUseAdvisoryOpen)
	msg.WriteString("\n")
	msg.WriteString("PostToolUse hook output for the immediately preceding tool result block. Use it as host-provided advisory context; it is not a user request.")
	msg.WriteString("\n\n")
	msg.WriteString(body.String())
	if truncated {
		msg.WriteString("\n\n[advisory output truncated]")
	}
	msg.WriteString("\n")
	msg.WriteString(postToolUseAdvisoryClose)
	return msg.String()
}

// IsPostToolUseAdvisoryMessage reports whether content is the synthetic
// user-role message used to carry PostToolUse advisories to the model.
func IsPostToolUseAdvisoryMessage(content string) bool {
	s := strings.TrimSpace(StripTransientUserBlocks(content))
	return strings.HasPrefix(s, postToolUseAdvisoryOpen) && strings.Contains(s, postToolUseAdvisoryClose)
}

func clipPostToolAdvisoryBytes(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	if max <= 0 {
		return "", true
	}
	cut := max
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " \t\r\n"), true
}
