package agent

import "sync"

// PromptTokenAnalyzer (OPT-103) Prompt token 深度分析器。
// 分析 prompt 中的 token 浪费，包括冗余空格、重复标点、重复内容和样板文本。
type PromptTokenAnalyzer struct {
	mu                sync.RWMutex
	analyses          int
	totalPromptTokens int
	totalWasteTokens  int
	wasteCategories   map[string]int
	avgEfficiency     float64
}

// NewPromptTokenAnalyzer 创建一个新的 PromptTokenAnalyzer 实例。
func NewPromptTokenAnalyzer() *PromptTokenAnalyzer {
	return &PromptTokenAnalyzer{
		wasteCategories: make(map[string]int),
	}
}

// Analyze 分析给定 prompt 的 token 使用情况。
// 返回包含 totalTokens、wasteTokens、efficiency 和 categories 的分析结果。
// token 数量使用 len(prompt)/4 进行估算。
func (a *PromptTokenAnalyzer) Analyze(prompt string) map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	totalTokens := len(prompt) / 4
	categories := a.categorizeWasteLocked(prompt)

	wasteTokens := 0
	for _, count := range categories {
		wasteTokens += count
	}
	if wasteTokens > totalTokens {
		wasteTokens = totalTokens
	}

	efficiency := 1.0
	if totalTokens > 0 {
		efficiency = 1.0 - float64(wasteTokens)/float64(totalTokens)
	}

	a.analyses++
	a.totalPromptTokens += totalTokens
	a.totalWasteTokens += wasteTokens
	for cat, count := range categories {
		a.wasteCategories[cat] += count
	}
	if a.totalPromptTokens > 0 {
		a.avgEfficiency = 1.0 - float64(a.totalWasteTokens)/float64(a.totalPromptTokens)
	}

	return map[string]interface{}{
		"totalTokens": totalTokens,
		"wasteTokens": wasteTokens,
		"efficiency":  efficiency,
		"categories":  categories,
	}
}

// CategorizeWaste 分类 prompt 中的 token 浪费。
// 返回各类浪费的 token 数：whitespace、punctuation、repetition、boilerplate。
func (a *PromptTokenAnalyzer) CategorizeWaste(prompt string) map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.categorizeWasteLocked(prompt)
}

// categorizeWasteLocked 在已加锁的情况下分类 token 浪费。
func (a *PromptTokenAnalyzer) categorizeWasteLocked(prompt string) map[string]int {
	categories := map[string]int{
		"whitespace":  0,
		"punctuation": 0,
		"repetition":  0,
		"boilerplate": 0,
	}

	// 空格浪费：连续空格中多余的空格
	for i := 1; i < len(prompt); i++ {
		if prompt[i] == ' ' && prompt[i-1] == ' ' {
			categories["whitespace"]++
		}
	}

	// 标点浪费：连续重复的标点符号
	puncts := []byte{'.', ',', '!', '?', ';', ':', '-', '_'}
	for i := 1; i < len(prompt); i++ {
		for _, p := range puncts {
			if prompt[i] == p && prompt[i-1] == p {
				categories["punctuation"]++
			}
		}
	}

	// 重复浪费：连续重复的词
	tokens := ptaSplitBySpace(prompt)
	for i := 1; i < len(tokens); i++ {
		if tokens[i] == tokens[i-1] && len(tokens[i]) > 0 {
			t := len(tokens[i]) / 4
			if t < 1 {
				t = 1
			}
			categories["repetition"] += t
		}
	}

	// 样板文本浪费：常见的系统前缀和模板短语
	boilerplates := []string{
		"You are a helpful assistant",
		"As an AI",
		"I am an AI",
		"Please note that",
		"It is important to",
		"In order to",
		"作为一个AI",
		"你是一个有用的助手",
		"请注意",
		"需要注意的是",
	}
	lowerPrompt := ptaToLower(prompt)
	for _, bp := range boilerplates {
		if ptaContains(lowerPrompt, ptaToLower(bp)) {
			t := len(bp) / 4
			if t < 1 {
				t = 1
			}
			categories["boilerplate"] += t
		}
	}

	return categories
}

// GetStats 返回分析器的统计信息。
func (a *PromptTokenAnalyzer) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return map[string]interface{}{
		"analyses":          a.analyses,
		"totalPromptTokens": a.totalPromptTokens,
		"totalWasteTokens":  a.totalWasteTokens,
		"avgEfficiency":     a.avgEfficiency,
		"wasteCategories":   a.wasteCategories,
	}
}

// Reset 重置分析器的所有状态。
func (a *PromptTokenAnalyzer) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.analyses = 0
	a.totalPromptTokens = 0
	a.totalWasteTokens = 0
	a.wasteCategories = make(map[string]int)
	a.avgEfficiency = 0
}

// ptaSplitBySpace 按空白字符分割字符串为 token 列表。
func ptaSplitBySpace(s string) []string {
	var tokens []string
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
			if start >= 0 {
				tokens = append(tokens, s[start:i])
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		tokens = append(tokens, s[start:])
	}
	return tokens
}

// ptaToLower 将字符串中的 ASCII 大写字母转换为小写。
func ptaToLower(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			runes[i] = r + ('a' - 'A')
		}
	}
	return string(runes)
}

// ptaContains 检查字符串 s 是否包含子串 sub。
func ptaContains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
