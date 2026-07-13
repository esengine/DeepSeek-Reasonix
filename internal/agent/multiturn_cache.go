package agent

import "sync"

// ── OPT-53: 多轮缓存追踪器 (MultiTurnCacheTracker) ──
// 跨多轮对话追踪缓存命中/未命中情况，用于识别缓存失效趋势
// 和检测潜在的缓存未命中问题。
//
// 原理：多轮对话中，每一轮的 prompt 包含之前所有轮次的内容，
// 理论上前缀部分应该被缓存命中。如果命中率持续偏低或突然下降，
// 通常意味着：
//  1. system prompt 中插入了动态内容（时间戳、随机 ID 等）
//  2. 工具定义在轮次间发生了变化
//  3. 前缀被非确定性内容打断（如排序不稳定的列表）
//
// 效果：通过追踪每轮的缓存命中情况，能够：
//  - 计算整体缓存命中率
//  - 识别命中率趋势（上升/稳定/下降）
//  - 在出现完全未命中时触发告警
//  - 记录最佳连续命中轮次

// TurnCacheRecord 单轮缓存记录
type TurnCacheRecord struct {
	Turn            int     // 轮次序号（从 1 开始）
	PromptTokens    int     // 该轮 prompt 总 token 数
	CacheHitTokens  int     // 缓存命中 token 数
	CacheMissTokens int     // 缓存未命中 token 数
	HitRate         float64 // 本轮命中率（0.0 ~ 1.0）
	PrefixHash      string  // 前缀哈希（用于检测前缀变化）
}

// CacheTrend 缓存趋势
type CacheTrend struct {
	Direction   string  // "improving" | "stable" | "declining"
	RecentAvg   float64 // 最近 3 轮的平均命中率
	PreviousAvg float64 // 之前 3 轮的平均命中率
}

// MultiTurnCacheStats 多轮缓存统计
type MultiTurnCacheStats struct {
	TotalTurns      int     // 总轮次数
	TotalHitTokens  int     // 总命中 token 数
	TotalMissTokens int     // 总未命中 token 数
	OverallHitRate  float64 // 整体命中率
	BestStreak      int     // 最佳连续命中轮次
	CurrentStreak   int     // 当前连续命中轮次
}

// MultiTurnCacheTracker 多轮缓存追踪器
type MultiTurnCacheTracker struct {
	mu              sync.RWMutex
	records         []TurnCacheRecord
	totalHitTokens  int
	totalMissTokens int
	totalTurns      int
	bestStreak      int
	currentStreak   int
}

// NewMultiTurnCacheTracker 创建多轮缓存追踪器
func NewMultiTurnCacheTracker() *MultiTurnCacheTracker {
	return &MultiTurnCacheTracker{
		records: make([]TurnCacheRecord, 0),
	}
}

// RecordTurn 记录一轮对话的缓存情况
func (t *MultiTurnCacheTracker) RecordTurn(turn int, promptTokens int, cacheHit int, cacheMiss int, prefixHash string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 计算本轮命中率
	var hitRate float64
	total := cacheHit + cacheMiss
	if total > 0 {
		hitRate = float64(cacheHit) / float64(total)
	}

	// 创建记录
	record := TurnCacheRecord{
		Turn:            turn,
		PromptTokens:    promptTokens,
		CacheHitTokens:  cacheHit,
		CacheMissTokens: cacheMiss,
		HitRate:         hitRate,
		PrefixHash:      prefixHash,
	}

	// 更新累计总量
	t.totalHitTokens += cacheHit
	t.totalMissTokens += cacheMiss
	t.totalTurns++

	// 更新连续命中计数（cacheHit > 0 视为命中）
	if cacheHit > 0 {
		t.currentStreak++
		if t.currentStreak > t.bestStreak {
			t.bestStreak = t.currentStreak
		}
	} else {
		t.currentStreak = 0
	}

	// 追加记录
	t.records = append(t.records, record)
}

// GetCacheHitRate 返回整体缓存命中率
func (t *MultiTurnCacheTracker) GetCacheHitRate() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	total := t.totalHitTokens + t.totalMissTokens
	if total == 0 {
		return 0
	}
	return float64(t.totalHitTokens) / float64(total)
}

// GetTrend 分析缓存命中率的趋势
// 比较最近 3 轮与之前 3 轮的平均命中率。
func (t *MultiTurnCacheTracker) GetTrend() CacheTrend {
	t.mu.RLock()
	defer t.mu.RUnlock()

	n := len(t.records)

	// 不足 6 轮时无法比较前后各 3 轮，返回 stable
	if n < 6 {
		return CacheTrend{
			Direction:   "stable",
			RecentAvg:   t.recentAvg(n),
			PreviousAvg: 0,
		}
	}

	// 前 3 轮：n-6 ~ n-3
	// 后 3 轮：n-3 ~ n
	previousRecords := t.records[n-6 : n-3]
	recentRecords := t.records[n-3:]

	recentAvg := avgHitRate(recentRecords)
	previousAvg := avgHitRate(previousRecords)

	direction := "stable"
	switch {
	case recentAvg > previousAvg:
		direction = "improving"
	case recentAvg < previousAvg:
		direction = "declining"
	}

	return CacheTrend{
		Direction:   direction,
		RecentAvg:   recentAvg,
		PreviousAvg: previousAvg,
	}
}

// GetBestStreak 返回最佳连续命中轮次数
func (t *MultiTurnCacheTracker) GetBestStreak() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.bestStreak
}

// GetStats 返回多轮缓存统计信息
func (t *MultiTurnCacheTracker) GetStats() MultiTurnCacheStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	total := t.totalHitTokens + t.totalMissTokens
	var overallRate float64
	if total > 0 {
		overallRate = float64(t.totalHitTokens) / float64(total)
	}

	return MultiTurnCacheStats{
		TotalTurns:      t.totalTurns,
		TotalHitTokens:  t.totalHitTokens,
		TotalMissTokens: t.totalMissTokens,
		OverallHitRate:  overallRate,
		BestStreak:      t.bestStreak,
		CurrentStreak:   t.currentStreak,
	}
}

// Reset 重置追踪器状态
func (t *MultiTurnCacheTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.records = t.records[:0]
	t.totalHitTokens = 0
	t.totalMissTokens = 0
	t.totalTurns = 0
	t.bestStreak = 0
	t.currentStreak = 0
}

// ShouldAlert 判断是否需要告警
// 当最后一轮缓存命中数为 0 时返回 true（可能存在缓存未命中问题）。
func (t *MultiTurnCacheTracker) ShouldAlert() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.records) == 0 {
		return false
	}

	last := t.records[len(t.records)-1]
	return last.CacheHitTokens == 0
}

// recentAvg 计算现有记录的平均命中率（不足 3 轮时取全部）
func (t *MultiTurnCacheTracker) recentAvg(n int) float64 {
	if n == 0 {
		return 0
	}
	start := n - 3
	if start < 0 {
		start = 0
	}
	return avgHitRate(t.records[start:])
}

// avgHitRate 计算一组记录的平均命中率
func avgHitRate(records []TurnCacheRecord) float64 {
	if len(records) == 0 {
		return 0
	}
	sum := 0.0
	for _, r := range records {
		sum += r.HitRate
	}
	return sum / float64(len(records))
}
