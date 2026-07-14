package agent

import "sync"

// ── OPT-123: PromptRedundancyChecker (Prompt 冗余检查器) ──
// 检测 prompt 中的冗余内容，按类型分类统计，并计算冗余分数。
// 支持检测四种冗余类型：
//   - repeated_phrases:    重复短语（2-gram / 3-gram 重复出现）
//   - redundant_modifiers: 多余修饰词（very, really, quite 等）
//   - duplicate_sentences: 重复句子
//   - filler_words:        填充词（basically, actually, literally 等）
//
// 冗余分数 = 冗余 token 数 / 总 token 数，取值范围 [0, 1]。

// PromptRedundancyChecker Prompt 冗余检查器，检测 prompt 中的冗余内容。
type PromptRedundancyChecker struct {
	mu                   sync.RWMutex
	totalChecks          int
	totalRedundancies    int
	redundancyByType     map[string]int
	totalRedundantTokens int
	totalScore           float64
}

// NewPromptRedundancyChecker 创建一个新的 Prompt 冗余检查器实例。
func NewPromptRedundancyChecker() *PromptRedundancyChecker {
	return &PromptRedundancyChecker{
		redundancyByType: make(map[string]int),
	}
}

// Check 检查 prompt 中的冗余内容并按类型分类统计。
// 返回的 map 包含键: "repeated_phrases"、"redundant_modifiers"、
// "duplicate_sentences"、"filler_words"，值为对应类型的冗余计数。
// 每次调用会更新内部累计统计。
func (p *PromptRedundancyChecker) Check(prompt string) map[string]int {
	counts, redundantTokens, totalTokens := prcAnalyzeRedundancy(prompt)

	score := 0.0
	if totalTokens > 0 {
		score = float64(redundantTokens) / float64(totalTokens)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalChecks++
	totalRedundancy := 0
	for _, count := range counts {
		totalRedundancy += count
	}
	p.totalRedundancies += totalRedundancy
	for typ, count := range counts {
		p.redundancyByType[typ] += count
	}
	p.totalRedundantTokens += redundantTokens
	p.totalScore += score

	return counts
}

// GetRedundancyScore 计算 prompt 的冗余分数（0-1），
// 等于冗余 token 数除以总 token 数。
func (p *PromptRedundancyChecker) GetRedundancyScore(prompt string) float64 {
	_, redundantTokens, totalTokens := prcAnalyzeRedundancy(prompt)
	if totalTokens == 0 {
		return 0
	}
	return float64(redundantTokens) / float64(totalTokens)
}

// GetStats 返回检查器的统计信息，包括总检查次数、总冗余数、
// 按类型分布、冗余 token 总数和平均冗余分数。
func (p *PromptRedundancyChecker) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["totalChecks"] = p.totalChecks
	stats["totalRedundancies"] = p.totalRedundancies

	byTypeCopy := make(map[string]int, len(p.redundancyByType))
	for k, v := range p.redundancyByType {
		byTypeCopy[k] = v
	}
	stats["redundancyByType"] = byTypeCopy
	stats["totalRedundantTokens"] = p.totalRedundantTokens

	avgScore := 0.0
	if p.totalChecks > 0 {
		avgScore = p.totalScore / float64(p.totalChecks)
	}
	stats["avgScore"] = avgScore

	return stats
}

// Reset 重置检查器的所有统计数据。
func (p *PromptRedundancyChecker) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalChecks = 0
	p.totalRedundancies = 0
	p.redundancyByType = make(map[string]int)
	p.totalRedundantTokens = 0
	p.totalScore = 0
}

// ── 辅助函数（prc 前缀）──

// prcModifierWords 定义冗余修饰词集合。
var prcModifierWords = map[string]bool{
	"very": true, "really": true, "quite": true, "rather": true,
	"somewhat": true, "fairly": true, "pretty": true, "extremely": true,
	"absolutely": true, "completely": true,
}

// prcFillerWords 定义填充词集合。
var prcFillerWords = map[string]bool{
	"basically": true, "actually": true, "literally": true, "essentially": true,
	"simply": true, "just": true, "totally": true, "honestly": true,
	"frankly": true, "obviously": true, "clearly": true, "indeed": true,
}

// prcAnalyzeRedundancy 分析 prompt 的冗余内容，
// 返回各类型计数、冗余 token 数和总 token 数。
func prcAnalyzeRedundancy(prompt string) (counts map[string]int, redundantTokens int, totalTokens int) {
	words := prcSplitWords(prompt)
	lowerWords := make([]string, len(words))
	for i, w := range words {
		lowerWords[i] = prcToLower(w)
	}
	totalTokens = len(words)

	repeatedCount, repeatedTokens := prcCountRepeatedPhrases(lowerWords)
	modifierCount := prcCountMatches(lowerWords, prcModifierWords)
	sentences := prcSplitSentences(prompt)
	duplicateCount, duplicateTokens := prcCountDuplicates(sentences)
	fillerCount := prcCountMatches(lowerWords, prcFillerWords)

	counts = map[string]int{
		"repeated_phrases":    repeatedCount,
		"redundant_modifiers": modifierCount,
		"duplicate_sentences": duplicateCount,
		"filler_words":        fillerCount,
	}

	redundantTokens = repeatedTokens + modifierCount + duplicateTokens + fillerCount

	return counts, redundantTokens, totalTokens
}

// prcCountRepeatedPhrases 统计重复的 2-gram 和 3-gram 短语。
// 返回重复次数（不含首次出现）和对应的冗余 token 数。
func prcCountRepeatedPhrases(words []string) (int, int) {
	if len(words) < 2 {
		return 0, 0
	}

	ngramCounts := make(map[string]int)

	// 2-grams
	for i := 0; i <= len(words)-2; i++ {
		phrase := words[i] + " " + words[i+1]
		ngramCounts[phrase]++
	}

	// 3-grams
	for i := 0; i <= len(words)-3; i++ {
		phrase := words[i] + " " + words[i+1] + " " + words[i+2]
		ngramCounts[phrase]++
	}

	count := 0
	tokens := 0
	for phrase, c := range ngramCounts {
		if c > 1 {
			extra := c - 1
			count += extra
			// 统计短语中的单词数
			phraseLen := 1
			for j := 0; j < len(phrase); j++ {
				if phrase[j] == ' ' {
					phraseLen++
				}
			}
			tokens += extra * phraseLen
		}
	}

	return count, tokens
}

// prcCountMatches 统计词列表中匹配指定集合的单词数量。
func prcCountMatches(words []string, wordSet map[string]bool) int {
	count := 0
	for _, w := range words {
		if wordSet[w] {
			count++
		}
	}
	return count
}

// prcCountDuplicates 统计重复句子数量和对应的冗余 token 数。
// 重复判定基于小写化后的完整句子匹配。
func prcCountDuplicates(sentences []string) (int, int) {
	if len(sentences) < 2 {
		return 0, 0
	}

	seen := make(map[string]int)
	for _, s := range sentences {
		lower := prcToLower(s)
		seen[lower]++
	}

	count := 0
	tokens := 0
	for s, c := range seen {
		if c > 1 {
			extra := c - 1
			count += extra
			wordCount := len(prcSplitWords(s))
			tokens += extra * wordCount
		}
	}

	return count, tokens
}

// prcSplitWords 将字符串按空白字符分词，并去除每个词首尾的非字母数字字符。
func prcSplitWords(s string) []string {
	var words []string
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if start >= 0 {
				word := prcCleanWord(s[start:i])
				if word != "" {
					words = append(words, word)
				}
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		word := prcCleanWord(s[start:])
		if word != "" {
			words = append(words, word)
		}
	}
	return words
}

// prcSplitSentences 将字符串按句末标点（. ! ?）分割为句子列表。
func prcSplitSentences(s string) []string {
	var sentences []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == '!' || c == '?' {
			sentence := prcTrim(s[start : i+1])
			if sentence != "" {
				sentences = append(sentences, sentence)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		remaining := prcTrim(s[start:])
		if remaining != "" {
			sentences = append(sentences, remaining)
		}
	}
	return sentences
}

// prcTrim 去除字符串首尾的空白字符。
func prcTrim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// prcCleanWord 去除单词首尾的非字母数字字符。
func prcCleanWord(s string) string {
	start := 0
	end := len(s)
	for start < end && !prcIsAlnum(s[start]) {
		start++
	}
	for end > start && !prcIsAlnum(s[end-1]) {
		end--
	}
	return s[start:end]
}

// prcIsAlnum 判断字节是否为 ASCII 字母或数字。
func prcIsAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// prcToLower 将字符串转换为小写（仅处理 ASCII 字母）。
func prcToLower(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}
