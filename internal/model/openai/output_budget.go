package openai

// OutputBudget reports the default total output budget sent by this client.
func (c *client) OutputBudget() int { return c.maxOutputTokens }

// SharesContextWindow is true only for the recognized DeepSeek protocol mode.
func (c *client) SharesContextWindow() bool { return c.deepseek }
