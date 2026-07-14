package agent

import "sync"

// OPT-200: PromptSegmentCompressor / 提示分段压缩器
// 对提示的各个段落分别进行压缩，包括移除多余空白和缩写常见词，
// 以减少Token消耗。

// PromptSegmentCompressor 是提示分段压缩器。
type PromptSegmentCompressor struct {
	mu                  sync.RWMutex
	compressedCount     int
	totalTokensSaved    int
	avgCompressionRatio float64
	minSegmentTokens    int
}

// NewPromptSegmentCompressor 创建一个新的PromptSegmentCompressor实例。
func NewPromptSegmentCompressor(minSegmentTokens int) *PromptSegmentCompressor {
	return &PromptSegmentCompressor{
		compressedCount:     0,
		totalTokensSaved:    0,
		avgCompressionRatio: 0.0,
		minSegmentTokens:    minSegmentTokens,
	}
}

// CompressSegment 压缩单个段落（移除多余空白、缩写常见词）。
// 如果段落Token数不足minSegmentTokens，则不压缩直接返回。
func (p *PromptSegmentCompressor) CompressSegment(segment string) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	originalTokens := pscEstimateTokens(segment)
	// 段落过小不压缩
	if originalTokens < p.minSegmentTokens {
		return segment
	}

	compressed := pscCompressWhitespace(segment)
	compressed = pscAbbreviate(compressed)
	compressedTokens := pscEstimateTokens(compressed)

	saved := originalTokens - compressedTokens
	if saved > 0 {
		p.totalTokensSaved += saved
	}
	ratio := p.GetCompressionRatioLocked(segment, compressed)
	// 更新平均压缩比（增量平均）
	p.compressedCount++
	if p.compressedCount == 1 {
		p.avgCompressionRatio = ratio
	} else {
		p.avgCompressionRatio = (p.avgCompressionRatio*float64(p.compressedCount-1) + ratio) / float64(p.compressedCount)
	}
	return compressed
}

// CompressAll 批量压缩多个段落。
func (p *PromptSegmentCompressor) CompressAll(segments []string) []string {
	result := make([]string, len(segments))
	for i, seg := range segments {
		result[i] = p.CompressSegment(seg)
	}
	return result
}

// GetCompressionRatio 计算压缩比（压缩后/压缩前），值越小压缩效果越好。
func (p *PromptSegmentCompressor) GetCompressionRatio(original string, compressed string) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.GetCompressionRatioLocked(original, compressed)
}

// GetCompressionRatioLocked 是GetCompressionRatio的内部实现，调用时已持有读锁。
func (p *PromptSegmentCompressor) GetCompressionRatioLocked(original string, compressed string) float64 {
	origTokens := pscEstimateTokens(original)
	if origTokens == 0 {
		return 1.0
	}
	compTokens := pscEstimateTokens(compressed)
	return float64(compTokens) / float64(origTokens)
}

// GetStats 返回压缩器的统计信息。
func (p *PromptSegmentCompressor) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"compressedCount":     p.compressedCount,
		"totalTokensSaved":    p.totalTokensSaved,
		"avgCompressionRatio": p.avgCompressionRatio,
		"minSegmentTokens":    p.minSegmentTokens,
	}
}

// Reset 重置压缩器为初始状态。
func (p *PromptSegmentCompressor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.compressedCount = 0
	p.totalTokensSaved = 0
	p.avgCompressionRatio = 0.0
}

// pscCompressWhitespace 移除多余空白字符：连续空格合并为单个，
// 去除行首行尾空白，连续空行合并为单个空行。
func pscCompressWhitespace(s string) string {
	var result []rune
	inWhitespace := false
	prevWasNewline := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !inWhitespace && !prevWasNewline {
				result = append(result, ' ')
			}
			inWhitespace = true
			continue
		}
		if r == '\n' {
			if !prevWasNewline {
				result = append(result, '\n')
			}
			inWhitespace = false
			prevWasNewline = true
			continue
		}
		inWhitespace = false
		prevWasNewline = false
		result = append(result, r)
	}
	// 去除尾部空白
	for len(result) > 0 && (result[len(result)-1] == ' ' || result[len(result)-1] == '\n') {
		result = result[:len(result)-1]
	}
	return string(result)
}

// pscAbbreviate 缩写常见词以减少Token消耗。
func pscAbbreviate(s string) string {
	abbreviations := map[string]string{
		"for example":     "e.g.",
		"For example":     "E.g.",
		"that is":         "i.e.",
		"That is":         "I.e.",
		"and so on":       "etc.",
		"and so forth":    "etc.",
		"please":          "pls",
		"Please":          "Pls",
		"because":         "bc",
		"Because":         "Bc",
		"information":     "info",
		"Information":     "Info",
		"application":     "app",
		"Application":     "App",
		"configuration":   "config",
		"Configuration":   "Config",
		"specification":   "spec",
		"Specification":   "Spec",
		"approximately":   "approx",
		"Approximately":   "Approx",
		"with respect to": "re",
		"With respect to": "Re",
	}
	result := s
	for full, abbr := range abbreviations {
		result = stringsReplaceAll(result, full, abbr)
	}
	return result
}

// pscEstimateTokens 在 prompt_segment_cache_v2.go 中已定义，此处复用。
// func pscEstimateTokens(s string) int

// stringsReplaceAll 是strings.ReplaceAll的简单实现，避免引入额外包。
func stringsReplaceAll(s, old, new string) string {
	if old == "" || old == new {
		return s
	}
	result := ""
	for {
		idx := indexOf(s, old)
		if idx < 0 {
			break
		}
		result += s[:idx] + new
		s = s[idx+len(old):]
	}
	return result + s
}

// indexOf 查找子串位置，未找到返回-1。
func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	if len(sub) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
