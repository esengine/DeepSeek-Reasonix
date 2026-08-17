package anthropic

import "reasonix/internal/provider"

// OutputBudget reports the mandatory max_tokens default used by this client.
func (c *client) OutputBudget() int { return c.defaultMaxTokens }

// SharesContextWindow is true only for the official DeepSeek endpoint mode.
func (c *client) SharesContextWindow() bool { return c.deepseek }

func (c *client) ContextBudgetPolicy() provider.ContextBudgetPolicy {
	if lim, ok := provider.LookupOfficialOpenCodeGo("anthropic", c.baseURL, c.model); ok {
		return provider.ContextBudgetPolicy{
			WindowMode:       provider.ContextWindowShared,
			AutoOutputTokens: lim.MaxOutput,
			MaxOutputTokens:  lim.MaxOutput,
			LimitMode:        provider.OutputLimitRequired,
		}
	}
	if c.deepseek {
		return provider.ContextBudgetPolicy{
			WindowMode:       provider.ContextWindowShared,
			AutoOutputTokens: provider.DeepSeekMaxOutputTokens,
			MaxOutputTokens:  provider.DeepSeekMaxOutputTokens,
			LimitMode:        provider.OutputLimitRequired,
		}
	}
	return provider.ContextBudgetPolicy{WindowMode: provider.ContextWindowUnknown, LimitMode: provider.OutputLimitRequired}
}
