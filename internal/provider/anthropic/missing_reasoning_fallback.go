package anthropic

import (
	"context"

	"reasonix/internal/provider"
)

// SupportsMissingReasoningFallback is deliberately narrower than the generic
// Anthropic adapter. DeepSeek's compatibility endpoint has an explicit
// thinking.type=disabled request mode; native Anthropic tool loops do not.
func (c *client) SupportsMissingReasoningFallback() bool {
	return c.deepSeekThinkingEnabled()
}

func (c *client) missingReasoningFallback(ctx context.Context) bool {
	return c.SupportsMissingReasoningFallback() && provider.MissingReasoningFallbackFromContext(ctx)
}

func (c *client) replayMessages(messages []provider.Message, recoveryWithoutThinking bool) []provider.Message {
	if c.deepseek && !recoveryWithoutThinking {
		messages, _ = provider.ProjectReplaySafeMessages(c, messages)
	}
	return messages
}

func (c *client) replayReasoningBlock(m provider.Message, recoveryWithoutThinking bool) (contentBlock, bool) {
	activity := len(m.ToolCalls) > 0 || len(m.ServerSearch) > 0
	if c.deepseek && !recoveryWithoutThinking && activity && m.ReasoningContent != "" {
		return contentBlock{Type: "thinking", Thinking: m.ReasoningContent}, true
	}
	if !c.deepseek && c.thinking == "adaptive" && m.ReasoningContent != "" && m.ReasoningSignature != "" {
		return contentBlock{Type: "thinking", Thinking: m.ReasoningContent, Signature: m.ReasoningSignature}, true
	}
	return contentBlock{}, false
}

func (c *client) applyDeepSeekThinking(r *anthRequest, req provider.Request, recoveryWithoutThinking bool) {
	r.Temperature = req.Temperature
	t := c.thinking
	if recoveryWithoutThinking {
		t = "disabled"
	} else if t != "disabled" {
		t = "enabled"
	}
	if c.effort == "disabled" {
		t = "disabled"
	}
	r.Thinking = &thinkingConfig{Type: t}
	if t == "disabled" {
		return
	}
	switch effort := normalizeDeepSeekAnthropicEffort(c.model, c.effort); effort {
	case "low", "high", "max":
		r.OutputConfig = &outputConfig{Effort: effort}
	}
}
