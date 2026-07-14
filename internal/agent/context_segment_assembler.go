package agent

import "sync"

// ── OPT-148: ContextSegmentAssembler (上下文片段组装器) ──
// 将多个命名片段高效组装为完整上下文。支持添加/移除片段、设置组装顺序，
// 并按顺序拼接片段内容。Token 估算使用字符数 len(content) 作为近似值。

// ContextSegmentAssembler 上下文片段组装器，将多个片段高效组装为完整上下文。
type ContextSegmentAssembler struct {
	mu                   sync.RWMutex
	segments             map[string]string
	totalAssemblies      int
	totalSegments        int
	totalTokensAssembled int
	assemblyOrder        []string
}

// NewContextSegmentAssembler 创建一个新的上下文片段组装器。
func NewContextSegmentAssembler() *ContextSegmentAssembler {
	return &ContextSegmentAssembler{
		segments:      make(map[string]string),
		assemblyOrder: []string{},
	}
}

// AddSegment 添加或更新一个命名片段。
// 若片段为新增，则追加到组装顺序末尾。
func (a *ContextSegmentAssembler) AddSegment(name string, content string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.segments[name]; !exists {
		a.assemblyOrder = append(a.assemblyOrder, name)
	}
	a.segments[name] = content
}

// RemoveSegment 移除指定名称的片段，同时从组装顺序中移除。
func (a *ContextSegmentAssembler) RemoveSegment(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.segments, name)
	a.assemblyOrder = csaRemoveFromSlice(a.assemblyOrder, name)
}

// SetOrder 设置片段的组装顺序。
// names 中的名称顺序即为组装时的拼接顺序。
func (a *ContextSegmentAssembler) SetOrder(names []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assemblyOrder = csaCopySlice(names)
}

// Assemble 按组装顺序拼接所有片段为完整上下文字符串。
// 跳过组装顺序中不存在对应片段的名称。每次调用更新组装统计。
func (a *ContextSegmentAssembler) Assemble() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	var result string
	segmentCount := 0
	for _, name := range a.assemblyOrder {
		if content, ok := a.segments[name]; ok {
			result += content
			segmentCount++
		}
	}

	a.totalAssemblies++
	a.totalSegments += segmentCount
	a.totalTokensAssembled += csaTokenCount(result)
	return result
}

// GetStats 返回组装器的统计信息，包括 totalAssemblies、totalSegments、
// totalTokensAssembled、segmentCount 和 avgAssemblyTokens。
func (a *ContextSegmentAssembler) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	avgTokens := 0.0
	if a.totalAssemblies > 0 {
		avgTokens = float64(a.totalTokensAssembled) / float64(a.totalAssemblies)
	}

	return map[string]interface{}{
		"totalAssemblies":      a.totalAssemblies,
		"totalSegments":        a.totalSegments,
		"totalTokensAssembled": a.totalTokensAssembled,
		"segmentCount":         len(a.segments),
		"avgAssemblyTokens":    avgTokens,
	}
}

// Reset 重置组装器的所有状态，包括片段列表、组装顺序和统计计数。
func (a *ContextSegmentAssembler) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.segments = make(map[string]string)
	a.assemblyOrder = []string{}
	a.totalAssemblies = 0
	a.totalSegments = 0
	a.totalTokensAssembled = 0
}

// csaTokenCount 估算字符串的 token 数量（使用字符数 len(s) 作为近似值）。
func csaTokenCount(s string) int {
	return len(s)
}

// csaCopySlice 复制字符串切片，返回新切片。
func csaCopySlice(s []string) []string {
	result := make([]string, len(s))
	copy(result, s)
	return result
}

// csaRemoveFromSlice 从字符串切片中移除所有等于 item 的元素。
func csaRemoveFromSlice(s []string, item string) []string {
	result := make([]string, 0, len(s))
	for _, v := range s {
		if v != item {
			result = append(result, v)
		}
	}
	return result
}
