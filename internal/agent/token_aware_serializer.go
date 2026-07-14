package agent
import "sync"

// ── OPT-176: TokenAwareSerializer (Token 感知序列化器) ──
// 将消息列表序列化为紧凑格式以减少 token 开销，支持反序列化还原。
// 使用与 format 对应的分隔符连接消息，并通过 len/4 启发式估算 token 数量，
// 累计输入/输出 token 估算值以便统计节省效果。

// TokenAwareSerializer Token 感知序列化器，优化消息序列化以减少 token 开销。
type TokenAwareSerializer struct {
	mu                sync.RWMutex
	format            string
	serializeCount    int
	totalInputTokens  int
	totalOutputTokens int
}

// NewTokenAwareSerializer 创建一个新的 Token 感知序列化器。
// format 指定序列化格式（决定分隔符），空字符串默认为 "compact"。
func NewTokenAwareSerializer(format string) *TokenAwareSerializer {
	if format == "" {
		format = "compact"
	}
	return &TokenAwareSerializer{
		format: format,
	}
}

// Serialize 将消息列表序列化为紧凑格式的单一字符串。
// 消息之间用与 format 对应的分隔符连接，并累计输入/输出 token 估算值。
func (s *TokenAwareSerializer) Serialize(messages []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	delimiter := tasrDelimiter(s.format)
	inputTokens := 0
	var result string
	for i, msg := range messages {
		inputTokens += tasrEstimateTokens(msg)
		if i > 0 {
			result += delimiter
		}
		result += msg
	}

	outputTokens := tasrEstimateTokens(result)
	s.serializeCount++
	s.totalInputTokens += inputTokens
	s.totalOutputTokens += outputTokens

	return result
}

// Deserialize 将序列化后的字符串反序列化为消息列表。
// 依据当前 format 对应的分隔符进行拆分，空输入返回 nil。
func (s *TokenAwareSerializer) Deserialize(data string) []string {
	s.mu.RLock()
	delimiter := tasrDelimiter(s.format)
	s.mu.RUnlock()

	if data == "" {
		return nil
	}

	var messages []string
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == delimiter[0] {
			messages = append(messages, data[start:i])
			start = i + 1
		}
	}
	messages = append(messages, data[start:])
	return messages
}

// EstimateSavings 估算通过序列化节省的 token 数量。
// 返回 inputTokens - outputTokens，若为负则返回 0。
func (s *TokenAwareSerializer) EstimateSavings(inputTokens int, outputTokens int) int {
	saved := inputTokens - outputTokens
	if saved < 0 {
		return 0
	}
	return saved
}

// GetStats 返回序列化器的统计信息，包括 format、serializeCount、
// totalInputTokens、totalOutputTokens 和 totalTokensSaved。
func (s *TokenAwareSerializer) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	saved := s.totalInputTokens - s.totalOutputTokens
	if saved < 0 {
		saved = 0
	}
	return map[string]interface{}{
		"format":            s.format,
		"serializeCount":    s.serializeCount,
		"totalInputTokens":  s.totalInputTokens,
		"totalOutputTokens": s.totalOutputTokens,
		"totalTokensSaved":  saved,
	}
}

// Reset 重置序列化器的所有统计数据，保留 format 配置。
func (s *TokenAwareSerializer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serializeCount = 0
	s.totalInputTokens = 0
	s.totalOutputTokens = 0
}

// ── 辅助函数（tasr 前缀）──

// tasrEstimateTokens 使用 len(text)/4 启发式估算文本的 token 数量。
func tasrEstimateTokens(text string) int {
	return len(text) / 4
}

// tasrDelimiter 根据 format 返回对应的分隔符。
// "compact" 使用 ASCII 单元分隔符 (0x1F)，"pipe" 使用 "|"，"newline" 使用 "\n"。
// 未知的 format 默认使用单元分隔符 (0x1F)。
func tasrDelimiter(format string) string {
	switch format {
	case "pipe":
		return "|"
	case "newline":
		return "\n"
	default:
		return "\x1f"
	}
}
