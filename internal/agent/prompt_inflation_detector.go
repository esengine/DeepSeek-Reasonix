package agent

import "sync"

// ── OPT-139: PromptInflationDetector (Prompt 膨胀检测器) ──
// 检测 prompt 中的不必要增长。从四个维度衡量膨胀:
//   - redundancy        重复内容（重复出现的句子）
//   - verbosity         冗长表达（过长的句子）
//   - over_explanation  过度解释（解释性标记短语）
//   - repetition        重复词汇（多次出现的同一词）
//
// 膨胀分数 = excessTokens / totalTokens (0-1)，超过 0.3 视为膨胀。

// PromptInflationDetector Prompt 膨胀检测器，检测 prompt 中的不必要增长。
type PromptInflationDetector struct {
	mu                sync.RWMutex
	totalChecks       int
	totalInflated     int
	totalExcessTokens int
	inflationFactors  map[string]int
	baselineLength    int
	scoreSum          float64 // 用于计算 avgInflationScore
}

// NewPromptInflationDetector 创建一个新的 Prompt 膨胀检测器。
// baseline 为基准 prompt 长度（字符数）。
func NewPromptInflationDetector(baseline int) *PromptInflationDetector {
	return &PromptInflationDetector{
		inflationFactors: make(map[string]int),
		baselineLength:   baseline,
	}
}

// Detect 检测 prompt 中的膨胀因素。
// 返回各因素对应的超额 token 估算: redundancy、verbosity、over_explanation、repetition。
// 同时更新内部统计（totalChecks、totalExcessTokens、inflationFactors、totalInflated）。
func (d *PromptInflationDetector) Detect(prompt string) map[string]int {
	d.mu.Lock()
	defer d.mu.Unlock()

	factors, excess, totalTokens := pidAnalyze(prompt)
	score := 0.0
	if totalTokens > 0 {
		score = float64(excess) / float64(totalTokens)
	}

	d.totalChecks++
	d.totalExcessTokens += excess
	for k, v := range factors {
		d.inflationFactors[k] += v
	}
	d.scoreSum += score
	if score > 0.3 {
		d.totalInflated++
	}
	return factors
}

// GetInflationScore 返回 prompt 的膨胀分数 (0-1)。
// 分数 = excessTokens / totalTokens，其中 totalTokens ≈ len(prompt)/4。
func (d *PromptInflationDetector) GetInflationScore(prompt string) float64 {
	_, excess, totalTokens := pidAnalyze(prompt)
	if totalTokens <= 0 {
		return 0
	}
	return pidClamp(float64(excess)/float64(totalTokens), 0, 1)
}

// IsInflated 判断 prompt 是否存在膨胀（膨胀分数 > 0.3）。
func (d *PromptInflationDetector) IsInflated(prompt string) bool {
	return d.GetInflationScore(prompt) > 0.3
}

// GetStats 返回检测器的统计信息。
// 包含 totalChecks、totalInflated、totalExcessTokens、avgInflationScore、baselineLength。
func (d *PromptInflationDetector) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	avgInflationScore := 0.0
	if d.totalChecks > 0 {
		avgInflationScore = d.scoreSum / float64(d.totalChecks)
	}
	return map[string]interface{}{
		"totalChecks":       d.totalChecks,
		"totalInflated":     d.totalInflated,
		"totalExcessTokens": d.totalExcessTokens,
		"avgInflationScore": avgInflationScore,
		"baselineLength":    d.baselineLength,
	}
}

// Reset 重置检测器的所有统计数据。baselineLength 作为配置保留。
func (d *PromptInflationDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.totalChecks = 0
	d.totalInflated = 0
	d.totalExcessTokens = 0
	d.inflationFactors = make(map[string]int)
	d.scoreSum = 0
}

// ---------------------------------------------------------------------------
// 辅助函数 (pid 前缀)
// ---------------------------------------------------------------------------

// pidExplanationMarkers 过度解释的标记短语集合。
var pidExplanationMarkers = []string{
	"in other words", "that is to say", "to put it simply",
	"basically", "essentially", "in summary", "as a matter of fact",
	"needless to say", "it goes without saying", "what i mean is",
}

// pidAnalyze 分析 prompt，返回 (各因素超额 token, 总超额 token, 总 token)。
func pidAnalyze(prompt string) (map[string]int, int, int) {
	totalTokens := pidEstimateTokens(prompt)
	factors := map[string]int{
		"redundancy":       pidDetectRedundancy(prompt),
		"verbosity":        pidDetectVerbosity(prompt),
		"over_explanation": pidDetectOverExplanation(prompt),
		"repetition":       pidDetectRepetition(prompt),
	}
	excess := 0
	for _, v := range factors {
		excess += v
	}
	return factors, excess, totalTokens
}

// pidEstimateTokens 估算 token 数: len(s) / 4。
func pidEstimateTokens(s string) int {
	return len(s) / 4
}

// pidDetectRedundancy 检测重复句子。每个重复副本按其 token 估算计入超额。
func pidDetectRedundancy(prompt string) int {
	sentences := pidSplitSentences(prompt)
	seen := make(map[string]int)
	for _, s := range sentences {
		seen[s]++
	}
	excess := 0
	for s, c := range seen {
		if c > 1 {
			excess += (c - 1) * pidEstimateTokens(s)
		}
	}
	return excess
}

// pidDetectVerbosity 检测冗长句子。词数超过 20 的句子，超出部分计入超额。
func pidDetectVerbosity(prompt string) int {
	sentences := pidSplitSentences(prompt)
	excess := 0
	for _, s := range sentences {
		words := pidTokenize(s)
		if len(words) > 20 {
			excess += len(words) - 20
		}
	}
	return excess
}

// pidDetectOverExplanation 检测过度解释标记短语。每个出现计 5 个超额 token。
func pidDetectOverExplanation(prompt string) int {
	lower := pidToLower(prompt)
	count := 0
	for _, m := range pidExplanationMarkers {
		count += pidCountSubstring(lower, m)
	}
	return count * 5
}

// pidDetectRepetition 检测重复词汇。每个词的额外出现计入 1 个超额 token。
func pidDetectRepetition(prompt string) int {
	words := pidTokenize(prompt)
	counts := make(map[string]int)
	for _, w := range words {
		counts[w]++
	}
	excess := 0
	for _, c := range counts {
		if c > 1 {
			excess += c - 1
		}
	}
	return excess
}

// pidSplitSentences 按句子终止符切分 prompt 并去除首尾空白。
func pidSplitSentences(s string) []string {
	sentences := make([]string, 0)
	var buf []rune
	for _, r := range s {
		if r == '.' || r == '!' || r == '?' || r == '。' || r == '！' || r == '？' || r == ';' || r == '；' {
			if len(buf) > 0 {
				sentences = append(sentences, pidTrim(string(buf)))
			}
			buf = buf[:0]
		} else {
			buf = append(buf, r)
		}
	}
	if len(buf) > 0 {
		sentences = append(sentences, pidTrim(string(buf)))
	}
	return sentences
}

// pidTokenize 将字符串切分为 token 列表。
// ASCII 字母数字按单词聚合（小写化），非 ASCII 字符作为独立 token。
func pidTokenize(s string) []string {
	tokens := make([]string, 0)
	var buf []rune
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			buf = append(buf, r)
			continue
		}
		if len(buf) > 0 {
			tokens = append(tokens, string(buf))
			buf = buf[:0]
		}
		if r >= 0x80 {
			tokens = append(tokens, string(r))
		}
	}
	if len(buf) > 0 {
		tokens = append(tokens, string(buf))
	}
	return tokens
}

// pidToLower 将 ASCII 大写字母转为小写。
func pidToLower(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] = b[i] + ('a' - 'A')
		}
	}
	return string(b)
}

// pidCountSubstring 统计 sub 在 s 中出现的次数（不重叠）。
func pidCountSubstring(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	count := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			count++
			i += len(sub) - 1
		}
	}
	return count
}

// pidTrim 去除字符串首尾的空白字符。
func pidTrim(s string) string {
	start := 0
	end := len(s)
	for start < end && pidIsSpace(s[start]) {
		start++
	}
	for end > start && pidIsSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

// pidIsSpace 判断字节是否为空白字符。
func pidIsSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// pidClamp 将值限制在 [lo, hi] 范围内。
func pidClamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
