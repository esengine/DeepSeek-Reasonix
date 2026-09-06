package anthropic

import "reasonix/internal/provider"

func updateThinkingStream(blocks map[int]*provider.ThinkingBlock, ev streamEvent, send func(provider.Chunk) bool) bool {
	chunk := provider.Chunk{Type: provider.ChunkReasoning}
	switch ev.Type {
	case "content_block_start":
		b := ev.ContentBlock
		if b == nil || b.Type != "thinking" && b.Type != "redacted_thinking" {
			return true
		}
		blocks[ev.Index] = &provider.ThinkingBlock{Type: b.Type, Thinking: b.Thinking, Signature: b.Signature, Data: b.Data}
		chunk.Text, chunk.Signature, chunk.ReasoningState = b.Thinking, b.Signature, provider.ReasoningIncomplete
	case "content_block_delta":
		if ev.Delta == nil {
			return true
		}
		b := blocks[ev.Index]
		switch ev.Delta.Type {
		case "thinking_delta":
			if b != nil {
				b.Thinking += ev.Delta.Thinking
			}
			chunk.Text = ev.Delta.Thinking
		case "signature_delta":
			if b != nil {
				b.Signature += ev.Delta.Signature
			}
			chunk.Signature = thinkingSignature(b, ev.Delta.Signature)
		default:
			return true
		}
	case "content_block_stop":
		b := blocks[ev.Index]
		if b == nil {
			return true
		}
		chunk.ThinkingBlock, chunk.ReasoningState = b, provider.ReasoningComplete
		delete(blocks, ev.Index)
	default:
		return true
	}
	return send(chunk)
}
