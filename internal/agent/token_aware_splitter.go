package agent

import "sync"

// ── OPT-151: TokenAwareSplitter (Token 感知分割器) ──
// 在最优边界分割大上下文以最小化 token 浪费。当上下文超过 token 预算时，
// 按优先级在换行符、句号、空格处寻找最佳分割点，确保分割后的片段
// 在语义边界处断开，减少因截断导致的信息损失。
//
// 原理：LLM 上下文窗口有 token 上限，当输入超过预算时需要分割。
// 简单的按固定长度截断可能切断句子或词组，导致语义不完整。
// 本分割器优先在自然边界（换行符 > 句号 > 空格）处分割，
// 最大化每个片段的语义完整性。
//
// 效果：减少因截断导致的语义断裂，提高分割后片段的可理解性，
// 同时统计分割次数与节省的 token 数，为上下文管理提供反馈。

// TokenAwareSplitter Token 感知分割器
type TokenAwareSplitter struct {
	mu               sync.RWMutex
	maxSegmentTokens int
	splitCount       int
	totalTokensSaved int
}

// NewTokenAwareSplitter 创建 Token 感知分割器。
// maxTokens 指定每个片段的最大 token 预算，若 <= 0 则默认 4096。
func NewTokenAwareSplitter(maxTokens int) *TokenAwareSplitter {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	return &TokenAwareSplitter{
		maxSegmentTokens: maxTokens,
	}
}

// Split 按 token 预算分割文本，优先在换行符、句号、空格处分割。
// text 为待分割文本，estimatedTokens 为文本的预估 token 数（若 <= 0 则自动估算）。
// 返回分割后的文本片段列表。每次调用递增 splitCount，并累加因边界分割节省的 token 数。
func (s *TokenAwareSplitter) Split(text string, estimatedTokens int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.splitCount++

	if estimatedTokens <= 0 {
		estimatedTokens = tasEstimateTokens(text)
	}

	if estimatedTokens <= s.maxSegmentTokens {
		return []string{text}
	}

	var segments []string
	remaining := text
	remainingTokens := estimatedTokens

	for remainingTokens > s.maxSegmentTokens && len(remaining) > 0 {
		// 根据 token 比例估算当前片段允许的最大字符数
		maxChars := len(remaining) * s.maxSegmentTokens / remainingTokens
		if maxChars <= 0 {
			maxChars = s.maxSegmentTokens * 4
		}
		if maxChars > len(remaining) {
			maxChars = len(remaining)
		}

		splitPoint := tasFindSplitPoint(remaining, maxChars)
		if splitPoint <= 0 {
			splitPoint = maxChars
		}
		if splitPoint > len(remaining) {
			splitPoint = len(remaining)
		}

		segment := remaining[:splitPoint]
		segments = append(segments, segment)

		// 统计因在边界处提前分割而节省的 token 数
		segmentTokens := tasEstimateTokens(segment)
		saved := s.maxSegmentTokens - segmentTokens
		if saved > 0 {
			s.totalTokensSaved += saved
		}

		remaining = remaining[splitPoint:]
		remainingTokens = tasEstimateTokens(remaining)
	}

	if len(remaining) > 0 {
		segments = append(segments, remaining)
	}

	return segments
}

// FindSplitPoint 找到最佳分割点。
// 在 maxTokens 对应的字符范围内，优先在换行符、句号、空格处寻找分割位置。
// 返回分割点的字符索引。
func (s *TokenAwareSplitter) FindSplitPoint(text string, maxTokens int) int {
	maxChars := maxTokens * 4
	if maxChars > len(text) {
		maxChars = len(text)
	}
	return tasFindSplitPoint(text, maxChars)
}

// GetStats 返回分割器的统计信息，包括 splitCount、totalTokensSaved、maxSegmentTokens。
func (s *TokenAwareSplitter) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"splitCount":       s.splitCount,
		"totalTokensSaved": s.totalTokensSaved,
		"maxSegmentTokens": s.maxSegmentTokens,
	}
}

// Reset 重置分割器，清除所有统计信息。
func (s *TokenAwareSplitter) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.splitCount = 0
	s.totalTokensSaved = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tas 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tasEstimateTokens 粗略估算字符串的 token 数（约 4 字符/token）。
func tasEstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4
}

// tasFindSplitPoint 在 text[0:maxChars] 范围内寻找最佳分割点。
// 优先级：换行符 > 句号 > 空格 > 直接在 maxChars 处截断。
// 返回分割点的字符索引（分割点之后的位置，即下一片段的起始）。
func tasFindSplitPoint(text string, maxChars int) int {
	if maxChars >= len(text) {
		return len(text)
	}
	if maxChars <= 0 {
		return 0
	}

	// 优先在换行符处分割
	if idx := tasLastIndexByte(text, '\n', maxChars); idx >= 0 {
		return idx + 1
	}

	// 其次在句号处分割
	if idx := tasLastIndexByte(text, '.', maxChars); idx >= 0 {
		return idx + 1
	}

	// 再次在空格处分割
	if idx := tasLastIndexByte(text, ' ', maxChars); idx >= 0 {
		return idx + 1
	}

	// 最后直接在 maxChars 处截断
	return maxChars
}

// tasLastIndexByte 在 text[0:limit] 范围内反向查找字节 b 的最后一个位置。
// 返回 -1 表示未找到。
func tasLastIndexByte(text string, b byte, limit int) int {
	if limit > len(text) {
		limit = len(text)
	}
	for i := limit - 1; i >= 0; i-- {
		if text[i] == b {
			return i
		}
	}
	return -1
}
