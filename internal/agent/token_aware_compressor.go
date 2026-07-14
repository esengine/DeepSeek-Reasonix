package agent

import "sync"

// ── OPT-120: TokenAwareCompressor (Token 感知压缩器) ──
// 根据 token 预算动态压缩内容，支持三种压缩策略：
//   - no_compression: 预算充足时不压缩
//   - aggressive: 预算紧张时激进压缩（只保留前 100 字符 + "..."）
//   - light: 预算适中时轻度压缩（去多余空格 + 截断到 budget/4 长度）
//
// 原理：根据可用预算与内容长度的比值选择压缩强度。预算充裕时保留
// 完整内容以保证信息完整性；预算极度紧张时只保留关键前缀；预算适中
// 时通过去除冗余空格和适度截断平衡信息保留与预算约束。
//
// 效果：在 token 预算约束下最大化信息保留率，同时统计压缩比与策略
// 使用分布，为预算分配策略提供反馈。

// TokenAwareCompressor Token 感知压缩器
type TokenAwareCompressor struct {
	mu                    sync.RWMutex
	totalCompressed       int
	totalTokensBefore     int
	totalTokensAfter      int
	compressionStrategies map[string]int
	budgetThreshold       int
}

// NewTokenAwareCompressor 创建 Token 感知压缩器。
// budgetThreshold 指定默认预算阈值，用于判断是否需要压缩。
func NewTokenAwareCompressor(budgetThreshold int) *TokenAwareCompressor {
	return &TokenAwareCompressor{
		compressionStrategies: make(map[string]int),
		budgetThreshold:       budgetThreshold,
	}
}

// Compress 根据 availableBudget 压缩内容。
// 压缩策略：
//   - budget > len(content)*4: 不压缩（no_compression）
//   - budget < len(content)/2: 激进压缩（aggressive），只保留前 100 字符 + "..."
//   - 否则: 轻度压缩（light），去除多余空格并截断到 budget/4 长度
//
// 每次调用递增 totalCompressed，并更新 token 前后统计与策略使用计数。
func (c *TokenAwareCompressor) Compress(content string, availableBudget int) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalCompressed++
	tokensBefore := tacEstimateTokens(content)
	c.totalTokensBefore += tokensBefore

	contentLen := len(content)

	var result string
	var strategy string

	switch {
	case availableBudget > contentLen*4:
		// 预算充足，不压缩
		strategy = "no_compression"
		result = content

	case availableBudget < contentLen/2:
		// 预算紧张，激进压缩
		strategy = "aggressive"
		result = tacAggressiveCompress(content)

	default:
		// 预算适中，轻度压缩
		strategy = "light"
		result = tacLightCompress(content, availableBudget)
	}

	c.compressionStrategies[strategy]++
	c.totalTokensAfter += tacEstimateTokens(result)

	return result
}

// RecordCompression 记录一次压缩操作的统计信息。
// strategy 为压缩策略名称，tokensBefore/tokensAfter 为压缩前后的 token 数。
// 用于记录外部压缩操作（非通过 Compress 方法执行的压缩）。
func (c *TokenAwareCompressor) RecordCompression(strategy string, tokensBefore int, tokensAfter int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalCompressed++
	c.totalTokensBefore += tokensBefore
	c.totalTokensAfter += tokensAfter
	c.compressionStrategies[strategy]++
}

// GetCompressionRatio 返回压缩比（totalTokensAfter / totalTokensBefore）。
// 若 totalTokensBefore 为 0 则返回 1.0（表示无压缩）。
func (c *TokenAwareCompressor) GetCompressionRatio() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.totalTokensBefore == 0 {
		return 1.0
	}
	return float64(c.totalTokensAfter) / float64(c.totalTokensBefore)
}

// GetStats 返回压缩器的统计信息，包括 totalCompressed、
// totalTokensBefore、totalTokensAfter、compressionRatio 和 strategies。
func (c *TokenAwareCompressor) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var ratio float64
	if c.totalTokensBefore == 0 {
		ratio = 1.0
	} else {
		ratio = float64(c.totalTokensAfter) / float64(c.totalTokensBefore)
	}

	// 复制 strategies map 避免外部修改
	strategiesCopy := make(map[string]int, len(c.compressionStrategies))
	for k, v := range c.compressionStrategies {
		strategiesCopy[k] = v
	}

	return map[string]interface{}{
		"totalCompressed":   c.totalCompressed,
		"totalTokensBefore": c.totalTokensBefore,
		"totalTokensAfter":  c.totalTokensAfter,
		"compressionRatio":  ratio,
		"strategies":        strategiesCopy,
	}
}

// Reset 重置压缩器，清除所有统计与策略记录。
func (c *TokenAwareCompressor) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalCompressed = 0
	c.totalTokensBefore = 0
	c.totalTokensAfter = 0
	c.compressionStrategies = make(map[string]int)
}

// ---------------------------------------------------------------------------
// 内部辅助函数（以 tac 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tacEstimateTokens 粗略估算字符串的 token 数（约 4 字符/token）。
func tacEstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4
}

// tacAggressiveCompress 激进压缩：只保留前 100 字符并追加 "..."。
func tacAggressiveCompress(content string) string {
	if len(content) <= 100 {
		return content
	}
	return content[:100] + "..."
}

// tacLightCompress 轻度压缩：去除多余空格并截断到 maxLen 长度。
// maxLen = availableBudget / 4。
func tacLightCompress(content string, availableBudget int) string {
	// 去除多余空格（连续空白压缩为单个空格）
	compressed := tacSqueezeSpaces(content)

	// 截断到 budget/4 长度
	maxLen := availableBudget / 4
	if maxLen <= 0 {
		return compressed
	}
	if len(compressed) > maxLen {
		compressed = compressed[:maxLen]
	}
	return compressed
}

// tacSqueezeSpaces 将连续的空白字符压缩为单个空格。
func tacSqueezeSpaces(s string) string {
	if len(s) == 0 {
		return s
	}

	result := make([]byte, 0, len(s))
	inSpace := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if !inSpace {
				result = append(result, ' ')
				inSpace = true
			}
		} else {
			result = append(result, ch)
			inSpace = false
		}
	}
	return string(result)
}
