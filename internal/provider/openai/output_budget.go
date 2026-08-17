package openai

import "reasonix/internal/provider"

// OutputBudget reports the default total output budget sent by this client.
func (c *client) OutputBudget() int { return c.maxOutputTokens }

// SharesContextWindow is true only for the recognized DeepSeek protocol mode.
func (c *client) SharesContextWindow() bool { return c.deepseek }

func (c *client) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	if lim, ok := provider.LookupOfficialOpenCodeGo("openai", c.baseURL, c.model); ok {
		return provider.ContextBudgetPolicy{
			WindowMode:       provider.ContextWindowShared,
			AutoOutputTokens: lim.MaxOutput,
			MaxOutputTokens:  lim.MaxOutput,
			LimitMode:        provider.OutputLimitAlways,
		}
	}
	if IsDeepSeek(c.baseURL) {
		return provider.ContextBudgetPolicy{
			WindowMode:       provider.ContextWindowShared,
			AutoOutputTokens: provider.DeepSeekMaxOutputTokens,
			MaxOutputTokens:  provider.DeepSeekMaxOutputTokens,
			LimitMode:        provider.OutputLimitOmitWhenSafe,
		}
	}
	if c.kimiK3 && IsKimiAPI(c.baseURL) {
		return provider.ContextBudgetPolicy{
			WindowMode:       provider.ContextWindowShared,
			AutoOutputTokens: 131_072,
			MaxOutputTokens:  131_072,
			LimitMode:        provider.OutputLimitAlways,
		}
	}
	return provider.ContextBudgetPolicy{WindowMode: provider.ContextWindowUnknown, LimitMode: provider.OutputLimitOmitWhenSafe}
}
