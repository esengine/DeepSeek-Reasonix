package agent

import (
	"sync"
	"time"
)

// ── OPT-95: ZeroTokenStatsCollector (零开销统计收集器) ──
// 以零 token 开销的方式从所有 OPT 模块收集统计信息（惰性收集）。
//
// 原理：直接在每轮对话中聚合所有 OPT 模块的统计会消耗上下文 token。
// ZeroTokenStatsCollector 采用惰性收集策略：仅在需要时（或经过足够
// 间隔后）遍历各模块并通过 GetStats() 提取统计，结果缓存供后续读取，
// 避免每轮都产生收集开销。
//
// 效果：将统计聚合的开销从"每轮"降低到"按需/按间隔"，在不损失
// 可观测性的前提下节省上下文 token。

// CollectedStats 一次收集得到的聚合统计
type CollectedStats struct {
	ModuleCount       int                    // 成功收集统计的模块数量
	CollectedAt       int64                  // 收集时间戳（Unix 毫秒）
	Stats             map[string]interface{} // 模块名 -> 统计信息
	CollectDurationMs int64                  // 收集耗时（毫秒）
}

// ZeroTokenStatsInfo 收集器自身的统计信息
type ZeroTokenStatsInfo struct {
	ModuleCount           int   // 上次成功收集的模块数量
	LastCollectedAt       int64 // 上次收集时间戳（Unix 毫秒）
	LastCollectDurationMs int64 // 上次收集耗时（毫秒）
}

// ztStatsProvider 由暴露通用 GetStats() 的模块实现。
// 仅当模块的 GetStats 返回 interface{} 时才能通过类型断言收集；
// 返回具体类型的模块不满足该接口，将被跳过。
type ztStatsProvider interface {
	GetStats() interface{}
}

// ZeroTokenStatsCollector 零开销统计收集器
// 惰性收集各 OPT 模块的统计信息并缓存结果。
type ZeroTokenStatsCollector struct {
	mu                  sync.RWMutex
	collectedAt         int64
	statsCache          map[string]interface{}
	moduleCount         int
	lastCollectDuration int64
}

// NewZeroTokenStatsCollector 创建零开销统计收集器
func NewZeroTokenStatsCollector() *ZeroTokenStatsCollector {
	return &ZeroTokenStatsCollector{
		statsCache: make(map[string]interface{}),
	}
}

// CollectStats 遍历 modules，通过类型断言调用其 GetStats()（若可用），
// 将结果存入缓存并返回。
//
// 仅实现了 ztStatsProvider（即 GetStats() interface{}）的模块会被收集；
// 其余模块被跳过。
func (c *ZeroTokenStatsCollector) CollectStats(modules map[string]interface{}) CollectedStats {
	start := time.Now()

	collected := make(map[string]interface{}, len(modules))
	for name, mod := range modules {
		if sp, ok := mod.(ztStatsProvider); ok {
			collected[name] = sp.GetStats()
		}
	}

	durationMs := time.Since(start).Milliseconds()
	nowMs := time.Now().UnixMilli()

	c.mu.Lock()
	c.collectedAt = nowMs
	c.statsCache = collected
	c.moduleCount = len(collected)
	c.lastCollectDuration = durationMs
	c.mu.Unlock()

	return CollectedStats{
		ModuleCount:       len(collected),
		CollectedAt:       nowMs,
		Stats:             ztCopyStatsMap(collected),
		CollectDurationMs: durationMs,
	}
}

// ShouldCollect 返回是否距上次收集已过去足够时间（intervalMs 毫秒）。
// 若从未收集过，始终返回 true。
func (c *ZeroTokenStatsCollector) ShouldCollect(intervalMs int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.collectedAt == 0 {
		return true
	}
	return time.Now().UnixMilli()-c.collectedAt >= intervalMs
}

// GetCachedStats 返回缓存的统计而不重新收集。若从未收集过则返回空 map。
func (c *ZeroTokenStatsCollector) GetCachedStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ztCopyStatsMap(c.statsCache)
}

// GetStats 返回收集器自身的统计信息
func (c *ZeroTokenStatsCollector) GetStats() ZeroTokenStatsInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ZeroTokenStatsInfo{
		ModuleCount:           c.moduleCount,
		LastCollectedAt:       c.collectedAt,
		LastCollectDurationMs: c.lastCollectDuration,
	}
}

// Reset 重置收集器，清除缓存与统计
func (c *ZeroTokenStatsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectedAt = 0
	c.statsCache = make(map[string]interface{})
	c.moduleCount = 0
	c.lastCollectDuration = 0
}

// ---------------------------------------------------------------------------
// 内部辅助函数（以 zt 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// ztCopyStatsMap 返回统计 map 的浅拷贝，防止外部修改污染缓存。
func ztCopyStatsMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
