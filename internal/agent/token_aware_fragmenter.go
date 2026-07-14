package agent

import "sync"

// ── OPT-156: TokenAwareFragmenter (Token 感知分片器 / Token-Aware Fragmenter) ──
// 将大请求按估算的 token 数分片，以支持流式处理。每个分片包含约 fragmentSize
// 个 token，通过简单的字符长度 / 4 估算 token 数，确保每个分片在 token 预算范围内。
//
// 原理：当请求文本过大时，需要将其拆分为多个较小的分片进行流式处理。
// 按 token 数而非字符数分片可以更精确地控制每个分片的大小，
// 使得每个分片在 LLM 的处理预算内，提高流式处理的效率。
//
// 效果：支持大请求的流式处理，统计分片数量和最后一片的 token 数，
// 为流量管理和分片策略优化提供数据支撑。

// TokenAwareFragmenter Token 感知分片器
type TokenAwareFragmenter struct {
	mu                 sync.RWMutex
	fragmentSize       int // 每个分片的目标 token 数
	fragmentCount      int // 当前分片操作产生的分片数
	totalFragments     int // 累计产生的分片总数
	lastFragmentTokens int // 最后一个分片的 token 数
}

// NewTokenAwareFragmenter 创建 Token 感知分片器。
// fragmentSize 指定每个分片的目标 token 数，若 <= 0 则默认 512。
func NewTokenAwareFragmenter(fragmentSize int) *TokenAwareFragmenter {
	if fragmentSize <= 0 {
		fragmentSize = 512
	}
	return &TokenAwareFragmenter{
		fragmentSize: fragmentSize,
	}
}

// Fragment 按估算的 token 数将文本分片，每片约 fragmentSize 个 token。
// text 为待分片文本，返回分片后的文本列表。
// 同时更新 fragmentCount、totalFragments 和 lastFragmentTokens 统计信息。
func (f *TokenAwareFragmenter) Fragment(text string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	fragments := tafFragmentText(text, f.fragmentSize)
	f.fragmentCount = len(fragments)
	f.totalFragments += len(fragments)
	if len(fragments) > 0 {
		f.lastFragmentTokens = tafEstimateTokens(fragments[len(fragments)-1])
	}
	return fragments
}

// EstimateFragmentCount 估算文本分片后的分片数量。
// text 为待估算文本，返回预估的分片数。
func (f *TokenAwareFragmenter) EstimateFragmentCount(text string) int {
	f.mu.RLock()
	defer f.mu.RUnlock()

	totalTokens := tafEstimateTokens(text)
	if totalTokens == 0 {
		return 0
	}
	count := totalTokens / f.fragmentSize
	if totalTokens%f.fragmentSize != 0 {
		count++
	}
	if count == 0 {
		count = 1
	}
	return count
}

// GetLastFragmentTokens 获取最后一次分片操作中最后一个分片的 token 数。
func (f *TokenAwareFragmenter) GetLastFragmentTokens() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastFragmentTokens
}

// GetStats 返回分片器的统计信息。
// 包含 fragmentSize、fragmentCount、totalFragments 和 lastFragmentTokens。
func (f *TokenAwareFragmenter) GetStats() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return map[string]interface{}{
		"fragmentSize":       f.fragmentSize,
		"fragmentCount":      f.fragmentCount,
		"totalFragments":     f.totalFragments,
		"lastFragmentTokens": f.lastFragmentTokens,
	}
}

// Reset 重置分片器的所有统计信息。
func (f *TokenAwareFragmenter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fragmentCount = 0
	f.totalFragments = 0
	f.lastFragmentTokens = 0
}

// tafEstimateTokens 估算文本的 token 数，采用字符长度 / 4 的简单估算方式。
func tafEstimateTokens(text string) int {
	return len(text) / 4
}

// tafFragmentText 将文本按目标 token 数分片。
// 每个分片大约包含 fragmentSize * 4 个字符（因为 1 token ≈ 4 字符）。
// 在分片边界处尽量不切断单词，优先在空白字符处分割。
func tafFragmentText(text string, fragmentSize int) []string {
	if len(text) == 0 {
		return nil
	}
	// 每个分片的字符数 ≈ fragmentSize tokens * 4 chars/token
	charsPerFragment := fragmentSize * 4
	if charsPerFragment <= 0 {
		charsPerFragment = 1
	}

	var fragments []string
	remaining := text
	for len(remaining) > 0 {
		end := charsPerFragment
		if end > len(remaining) {
			end = len(remaining)
		}
		// 尝试在空格或换行符处分割，避免切断单词
		if end < len(remaining) {
			for i := end; i > end/2 && i > 0; i-- {
				if remaining[i] == ' ' || remaining[i] == '\n' || remaining[i] == '\t' {
					end = i + 1
					break
				}
			}
		}
		fragments = append(fragments, remaining[:end])
		remaining = remaining[end:]
	}
	return fragments
}
