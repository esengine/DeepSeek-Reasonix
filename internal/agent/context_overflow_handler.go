package agent
import "sync"

// OPT-173: ContextOverflowHandler — 上下文溢出处理器
// ContextOverflowHandler handles situations where the context exceeds the window limit.
// It selects and applies strategies (trim_oldest, summarize, truncate) to reduce tokens.
type ContextOverflowHandler struct {
	mu                 sync.RWMutex
	maxTokens          int      // 最大token限制 maximum token limit
	overflowCount      int      // 溢出次数 number of overflows handled
	totalTrimmedTokens int      // 累计修剪的token总数 total tokens trimmed
	strategies         []string // 可用策略列表 available strategies
}

// NewContextOverflowHandler creates a new ContextOverflowHandler with the given max token limit.
// NewContextOverflowHandler 使用给定的最大token限制创建新的上下文溢出处理器。
func NewContextOverflowHandler(maxTokens int) *ContextOverflowHandler {
	return &ContextOverflowHandler{
		maxTokens:          maxTokens,
		overflowCount:      0,
		totalTrimmedTokens: 0,
		strategies:         []string{"trim_oldest", "summarize", "truncate"},
	}
}

// HandleOverflow processes an overflow by selecting a strategy and trimming messages.
// HandleOverflow 处理溢出，选择策略并修剪消息，返回修剪后的消息和使用的策略。
func (c *ContextOverflowHandler) HandleOverflow(messages []string, estimatedTokens int) ([]string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	excess := estimatedTokens - c.maxTokens
	if excess <= 0 {
		return messages, ""
	}

	strategy := c.SelectStrategy(excess)
	c.overflowCount++

	var result []string
	switch strategy {
	case "trim_oldest":
		// 修剪最旧的消息，直到token数降到限制以内
		// trim oldest messages until tokens are within the limit
		result = messages
		currentTokens := estimatedTokens
		for len(result) > 0 && currentTokens > c.maxTokens {
			trimmed := cohEstimateTokens(result[0])
			currentTokens -= trimmed
			c.totalTrimmedTokens += trimmed
			result = result[1:]
		}
	case "summarize":
		// 将旧消息合并为摘要，保留最新的消息
		// merge old messages into a summary, keep the latest
		if len(messages) <= 1 {
			result = messages
		} else {
			summary := "[summary] "
			for i := 0; i < len(messages)-1; i++ {
				summary += messages[i] + " "
				c.totalTrimmedTokens += cohEstimateTokens(messages[i])
			}
			c.totalTrimmedTokens -= cohEstimateTokens(summary)
			result = []string{summary, messages[len(messages)-1]}
		}
	case "truncate":
		// 截断每条消息以减少token数
		// truncate each message to reduce tokens
		result = make([]string, 0, len(messages))
		remaining := excess
		for _, msg := range messages {
			if remaining > 0 {
				cutLen := len(msg) / 2
				if cutLen > remaining*4 {
					cutLen = remaining * 4
				}
				if cutLen > 0 && cutLen < len(msg) {
					c.totalTrimmedTokens += cohEstimateTokens(msg[cutLen:])
					msg = msg[:cutLen]
					remaining -= cohEstimateTokens(msg)
				}
			}
			result = append(result, msg)
		}
	default:
		result = messages
	}

	return result, strategy
}

// SelectStrategy selects a strategy based on the excess token amount.
// SelectStrategy 根据超出的token量选择策略。
// excess < 100: truncate, < 500: summarize, >= 500: trim_oldest
func (c *ContextOverflowHandler) SelectStrategy(excess int) string {
	if excess < 100 {
		return "truncate"
	}
	if excess < 500 {
		return "summarize"
	}
	return "trim_oldest"
}

// GetOverflowCount returns the number of overflows handled.
// GetOverflowCount 返回已处理的溢出次数。
func (c *ContextOverflowHandler) GetOverflowCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.overflowCount
}

// GetStats returns statistics about the overflow handler.
// GetStats 返回溢出处理器的统计信息。
func (c *ContextOverflowHandler) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"maxTokens":          c.maxTokens,
		"overflowCount":      c.overflowCount,
		"totalTrimmedTokens": c.totalTrimmedTokens,
		"strategyCount":      len(c.strategies),
	}
}

// Reset resets the overflow handler to its initial state (preserving maxTokens and strategies).
// Reset 将溢出处理器重置为初始状态（保留maxTokens和strategies配置）。
func (c *ContextOverflowHandler) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.overflowCount = 0
	c.totalTrimmedTokens = 0
}

// cohEstimateTokens estimates the number of tokens in a string (len/4).
// cohEstimateTokens 估算字符串中的token数（长度除以4）。
func cohEstimateTokens(s string) int {
	return len(s) / 4
}
