package anthropic

import (
	"encoding/json"
	"strings"

	"reasonix/internal/provider"
)

func (c *client) replayMessages(messages []provider.Message) []provider.Message {
	messages, _ = provider.ProjectReplaySafeMessages(c, messages)
	return messages
}

func (c *client) replayReasoningBlock(m provider.Message) (contentBlock, bool) {
	// DeepSeek's thinking mode requires every historical assistant turn's
	// thinking block to be passed back whenever the request declares tools —
	// including plain question-answer turns with no tool activity.
	if c.deepseek && m.ReasoningContent != "" {
		return contentBlock{Type: "thinking", Thinking: m.ReasoningContent}, true
	}
	if !c.deepseek && c.replaysReceivedThinking() && m.ReasoningSignature != "" {
		return contentBlock{Type: "thinking", Thinking: m.ReasoningContent, Signature: m.ReasoningSignature}, true
	}
	if !c.nativeAnthropic && !c.deepseek && c.replaysReceivedThinking() && m.ReasoningContent != "" {
		return contentBlock{Type: "thinking", Thinking: m.ReasoningContent}, true
	}
	return contentBlock{}, false
}

// mergeThinkingFirst merges appended blocks into an existing same-role
// message's content. A thinking block must lead its message, so a leading
// thinking run in the appended blocks moves to the front: after projection
// degrades a reasoning-less turn to plain text, a following healthy assistant
// turn merging into it must stay [thinking, text, ...], never [text, ...].
func mergeThinkingFirst(existing, blocks []contentBlock) []contentBlock {
	head := leadingThinkingBlocks(blocks)
	if head == 0 || len(existing) == 0 {
		return append(existing, blocks...)
	}
	merged := make([]contentBlock, 0, len(existing)+len(blocks))
	merged = append(merged, blocks[:head]...)
	merged = append(merged, existing...)
	merged = append(merged, blocks[head:]...)
	return merged
}

// leadingThinkingBlocks counts the signed or redacted blocks at the head.
func leadingThinkingBlocks(blocks []contentBlock) int {
	head := 0
	for head < len(blocks) && (blocks[head].Type == "thinking" || blocks[head].Type == "redacted_thinking") {
		head++
	}
	return head
}

func (c *client) applyDeepSeekThinking(r *anthRequest, req provider.Request) {
	r.Temperature = req.Temperature
	t := c.thinking
	if t != "disabled" {
		t = "enabled"
	}
	if c.effort == "disabled" {
		t = "disabled"
	}
	effort := normalizeDeepSeekAnthropicEffort(c.model, c.effort)
	switch override := strings.ToLower(strings.TrimSpace(req.EffortOverride)); override {
	case "disabled":
		t = "disabled"
	case "":
	default:
		if normalized := normalizeDeepSeekAnthropicEffort(c.model, override); normalized != "" {
			effort = normalized
		}
	}
	r.Thinking = &thinkingConfig{Type: t}
	if t == "disabled" {
		return
	}
	switch effort {
	case "low", "high", "max":
		r.OutputConfig = &outputConfig{Effort: effort}
	}
}

func (c *client) ReasoningReplayCapabilities() provider.ReasoningReplayCapabilities {
	if c.deepseek {
		return provider.ReasoningReplayCapabilities{Format: "anthropic-thinking"}
	}
	return provider.ReasoningReplayCapabilities{Format: "anthropic-thinking", RequireSignature: c.nativeAnthropic}
}

func (c *client) replayReasoningBlocks(m provider.Message) []contentBlock {
	if !c.deepseek && c.replaysReceivedThinking() && len(m.ThinkingBlocks) > 0 {
		blocks := make([]contentBlock, 0, len(m.ThinkingBlocks))
		for _, b := range m.ThinkingBlocks {
			blocks = append(blocks, contentBlock{Type: b.Type, Thinking: b.Thinking, Signature: b.Signature, Data: b.Data})
		}
		return blocks
	}
	if block, ok := c.replayReasoningBlock(m); ok {
		return []contentBlock{block}
	}
	return nil
}

func (b contentBlock) MarshalJSON() ([]byte, error) {
	type plain contentBlock
	if b.Type == "thinking" && b.Thinking == "" {
		return json.Marshal(struct {
			plain
			Thinking string `json:"thinking"`
		}{plain(b), b.Thinking})
	}
	return json.Marshal(plain(b))
}

func thinkingSignature(b *provider.ThinkingBlock, delta string) string {
	if b != nil {
		return b.Signature
	}
	return delta
}

func (c *client) replaysSignedThinking() bool {
	return c.thinking == "adaptive" || (c.nativeAnthropic && c.thinking == "enabled")
}

func (c *client) replaysReceivedThinking() bool {
	return c.replaysSignedThinking() || (!c.nativeAnthropic && c.thinking == "enabled" && c.effort != "disabled")
}
