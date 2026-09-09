package agent

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/i18n"
)

// truncateReadFileOutput returns a contiguous prefix on a complete rendered
// line when possible. Its recovery cursor is exactly the first unseen byte, so
// paging can reconstruct the original without a gap or duplicate.
func truncateReadFileOutput(s, toolName, toolCallID string) (string, string) {
	resultRef := toolResultRef(toolCallID, s)
	headKeep := maxToolOutputBytes - 1024
	if headKeep < 1024 {
		headKeep = maxToolOutputBytes / 2
	}
	head := snapToRuneBoundary(s, 0, min(headKeep, len(s)))
	if newline := strings.LastIndexByte(head, '\n'); newline >= 1024 {
		head = head[:newline+1]
	}
	for range 4 {
		marker := toolOutputRecoveryMarkerAt(toolName, toolCallID, resultRef, len(s), len(head), len(head))
		if len(head)+len(marker) <= maxToolOutputBytes {
			notice := fmt.Sprintf(i18n.M.ToolOutputTruncatedFmt, len(s)-len(head), len(s))
			return head + marker, notice
		}
		trimTo := len(head) - (len(head) + len(marker) - maxToolOutputBytes)
		if trimTo <= 0 {
			head = ""
			continue
		}
		head = snapToRuneBoundary(head, 0, trimTo)
		if newline := strings.LastIndexByte(head, '\n'); newline >= 0 {
			head = head[:newline+1]
		}
	}
	marker := toolOutputRecoveryMarkerAt(toolName, toolCallID, resultRef, len(s), len(head), len(head))
	if len(marker) > maxToolOutputBytes {
		marker = snapToRuneBoundary(marker, 0, maxToolOutputBytes)
	}
	notice := fmt.Sprintf(i18n.M.ToolOutputTruncatedFmt, len(s)-len(head), len(s))
	return head + marker, notice
}

func snapToRuneBoundary(s string, lo, hi int) string {
	for lo > 0 && !utf8.RuneStart(s[lo]) {
		lo--
	}
	for hi < len(s) && !utf8.RuneStart(s[hi]) {
		hi++
	}
	return s[lo:hi]
}
