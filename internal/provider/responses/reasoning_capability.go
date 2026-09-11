package responses

import "reasonix/internal/provider"

func ReasoningForConfig(cfg provider.Config) provider.ReasoningCapability {
	cfg = provider.ApplyOpenCodeGoContract("responses", cfg)
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	if protocol == "none" {
		return provider.ReasoningOptions("")
	}
	cap := provider.ReasoningOptions("")
	if protocol == "deepseek" {
		return provider.DeclaredReasoning(cfg, provider.ReasoningOptions("high", "none", "low", "high", "max"))
	}
	switch DetectVendor(cfg.BaseURL) {
	case "deepseek":
		cap = provider.ReasoningOptions("high", "none", "low", "high", "max")
	case "mimo":
		cap = provider.ReasoningOptions("", "none", "low", "medium", "high")
	default:
		if protocol == "openai" {
			cap = provider.ReasoningOptions("", "low", "medium", "high")
		}
	}
	return provider.DeclaredReasoning(cfg, cap)
}
func (c *client) ReasoningCapability() provider.ReasoningCapability { return c.reasoning.Clone() }
