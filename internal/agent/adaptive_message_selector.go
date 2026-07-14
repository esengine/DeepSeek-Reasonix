package agent

import (
	"sync"

	"reasonix/internal/provider"
)

// ── OPT-90: 自适应消息选择器 (Adaptive Message Selector) ──
// 在给定 token 预算内自适应地选择保留哪些消息。
//
// 原理：始终保留系统提示与最近 2 条消息以保证指令与即时上下文完整；
// 对中间消息按重要性评分排序，在预算不足时优先丢弃重要性最低的消息。
// 策略（conservative/balanced/aggressive）调节丢弃的激进程度。
//
// 效果：在有限预算下最大化保留高价值信息，相比简单 FIFO 截断保留更多
// 关键上下文。

// AdaptiveMessageSelector 自适应消息选择器
type AdaptiveMessageSelector struct {
	mu                   sync.RWMutex
	totalSelections      int
	totalMessagesDropped int
	tokensSaved          int
	strategy             string
}

// AdaptiveSelectorStats 自适应选择器统计
type AdaptiveSelectorStats struct {
	TotalSelections      int
	TotalMessagesDropped int
	TokensSaved          int
	Strategy             string
}

// NewAdaptiveMessageSelector 创建自适应消息选择器，默认策略 "balanced"
func NewAdaptiveMessageSelector() *AdaptiveMessageSelector {
	return &AdaptiveMessageSelector{
		strategy: "balanced",
	}
}

// SelectMessages 在 token 预算内选择消息。
// 始终保留系统提示与最后 2 条消息；中间消息按重要性排序，预算不足时
// 从重要性最低的开始丢弃。
func (s *AdaptiveMessageSelector) SelectMessages(messages []provider.Message, tokenBudget int) []provider.Message {
	strategy := s.getStrategy()

	n := len(messages)
	if n == 0 {
		s.recordSelection(0, 0)
		return messages
	}

	// 始终保留：系统提示 + 最后 2 条
	keepIdx := make(map[int]bool)
	for i, m := range messages {
		if m.Role == provider.RoleSystem {
			keepIdx[i] = true
		}
	}
	for i := max(0, n-2); i < n; i++ {
		keepIdx[i] = true
	}

	// 中间消息 = 未被强制保留的
	type middleItem struct {
		idx    int
		score  float64
		tokens int
	}
	var middle []middleItem
	keptTokens := 0
	for i, m := range messages {
		tokens := s.EstimateMessageTokens(m)
		if keepIdx[i] {
			keptTokens += tokens
		} else {
			middle = append(middle, middleItem{
				idx:    i,
				score:  s.computeImportance(m, i, n, strategy),
				tokens: tokens,
			})
		}
	}

	// 贪心：按重要性从高到低尝试纳入，预算不足则丢弃
	dropped := 0
	droppedTokens := 0
	remaining := make([]middleItem, len(middle))
	copy(remaining, middle)
	for len(remaining) > 0 {
		// 找到当前最高分；同分时保留更靠前的消息（更早出现）
		best := 0
		for k := 1; k < len(remaining); k++ {
			if remaining[k].score > remaining[best].score {
				best = k
			} else if remaining[k].score == remaining[best].score && remaining[k].idx < remaining[best].idx {
				best = k
			}
		}
		it := remaining[best]
		if keptTokens+it.tokens <= tokenBudget {
			keepIdx[it.idx] = true
			keptTokens += it.tokens
		} else {
			dropped++
			droppedTokens += it.tokens
		}
		remaining = append(remaining[:best], remaining[best+1:]...)
	}

	// 按原始顺序构建结果
	result := make([]provider.Message, 0, len(keepIdx))
	for i, m := range messages {
		if keepIdx[i] {
			result = append(result, m)
		}
	}

	s.recordSelection(dropped, droppedTokens)
	return result
}

// EstimateMessageTokens 估算单条消息的 token 数（约 4 字符/token）
func (s *AdaptiveMessageSelector) EstimateMessageTokens(msg provider.Message) int {
	return len(msg.Content) / 4
}

// SetStrategy 设置选择策略：
//   - "conservative"：多保留（评分上浮）
//   - "balanced"：默认
//   - "aggressive"：多丢弃（评分下浮）
//
// 未知值回退为 "balanced"。
func (s *AdaptiveMessageSelector) SetStrategy(strategy string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch strategy {
	case "conservative", "balanced", "aggressive":
		s.strategy = strategy
	default:
		s.strategy = "balanced"
	}
}

// GetStats 返回选择器统计
func (s *AdaptiveMessageSelector) GetStats() AdaptiveSelectorStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return AdaptiveSelectorStats{
		TotalSelections:      s.totalSelections,
		TotalMessagesDropped: s.totalMessagesDropped,
		TokensSaved:          s.tokensSaved,
		Strategy:             s.strategy,
	}
}

// Reset 重置选择器统计（保留策略配置）
func (s *AdaptiveMessageSelector) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalSelections = 0
	s.totalMessagesDropped = 0
	s.tokensSaved = 0
}

// getStrategy 读取当前策略（加锁）
func (s *AdaptiveMessageSelector) getStrategy() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.strategy
}

// recordSelection 记录一次选择结果（加锁）
func (s *AdaptiveMessageSelector) recordSelection(dropped, droppedTokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalSelections++
	s.totalMessagesDropped += dropped
	s.tokensSaved += droppedTokens
}

// computeImportance 计算单条消息的重要性评分（用于中间消息排序）
func (s *AdaptiveMessageSelector) computeImportance(msg provider.Message, position, total int, strategy string) float64 {
	var base float64
	switch msg.Role {
	case provider.RoleSystem:
		base = 1.0
	case provider.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			base = 0.8
		} else {
			base = 0.5
		}
	case provider.RoleUser:
		base = 0.7
	case provider.RoleTool:
		base = 0.6
	default:
		base = 0.5
	}

	switch strategy {
	case "conservative":
		base += 0.15
	case "aggressive":
		base -= 0.15
	}

	// 近期中间消息小幅加成
	if total > 0 && position >= total-5 {
		base += 0.05
	}

	if base > 1.0 {
		base = 1.0
	}
	if base < 0 {
		base = 0
	}
	return base
}
