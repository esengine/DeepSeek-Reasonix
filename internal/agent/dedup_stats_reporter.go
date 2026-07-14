package agent

import (
	"sync"
	"time"
)

// ── OPT-66: 去重统计报告器 (DedupStatsReporter) ──
// 汇聚所有 OPT 模块的去重统计数据，提供统一的报告视图。
//
// 原理：各个 OPT 模块独立进行去重操作（对话去重、增量缓存、跨轮去重等），
// 但缺少一个集中的统计入口。DedupStatsReporter 将所有模块的去重次数、
// 节省 token 数按模块归类汇总，并支持定期报告触发判断。
//
// 效果：为监控系统提供全局去重效果概览，便于调优各模块参数。

// DedupStatsReporter 去重统计报告器，汇聚所有 OPT 模块的去重数据
type DedupStatsReporter struct {
	mu               sync.RWMutex
	totalDedups      int
	totalTokensSaved int
	byModule         map[string]int
	lastReportTime   int64
}

// DedupReport 去重统计报告快照
type DedupReport struct {
	TotalDedups      int
	TotalTokensSaved int
	ByModule         map[string]int
	ReportAge        int64 // 距上次报告的秒数
}

// NewDedupStatsReporter 创建新的去重统计报告器
func NewDedupStatsReporter() *DedupStatsReporter {
	return &DedupStatsReporter{
		byModule:       make(map[string]int),
		lastReportTime: time.Now().Unix(),
	}
}

// RecordDedup 记录一次去重操作
func (r *DedupStatsReporter) RecordDedup(module string, tokensSaved int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalDedups++
	r.totalTokensSaved += tokensSaved
	r.byModule[module] += tokensSaved
}

// GetReport 获取当前去重统计报告快照
func (r *DedupStatsReporter) GetReport() DedupReport {
	r.mu.RLock()
	defer r.mu.RUnlock()

	byModuleCopy := make(map[string]int, len(r.byModule))
	for k, v := range r.byModule {
		byModuleCopy[k] = v
	}

	return DedupReport{
		TotalDedups:      r.totalDedups,
		TotalTokensSaved: r.totalTokensSaved,
		ByModule:         byModuleCopy,
		ReportAge:        time.Now().Unix() - r.lastReportTime,
	}
}

// ShouldReport 判断是否应该生成报告（距上次报告超过 60 秒）
func (r *DedupStatsReporter) ShouldReport() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return (time.Now().Unix() - r.lastReportTime) >= 60
}

// Reset 重置所有统计数据并更新报告时间
func (r *DedupStatsReporter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalDedups = 0
	r.totalTokensSaved = 0
	r.byModule = make(map[string]int)
	r.lastReportTime = time.Now().Unix()
}
