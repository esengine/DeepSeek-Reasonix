package openai

// applyThinkingProfile sets the provider-specific thinking and output-budget
// fields on a chat request. Each case is one backend whose wire shape deviates
// from the generic OpenAI-compatible contract.
func (c *client) applyThinkingProfile(out *chatRequest, maxOutputTokens int) {
	switch {
	case c.kimiK3:
		// K3 fixes its sampling values and recommends omitting them. It also
		// names the output budget max_completion_tokens rather than max_tokens.
		out.Temperature = nil
		out.MaxTokens = 0
		out.MaxCompletionTokens = maxOutputTokens
		out.ExtraBody = omitExtraBodyFields(out.ExtraBody,
			"temperature", "top_p", "n", "presence_penalty", "frequency_penalty", "max_completion_tokens")
	case IsOpenAI(c.baseURL):
		// OpenAI's current Chat Completions contract replaces max_tokens with
		// max_completion_tokens, which includes visible and reasoning tokens and
		// is required by o-series models. Compatible gateways retain max_tokens.
		out.MaxTokens = 0
		out.MaxCompletionTokens = maxOutputTokens
	case c.deepseek:
		// DeepSeek's CoT is controlled by `thinking` plus `reasoning_effort` for
		// depth. Thinking is on by default but can be turned off via
		// effort=disabled / thinking=disabled (credit @eghrhegpe, #5063).
		if c.thinkingType == "disabled" {
			out.Thinking = &thinkingMode{Type: "disabled"}
		} else {
			out.Thinking = &thinkingMode{Type: "enabled"}
		}
	case c.minimax:
		// M3 uses a single `thinking.type` field with two valid values:
		// "adaptive" (default, thinking on) and "disabled" (off). Reasoning
		// depth is not a knob on M3, so reasoning_effort is omitted entirely.
		t := c.effort
		if t == "" {
			t = "adaptive" // /effort auto == the M3 model default
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.zhipu:
		// Zhipu GLM's binary thinking knob: "enabled" (default, thinking on) or
		// "disabled". reasoning_effort is silently ignored by the endpoint, so we
		// omit it and drive chain-of-thought purely through thinking.type.
		t := c.effort
		if t == "" {
			t = "enabled" // auto == the GLM default (thinking on)
		}
		if c.thinkingType != "" {
			t = c.thinkingType // explicit `thinking` config overrides the effort knob
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.longcat:
		// LongCat's binary thinking knob: "enabled" (default, thinking on) or
		// "disabled". The API documents reasoning_content in OpenAI responses but
		// not reasoning_effort, so keep depth out of the request.
		t := c.effort
		if t == "" {
			t = c.thinkingType
		}
		if t == "" {
			t = "enabled"
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.thinkingType != "":
		// Generic OpenAI-compatible provider with an explicit `thinking` config
		// field (e.g. opencode.ai) — emit thinking.type; reasoning_effort, if any,
		// is left untouched for backends that also honour it.
		out.Thinking = &thinkingMode{Type: c.thinkingType}
	}
}
