package agent

import (
	"encoding/json"
	"sync"
	"time"
)

// ── OPT-37: Phantom 统计报告器 (Phantom Stats Reporter) ──
// 通过零 token 通道（phantom panel）推送 OPT 统计到前端。
//
// 原理：phantom panel 是项目的零 token UI 更新机制 — 通过 Go channel
// 异步推送状态变更，不经过 LLM。利用这个通道推送 OPT 统计：
// 1. 缓存命中率趋势
// 2. token 节省量
// 3. 成本追踪
// 4. 各 OPT 模块状态
//
// 效果：用户实时看到 token 优化效果，无需额外 API 调用。

// PhantomStatsReporter 统计报告器
type PhantomStatsReporter struct {
	mu sync.RWMutex

	// 上次报告时间
	lastReport time.Time

	// 报告间隔
	reportInterval time.Duration

	// 缓存的统计快照
	lastSnapshot *PhantomStatsSnapshot

	// 统计累计
	totalTokensSaved int
	totalCacheHits   int
	totalRequests    int
}

// PhantomStatsSnapshot 统计快照
type PhantomStatsSnapshot struct {
	Timestamp      time.Time `json:"timestamp"`
	CacheHitRate   float64   `json:"cacheHitRate"`
	TokensSaved    int       `json:"tokensSaved"`
	CostSaved      float64   `json:"costSaved"`
	ActiveOPTs     int       `json:"activeOPTs"`
	HealthStatus   string    `json:"healthStatus"`
	ProviderType   string    `json:"providerType"`
	TokenMode      string    `json:"tokenMode"`
}

// NewPhantomStatsReporter 创建报告器
func NewPhantomStatsReporter() *PhantomStatsReporter {
	return &PhantomStatsReporter{
		reportInterval: 30 * time.Second, // 每 30 秒报告一次
	}
}

// ShouldReport 检查是否应该报告
func (r *PhantomStatsReporter) ShouldReport() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return time.Since(r.lastReport) >= r.reportInterval
}

// CollectSnapshot 从 Agent 收集统计快照
func (r *PhantomStatsReporter) CollectSnapshot(a *Agent) *PhantomStatsSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.totalRequests++

	snapshot := &PhantomStatsSnapshot{
		Timestamp: time.Now(),
	}

	// 收集缓存健康状态
	if a.cacheHealthMonitor != nil {
		health := a.cacheHealthMonitor.GetHealth()
		snapshot.CacheHitRate = health.HitRate
		snapshot.HealthStatus = health.Status
	}

	// 收集 token 节省量
	if a.toolMemo != nil {
		stats := a.toolMemo.GetStats()
		snapshot.TokensSaved += stats.TokensSaved
		r.totalTokensSaved += stats.TokensSaved
	}
	if a.conversationDedup != nil {
		stats := a.conversationDedup.GetStats()
		snapshot.TokensSaved += stats.TokensSaved
		r.totalTokensSaved += stats.TokensSaved
	}

	// 收集成本节省
	if a.costEstimator != nil {
		stats := a.costEstimator.GetStats()
		snapshot.CostSaved = stats.TotalCost
	}

	// 收集 provider 类型
	if a.providerCacheStrategy != nil {
		stats := a.providerCacheStrategy.GetStats()
		snapshot.ProviderType = stats.Provider
	}

	// 统计活跃 OPT 数
	active := 0
	if a.cacheEnforcer != nil {
		active++
	}
	if a.toolMemo != nil {
		active++
	}
	if a.conversationDedup != nil {
		active++
	}
	if a.contextBudget != nil {
		active++
	}
	if a.cacheHealthMonitor != nil {
		active++
	}
	if a.prefixPinner != nil {
		active++
	}
	if a.semanticPruner != nil {
		active++
	}
	if a.prefetchPredictor != nil {
		active++
	}
	if a.windowPredictor != nil {
		active++
	}
	if a.costEstimator != nil {
		active++
	}
	snapshot.ActiveOPTs = active

	r.lastSnapshot = snapshot
	r.lastReport = time.Now()

	return snapshot
}

// ToJSON 将快照转为 JSON（用于 phantom panel 传输）
func (s *PhantomStatsSnapshot) ToJSON() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// GetLastSnapshot 获取上次快照
func (r *PhantomStatsReporter) GetLastSnapshot() *PhantomStatsSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastSnapshot
}

// GetStats 获取累计统计
func (r *PhantomStatsReporter) GetStats() ReporterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return ReporterStats{
		TotalTokensSaved: r.totalTokensSaved,
		TotalRequests:    r.totalRequests,
	}
}

// ReporterStats 报告器统计
type ReporterStats struct {
	TotalTokensSaved int `json:"totalTokensSaved"`
	TotalRequests    int `json:"totalRequests"`
}
