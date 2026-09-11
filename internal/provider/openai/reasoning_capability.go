package openai

import (
	"reasonix/internal/provider"
)

// ReasoningForConfig is pure: capability discovery never reads credentials or
// performs I/O. It shares the adapter's endpoint and protocol predicates.
func ReasoningForConfig(cfg provider.Config) provider.ReasoningCapability {
	cfg = provider.ApplyOpenCodeGoContract("openai", cfg)
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	protocol = normalizeReasoningProtocol(protocol)
	if protocol == "none" {
		return provider.ReasoningOptions("")
	}
	var cap provider.ReasoningCapability
	switch {
	case usesKimiK3Contract(protocol, cfg.BaseURL, cfg.Model):
		return provider.ReasoningOptions("max", "low", "high", "max")
	case protocol == "glm" || (protocol == "" && (IsZhipu(cfg.BaseURL) || IsLongCat(cfg.BaseURL))):
		cap = provider.ReasoningOptions("enabled", "enabled", "disabled")
	case protocol == "" && IsMiniMax(cfg.BaseURL):
		cap = provider.ReasoningOptions("adaptive", "adaptive", "disabled")
	case protocol == "deepseek" || (protocol == "" && IsDeepSeek(cfg.BaseURL)):
		cap = provider.ReasoningOptions("high", "disabled", "high", "max")
		if cfg.Model == "deepseek-v4-flash" || cfg.Model == "deepseek-v4-pro" || IsOfficialDeepSeekVisionModel(cfg.Model) {
			cap = provider.ReasoningOptions("high", "disabled", "low", "high", "max")
		}
	case protocol == "" && IsOllamaCloud(cfg.BaseURL):
		cap = provider.ReasoningOptions("", "none", "low", "medium", "high", "max")
	case protocol == "openai" || (protocol == "" && IsMiMo(cfg.BaseURL)):
		cap = provider.ReasoningOptions("", "low", "medium", "high")
	default:
		cap = provider.ReasoningOptions("")
	}
	cap = provider.DeclaredReasoning(cfg, cap)
	if protocol == "glm" || (protocol == "" && (IsZhipu(cfg.BaseURL) || IsLongCat(cfg.BaseURL))) {
		cap = provider.RestrictReasoning(cap, "enabled", "disabled")
	}
	if protocol == "" && IsMiniMax(cfg.BaseURL) {
		cap = provider.RestrictReasoning(cap, "adaptive", "disabled")
	}
	if configuredThinkingType(cfg) == "disabled" {
		return provider.ReasoningOptions("disabled", "disabled")
	}
	return cap
}
func (c *client) ReasoningCapability() provider.ReasoningCapability { return c.reasoning.Clone() }

func configuredEffort(cfg provider.Config) (string, error) {
	effort, _ := cfg.Extra["effort"].(string)
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	cap := ReasoningForConfig(cfg)
	if effort == "auto" || effort == "off" || protocol == "none" || configuredThinkingType(cfg) == "disabled" {
		return effort, nil
	}
	return effort, cap.Validate(cfg.Model, effort)
}

// reasoningState is the immutable reasoning contract resolved at construction.
type reasoningState struct {
	ollamaCloud    bool
	thinkingLocked bool
	reasoning      provider.ReasoningCapability
}

func (c *client) applyReasoning(out *chatRequest, req provider.Request) {
	maxOutputTokens := out.MaxTokens
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
		// depth. Thinking is on by default but can be turned off for one
		// stateless request through EffortOverride=disabled.
		out.Thinking = &thinkingMode{Type: c.deepSeekRequestThinking(req)}
		if out.Thinking.Type == "disabled" {
			out.ReasoningEffort = ""
		}
	case c.minimax:
		// M3 uses a single `thinking.type` field with two valid values:
		// "adaptive" (default, thinking on) and "disabled" (off). Reasoning
		// depth is not a knob on M3, so reasoning_effort is omitted entirely.
		t := c.requestEffort(req)
		if t == "" {
			t = "adaptive" // /effort auto == the M3 model default
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.zhipu:
		// Zhipu GLM's binary thinking knob: "enabled" (default, thinking on) or
		// "disabled". reasoning_effort is silently ignored by the endpoint, so we
		// omit it and drive chain-of-thought purely through thinking.type.
		t := c.requestEffort(req)
		if t == "" {
			t = "enabled" // auto == the GLM default (thinking on)
		}
		if c.thinkingType != "" && req.EffortOverride == "" {
			t = c.thinkingType // explicit `thinking` config overrides the effort knob
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.longcat:
		// LongCat's binary thinking knob: "enabled" (default, thinking on) or
		// "disabled". The API documents reasoning_content in OpenAI responses but
		// not reasoning_effort, so keep depth out of the request.
		t := c.requestEffort(req)
		if t == "" {
			t = c.thinkingType
		}
		if t == "" {
			t = "enabled"
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
	case c.ollamaCloud:
		if out.ReasoningEffort == "none" {
			out.ReasoningEffort = ""
		}
	case c.thinkingType != "":
		// Generic OpenAI-compatible provider with an explicit `thinking` config
		// field (e.g. opencode.ai) — emit thinking.type; reasoning_effort, if any,
		// is left untouched for backends that also honour it.
		out.Thinking = &thinkingMode{Type: c.thinkingType}
	}
}
