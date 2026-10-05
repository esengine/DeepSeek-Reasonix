package anthropic

// OutputBudget reports the mandatory max_tokens default used by this client.
func (c *client) OutputBudget() int { return c.defaultMaxTokens }

// SharesContextWindow is true only for the official DeepSeek endpoint mode.
func (c *client) SharesContextWindow() bool { return c.deepseek }
