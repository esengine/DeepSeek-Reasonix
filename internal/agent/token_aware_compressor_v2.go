package agent
import "sync"

// OPT-174: TokenAwareCompressorV2 — Token感知压缩器V2
// TokenAwareCompressorV2 combines multiple compression strategies to optimize token usage.
// It applies whitespace removal, redundancy elimination, and abbreviation substitution.
type TokenAwareCompressorV2 struct {
	mu                    sync.RWMutex
	strategies            []string // 压缩策略列表 compression strategies
	compressCount         int      // 压缩次数 number of compressions
	totalTokensSaved      int      // 累计节省的token总数 total tokens saved
	avgCompressionRatio   float64  // 平均压缩比率 average compression ratio
}

// NewTokenAwareCompressorV2 creates a new TokenAwareCompressorV2.
// NewTokenAwareCompressorV2 创建新的Token感知压缩器V2。
func NewTokenAwareCompressorV2() *TokenAwareCompressorV2 {
	return &TokenAwareCompressorV2{
		strategies:          []string{"whitespace", "redundancy", "abbreviation"},
		compressCount:       0,
		totalTokensSaved:    0,
		avgCompressionRatio: 0.0,
	}
}

// Compress applies all compression strategies to the given text.
// Compress 对给定文本应用所有压缩策略。
func (t *TokenAwareCompressorV2) Compress(text string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	original := text
	for _, strategy := range t.strategies {
		text = t.compressWithStrategyLocked(text, strategy)
	}

	t.compressCount++
	originalTokens := tac2EstimateTokens(original)
	compressedTokens := tac2EstimateTokens(text)
	saved := originalTokens - compressedTokens
	if saved > 0 {
		t.totalTokensSaved += saved
	}

	ratio := t.GetCompressionRatioLocked(original, text)
	if t.compressCount > 0 {
		t.avgCompressionRatio = (t.avgCompressionRatio*float64(t.compressCount-1) + ratio) / float64(t.compressCount)
	}

	return text
}

// CompressWithStrategy applies a single specified compression strategy to the given text.
// CompressWithStrategy 对给定文本应用指定的单一压缩策略。
func (t *TokenAwareCompressorV2) CompressWithStrategy(text string, strategy string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.compressWithStrategyLocked(text, strategy)
}

// compressWithStrategyLocked applies a strategy without acquiring the lock (internal helper).
// compressWithStrategyLocked 应用策略但不加锁（内部辅助方法）。
func (t *TokenAwareCompressorV2) compressWithStrategyLocked(text string, strategy string) string {
	switch strategy {
	case "whitespace":
		return tac2CompressWhitespace(text)
	case "redundancy":
		return tac2CompressRedundancy(text)
	case "abbreviation":
		return tac2CompressAbbreviation(text)
	default:
		return text
	}
}

// GetCompressionRatio returns the compression ratio between original and compressed text.
// GetCompressionRatio 返回原始文本与压缩文本之间的压缩比率。
func (t *TokenAwareCompressorV2) GetCompressionRatio(original string, compressed string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.GetCompressionRatioLocked(original, compressed)
}

// GetCompressionRatioLocked computes the compression ratio without locking (internal helper).
// GetCompressionRatioLocked 计算压缩比率但不加锁（内部辅助方法）。
func (t *TokenAwareCompressorV2) GetCompressionRatioLocked(original string, compressed string) float64 {
	originalTokens := tac2EstimateTokens(original)
	if originalTokens == 0 {
		return 1.0
	}
	compressedTokens := tac2EstimateTokens(compressed)
	return float64(compressedTokens) / float64(originalTokens)
}

// GetStats returns statistics about the compressor.
// GetStats 返回压缩器的统计信息。
func (t *TokenAwareCompressorV2) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"strategyCount":       len(t.strategies),
		"compressCount":       t.compressCount,
		"totalTokensSaved":    t.totalTokensSaved,
		"avgCompressionRatio": t.avgCompressionRatio,
	}
}

// Reset resets the compressor to its initial state (preserving strategies).
// Reset 将压缩器重置为初始状态（保留策略配置）。
func (t *TokenAwareCompressorV2) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.compressCount = 0
	t.totalTokensSaved = 0
	t.avgCompressionRatio = 0.0
}

// tac2CompressWhitespace compresses multiple consecutive whitespace characters into a single space.
// tac2CompressWhitespace 将多个连续的空白字符压缩为单个空格。
func tac2CompressWhitespace(s string) string {
	result := make([]byte, 0, len(s))
	inWhitespace := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if !inWhitespace {
				result = append(result, ' ')
				inWhitespace = true
			}
		} else {
			result = append(result, ch)
			inWhitespace = false
		}
	}
	return string(result)
}

// tac2CompressRedundancy removes repeated words and phrases from the text.
// tac2CompressRedundancy 从文本中移除重复的词和短语。
func tac2CompressRedundancy(s string) string {
	words := make([]byte, 0, len(s))
	var prevWord []byte
	var currentWord []byte

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == ' ' {
			if len(currentWord) > 0 {
				if string(currentWord) != string(prevWord) {
					if len(words) > 0 {
						words = append(words, ' ')
					}
					words = append(words, currentWord...)
					prevWord = make([]byte, len(currentWord))
					copy(prevWord, currentWord)
				}
				currentWord = currentWord[:0]
			}
		} else {
			currentWord = append(currentWord, ch)
		}
	}
	// 处理最后一个词 handle the last word
	if len(currentWord) > 0 && string(currentWord) != string(prevWord) {
		if len(words) > 0 {
			words = append(words, ' ')
		}
		words = append(words, currentWord...)
	}
	return string(words)
}

// tac2CompressAbbreviation replaces common phrases with abbreviations to save tokens.
// tac2CompressAbbreviation 将常见短语替换为缩写以节省token。
func tac2CompressAbbreviation(s string) string {
	abbreviations := []struct {
		full string
		abbr string
	}{
		{"for example", "e.g."},
		{"that is", "i.e."},
		{"and so on", "etc."},
		{"as soon as possible", "ASAP"},
		{"by the way", "BTW"},
		{"for your information", "FYI"},
		{"in other words", "i.e."},
		{"with respect to", "wrt"},
	}
	result := s
	for _, a := range abbreviations {
		result = tac2ReplaceAll(result, a.full, a.abbr)
	}
	return result
}

// tac2ReplaceAll replaces all occurrences of old with new in s (simple string replace).
// tac2ReplaceAll 将s中所有old替换为new（简单字符串替换）。
func tac2ReplaceAll(s, old, new string) string {
	if len(old) == 0 {
		return s
	}
	result := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result = append(result, new...)
			i += len(old)
		} else {
			result = append(result, s[i])
			i++
		}
	}
	return string(result)
}

// tac2EstimateTokens estimates the number of tokens in a string (len/4).
// tac2EstimateTokens 估算字符串中的token数（长度除以4）。
func tac2EstimateTokens(s string) int {
	return len(s) / 4
}
