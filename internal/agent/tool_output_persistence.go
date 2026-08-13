package agent

import (
	"fmt"
	"strings"
)

// maxPersistedToolOutputBytes bounds the complete tool-result copy kept in the
// canonical transcript. Content remains the smaller model-facing view; this
// second bound prevents one tool call from making the session event log grow
// without limit while retaining enough head/tail evidence for history and
// compaction.
const maxPersistedToolOutputBytes = 256 * 1024

const maxPersistedToolLabelBytes = 128

func persistedToolOutput(original, toolName, toolCallID string) string {
	if len(original) <= maxPersistedToolOutputBytes {
		return original
	}

	const markerReserve = 320
	keep := maxPersistedToolOutputBytes - markerReserve
	headKeep := keep * 2 / 3
	tailKeep := keep - headKeep
	head := snapToRuneBoundary(original, 0, headKeep)
	tail := snapToRuneBoundary(original, len(original)-tailKeep, len(original))
	marker := fmt.Sprintf(
		"\n\n…[persisted tool output bounded tool=%s call_id=%s original_bytes=%d kept_bytes=%d; middle omitted from canonical transcript]…\n\n",
		nonEmptyLabel(toolName, "tool"), nonEmptyLabel(toolCallID, "-"), len(original), len(head)+len(tail),
	)

	result := head + marker + tail
	if len(result) <= maxPersistedToolOutputBytes {
		return result
	}
	// The marker itself is intentionally bounded, but rune-safe trimming here
	// keeps the hard byte ceiling true if labels are unusually long.
	overflow := len(result) - maxPersistedToolOutputBytes
	trimHead := overflow / 2
	trimTail := overflow - trimHead
	if trimHead < len(head) {
		head = snapToRuneBoundary(head, 0, len(head)-trimHead)
	}
	if trimTail < len(tail) {
		tail = snapToRuneBoundary(tail, trimTail, len(tail))
	}
	return head + marker + tail
}

func nonEmptyLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if len(value) <= maxPersistedToolLabelBytes {
		return value
	}
	return snapToRuneBoundary(value, 0, maxPersistedToolLabelBytes)
}
