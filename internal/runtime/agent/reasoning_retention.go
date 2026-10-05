package agent

import (
	"strings"
	"unicode/utf8"
)

// defaultReasoningByteLimit bounds retained hidden reasoning, not generation:
// the provider's output budget already ends a runaway stream.
const defaultReasoningByteLimit = 8 << 20

// retainReasoning appends the part of chunk inside the stream's first limit
// bytes, cut on a rune boundary; seen counts the bytes streamed before chunk.
func retainReasoning(b *strings.Builder, chunk string, seen, limit int) {
	if limit > 0 {
		if seen >= limit {
			return
		}
		if room := limit - seen; len(chunk) > room {
			for room > 0 && !utf8.RuneStart(chunk[room]) {
				room--
			}
			chunk = chunk[:room]
		}
	}
	b.WriteString(chunk)
}
