// Reasoning contract resolution: which thinking fields this endpoint accepts.
package anthropic

import (
	"encoding/json"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/model/openai"
)

// reasoningProtocolAnthropic is the config-declared name for Anthropic's own
// extended-thinking contract; see config.ReasoningProtocolAnthropic.
const reasoningProtocolAnthropic = "anthropic"

// reasoningProtocolDeepSeek is the config-declared name for DeepSeek's
// Anthropic-compatible contract; see config.ReasoningProtocolDeepSeek.
const reasoningProtocolDeepSeek = "deepseek"

// speaksDeepSeekContract reports whether the endpoint takes DeepSeek's shape of
// the Messages API: unsigned thinking blocks replayed on tool-call turns,
// thinking.type, output_config.effort, and no cache_control. The official host
// implies it; a relay carrying the same models can only declare it, because
// nothing on the wire tells one apart before a request is refused.
func speaksDeepSeekContract(root string, extra map[string]any) bool {
	return openai.IsDeepSeek(root) ||
		lowerExtra(extra, "reasoning_protocol") == reasoningProtocolDeepSeek
}

// replayThinking returns the thinking block to put back before m's tool_use,
// or nil when this endpoint's contract has no place for it.
func (c *client) replayThinking(m provider.Message) *contentBlock {
	switch {
	case c.deepseek && len(m.ToolCalls) > 0 && (m.ReasoningContent != "" || c.deepSeekThinkingEnabled()):
		// DeepSeek wants a tool-call turn's reasoning in every later request,
		// even once this one declares no tools or thinking has been turned off.
		// In thinking mode it refuses the turn with no block and accepts an empty one.
		return &contentBlock{Type: "thinking", Thinking: m.ReasoningContent}
	case c.thinking == "adaptive" && m.ReasoningContent != "" && m.ReasoningSignature != "":
		return &contentBlock{Type: "thinking", Thinking: m.ReasoningContent, Signature: m.ReasoningSignature}
	}
	return nil
}

// MarshalJSON keeps "thinking" on a thinking block even when empty: DeepSeek
// refuses a thinking block that lacks the field.
func (b contentBlock) MarshalJSON() ([]byte, error) {
	type plain contentBlock
	if b.Type != "thinking" || b.Thinking != "" {
		return json.Marshal(plain(b))
	}
	return json.Marshal(struct {
		plain
		Thinking string `json:"thinking"`
	}{plain(b), ""})
}

// resolveReasoning turns the configured knobs into the thinking mode and effort
// this endpoint's contract accepts. Anthropic's own fields — adaptive thinking,
// display, output_config.effort — need a first-party endpoint or a declared
// protocol; every other gateway answers the plain enabled|disabled toggle.
func resolveReasoning(root string, extra map[string]any) (thinking, effort string) {
	thinking = lowerExtra(extra, "thinking")
	effort = lowerExtra(extra, "effort")
	depth := provider.IsNativeAnthropic(root) ||
		lowerExtra(extra, "reasoning_protocol") == reasoningProtocolAnthropic
	switch {
	case !depth && thinking == "adaptive":
		// A value only Anthropic's contract defines, configured before this
		// endpoint's was known. Fall back to the provider default instead of
		// guessing a toggle spelling the relay may reject just as hard.
		thinking = ""
	case depth && thinking == "" && depthEffort(effort):
		// Depth means nothing without extended thinking, so the level engages
		// it — nothing outside this package writes a mode into the user's
		// config to make /effort work.
		thinking = "adaptive"
	}
	return thinking, effort
}

// depthEffort reports whether a level names a reasoning depth rather than the
// binary thinking toggle a compatible gateway takes.
func depthEffort(effort string) bool {
	switch effort {
	case "", "enabled", "disabled", "adaptive", "none", "off":
		return false
	default:
		return true
	}
}

func lowerExtra(extra map[string]any, key string) string {
	value, _ := extra[key].(string)
	return strings.ToLower(strings.TrimSpace(value))
}
