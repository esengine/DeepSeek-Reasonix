package anthropic

import (
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

func ReasoningForConfig(cfg provider.Config) provider.ReasoningCapability {
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	if protocol == "none" {
		return provider.ReasoningOptions("")
	}
	var cap provider.ReasoningCapability
	if protocol == "deepseek" || openai.IsDeepSeek(cfg.BaseURL) {
		cap = provider.ReasoningOptions("high", "disabled", "low", "high", "max")
	} else if thinking, _ := cfg.Extra["thinking"].(string); thinking == "enabled" || thinking == "disabled" {
		cap = provider.ReasoningOptions(thinking, "enabled", "disabled")
	} else {
		cap = provider.ReasoningOptions("", "low", "medium", "high", "xhigh", "max")
	}
	cap = provider.DeclaredReasoning(cfg, cap)
	if thinking, _ := cfg.Extra["thinking"].(string); thinking == "disabled" {
		return provider.ReasoningOptions("disabled", "disabled")
	}
	if thinking, _ := cfg.Extra["thinking"].(string); thinking == "enabled" && protocol != "deepseek" && !openai.IsDeepSeek(cfg.BaseURL) {
		cap = provider.RestrictReasoning(cap, "enabled", "disabled")
	}
	return cap
}
func (c *client) ReasoningCapability() provider.ReasoningCapability { return c.reasoning.Clone() }

// configuredEffort validates the adapter input after config compatibility normalization.
func configuredEffort(cfg provider.Config) (string, error) {
	effort, _ := cfg.Extra["effort"].(string)
	if effort == "auto" || effort == "off" {
		return "", nil
	}
	return effort, ReasoningForConfig(cfg).Validate(cfg.Model, effort)
}

func (c *client) RequiresToolCallReasoning() bool {
	return c.deepSeekThinkingEnabled()
}
