package agent

import "sync"

// AdaptiveContextSelector (OPT-102) 自适应上下文窗口选择器。
// 根据查询复杂度动态选择最优的上下文窗口大小，在保证回答质量的同时减少 token 消耗。
type AdaptiveContextSelector struct {
	mu              sync.RWMutex
	maxWindow       int
	currentWindow   int
	queryComplexity float64
	selections      map[int]int
	totalSelections int
	windowHistory   []int
}

// NewAdaptiveContextSelector 创建一个新的 AdaptiveContextSelector 实例。
// maxWindow 指定最大上下文窗口大小。
func NewAdaptiveContextSelector(maxWindow int) *AdaptiveContextSelector {
	return &AdaptiveContextSelector{
		maxWindow:  maxWindow,
		selections: make(map[int]int),
	}
}

// AnalyzeQueryComplexity 分析查询的复杂度，返回 0.0 到 1.0 之间的复杂度值。
// 复杂度由查询长度、关键词密度和问题类型综合决定。
func (s *AdaptiveContextSelector) AnalyzeQueryComplexity(query string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 长度因子 (0.0-0.4)：查询越长，复杂度越高
	lengthFactor := float64(len(query)) / 500.0
	if lengthFactor > 0.4 {
		lengthFactor = 0.4
	}

	// 关键词密度因子 (0.0-0.3)
	keywords := []string{
		"分析", "比较", "解释", "设计", "实现", "优化", "推导", "证明",
		"explain", "analyze", "compare", "design", "implement", "optimize",
		"derive", "prove", "summarize", "evaluate",
	}
	lowerQuery := acsToLower(query)
	keywordCount := 0
	for _, kw := range keywords {
		if acsContains(lowerQuery, acsToLower(kw)) {
			keywordCount++
		}
	}
	keywordFactor := float64(keywordCount) / 5.0 * 0.3
	if keywordFactor > 0.3 {
		keywordFactor = 0.3
	}

	// 问题类型因子 (0.0-0.3)
	questionType := 0.0
	indicators := []string{
		"?", "？", "如何", "为什么", "什么", "怎样", "是否",
		"how", "why", "what", "when", "where", "which",
	}
	for _, ind := range indicators {
		if acsContains(lowerQuery, acsToLower(ind)) {
			questionType = 0.3
			break
		}
	}

	complexity := lengthFactor + keywordFactor + questionType
	if complexity > 1.0 {
		complexity = 1.0
	}

	s.queryComplexity = complexity
	return complexity
}

// SelectWindow 根据复杂度选择上下文窗口大小。
// complexity < 0.3 → maxWindow*0.25
// complexity < 0.6 → maxWindow*0.5
// complexity < 0.8 → maxWindow*0.75
// 否则 → maxWindow
func (s *AdaptiveContextSelector) SelectWindow(complexity float64) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	var window int
	switch {
	case complexity < 0.3:
		window = int(float64(s.maxWindow) * 0.25)
	case complexity < 0.6:
		window = int(float64(s.maxWindow) * 0.5)
	case complexity < 0.8:
		window = int(float64(s.maxWindow) * 0.75)
	default:
		window = s.maxWindow
	}

	if window < 1 {
		window = 1
	}
	s.currentWindow = window
	return window
}

// RecordSelection 记录一次窗口选择，用于后续统计分析。
func (s *AdaptiveContextSelector) RecordSelection(window int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selections[window]++
	s.totalSelections++
	s.windowHistory = append(s.windowHistory, window)
}

// GetOptimalWindow 返回当前最优的上下文窗口大小。
func (s *AdaptiveContextSelector) GetOptimalWindow() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentWindow
}

// GetStats 返回选择器的统计信息。
func (s *AdaptiveContextSelector) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	avgWindow := 0
	if s.totalSelections > 0 {
		sum := 0
		for w, count := range s.selections {
			sum += w * count
		}
		avgWindow = sum / s.totalSelections
	}

	return map[string]interface{}{
		"maxWindow":       s.maxWindow,
		"currentWindow":   s.currentWindow,
		"queryComplexity": s.queryComplexity,
		"totalSelections": s.totalSelections,
		"avgWindow":       avgWindow,
	}
}

// Reset 重置选择器的所有状态，但保留 maxWindow 配置。
func (s *AdaptiveContextSelector) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentWindow = 0
	s.queryComplexity = 0
	s.selections = make(map[int]int)
	s.totalSelections = 0
	s.windowHistory = nil
}

// acsToLower 将字符串中的 ASCII 大写字母转换为小写。
func acsToLower(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			runes[i] = r + ('a' - 'A')
		}
	}
	return string(runes)
}

// acsContains 检查字符串 s 是否包含子串 sub。
func acsContains(s, sub string) bool {
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
