package boot

import (
	"strconv"
	"strings"

	"reasonix/internal/provider"
)

// finishCompliantBootChunks keeps legacy wiring fixtures focused on their tool
// while honoring the production top-level finish contract.
func finishCompliantBootChunks(req provider.Request, call int, chunks []provider.Chunk) []provider.Chunk {
	if !requestHasTool(req, "finish") {
		return chunks
	}
	doneAt, hasText, hasTool := -1, false, false
	for i, chunk := range chunks {
		switch chunk.Type {
		case provider.ChunkText:
			hasText = hasText || strings.TrimSpace(chunk.Text) != ""
		case provider.ChunkToolCall:
			hasTool = true
		case provider.ChunkDone:
			if doneAt < 0 {
				doneAt = i
			}
		}
	}
	if !hasText || hasTool || doneAt < 0 {
		return chunks
	}
	finish := provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID: "boot-finish-" + strconv.Itoa(call), Name: "finish", Arguments: `{"outcome":"completed"}`,
	}}
	out := append([]provider.Chunk{}, chunks[:doneAt]...)
	out = append(out, finish)
	return append(out, chunks[doneAt:]...)
}
