package agent

import "sync"

// ── OPT-127: ContextFreshnessTracker (上下文新鲜度追踪器) ──
// 追踪每条消息的新鲜度，根据当前轮次与消息所在轮次的差值判断消息
// 是否仍然"新鲜"。超过最大年龄（maxAge）的消息视为过期，可被清理。
//
// 原理：对话上下文中，越早的消息往往越不重要。ContextFreshnessTracker
// 为每条消息记录其所属轮次，通过 currentTurn - msgTurn < maxAge 判定
// 新鲜度，帮助上下文管理器优先保留近期消息。
//
// 效果：为上下文压缩和消息淘汰提供新鲜度依据，避免清理近期关键信息。

// ContextFreshnessTracker 追踪消息新鲜度的追踪器。
type ContextFreshnessTracker struct {
	mu                sync.RWMutex
	messageTimestamps map[int]int
	totalTracked      int
	totalExpired      int
	maxAge            int
	currentTurn       int
}

// NewContextFreshnessTracker 创建一个新的 ContextFreshnessTracker。
// maxAge 指定消息的最大新鲜年龄（轮次差），超过则视为过期。
func NewContextFreshnessTracker(maxAge int) *ContextFreshnessTracker {
	return &ContextFreshnessTracker{
		messageTimestamps: make(map[int]int),
		maxAge:            maxAge,
	}
}

// TrackMessage 追踪一条消息，记录其所属轮次。
// 若该消息索引为新消息，则递增总追踪计数。
func (t *ContextFreshnessTracker) TrackMessage(msgIndex int, turn int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.messageTimestamps[msgIndex]; !exists {
		t.totalTracked++
	}
	t.messageTimestamps[msgIndex] = turn
}

// UpdateTurn 更新当前轮次。
func (t *ContextFreshnessTracker) UpdateTurn(turn int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentTurn = turn
}

// GetFreshMessages 返回仍然新鲜的消息索引列表。
// 新鲜判定条件：currentTurn - msgTurn < maxAge。
// 返回的索引按升序排列。
func (t *ContextFreshnessTracker) GetFreshMessages() []int {
	t.mu.Lock()
	defer t.mu.Unlock()

	fresh := cftFreshIndicesLocked(t.messageTimestamps, t.currentTurn, t.maxAge)
	expired := t.totalTracked - len(fresh)
	if expired < 0 {
		expired = 0
	}
	t.totalExpired = expired
	return fresh
}

// GetFreshnessRatio 返回新鲜消息数占总消息数的比例。
// 若无消息被追踪，返回 0。
func (t *ContextFreshnessTracker) GetFreshnessRatio() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.totalTracked == 0 {
		return 0
	}
	fresh := cftFreshIndicesLocked(t.messageTimestamps, t.currentTurn, t.maxAge)
	return float64(len(fresh)) / float64(t.totalTracked)
}

// GetStats 返回追踪器的统计信息。
func (t *ContextFreshnessTracker) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	fresh := cftFreshIndicesLocked(t.messageTimestamps, t.currentTurn, t.maxAge)
	freshCount := len(fresh)
	expired := t.totalTracked - freshCount
	if expired < 0 {
		expired = 0
	}
	ratio := 0.0
	if t.totalTracked > 0 {
		ratio = float64(freshCount) / float64(t.totalTracked)
	}

	return map[string]interface{}{
		"totalTracked":   t.totalTracked,
		"totalExpired":   expired,
		"freshnessRatio": ratio,
		"currentTurn":    t.currentTurn,
		"maxAge":         t.maxAge,
	}
}

// Reset 清除所有追踪记录和统计信息，保留 maxAge 配置。
func (t *ContextFreshnessTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messageTimestamps = make(map[int]int)
	t.totalTracked = 0
	t.totalExpired = 0
	t.currentTurn = 0
}

// cftFreshIndicesLocked 返回仍新鲜的消息索引列表（升序）。
// 调用方需持有读锁或写锁。
func cftFreshIndicesLocked(timestamps map[int]int, currentTurn int, maxAge int) []int {
	var fresh []int
	for msgIndex, turn := range timestamps {
		if cftIsFresh(turn, currentTurn, maxAge) {
			fresh = append(fresh, msgIndex)
		}
	}
	return cftSortInts(fresh)
}

// cftIsFresh 判断消息是否新鲜。
// 新鲜条件：currentTurn - msgTurn < maxAge。
func cftIsFresh(msgTurn int, currentTurn int, maxAge int) bool {
	return currentTurn-msgTurn < maxAge
}

// cftSortInts 对整数切片进行升序排序（插入排序），返回排序后的切片。
func cftSortInts(s []int) []int {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
	return s
}
