package openai

import (
	"context"
	"reasonix/internal/provider"
)

func (c *client) ReasoningReplayCapabilities() provider.ReasoningReplayCapabilities {
	fallback := ""
	if c.AllowsEmptyReasoningFallback() {
		fallback = "empty-field"
	}
	return provider.ReasoningReplayCapabilities{Format: "chat-completions", EmptyFallback: fallback}
}

func emitChatReasoning(ctx context.Context, out chan<- provider.Chunk, content *string, fallback string) (bool, error) {
	text := fallback
	if content != nil && *content != "" {
		text = *content
	}
	if content == nil && text == "" {
		return false, nil
	}
	chunk := provider.Chunk{Type: provider.ChunkReasoning, Text: text}
	if text == "" {
		chunk.ReasoningState = provider.ReasoningEmpty
	}
	if !sendChunk(ctx, out, chunk) {
		return text != "", ctx.Err()
	}
	return text != "", nil
}
