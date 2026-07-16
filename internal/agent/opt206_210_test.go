package agent

import "testing"

// ── OPT-206: TokenAwareResourcePool 测试 ──

// TestTokenAwareResourcePool_AcquireEmpty 验证空池 Acquire 返回 ("", false)。
func TestTokenAwareResourcePool_AcquireEmpty(t *testing.T) {
	pool := NewTokenAwareResourcePool(8)
	resource, ok := pool.Acquire()
	if ok != false {
		t.Errorf("expected ok=false for empty pool, got %v", ok)
	}
	if resource != "" {
		t.Errorf("expected empty resource for empty pool, got %q", resource)
	}
	// 空池 Acquire 应计入 allocCount
	if pool.GetStats()["allocCount"].(int) != 1 {
		t.Errorf("expected allocCount=1 after empty Acquire, got %v", pool.GetStats()["allocCount"])
	}
}

// TestTokenAwareResourcePool_ReleaseAndAcquireReuse 验证 Release 后 Acquire 能复用资源。
func TestTokenAwareResourcePool_ReleaseAndAcquireReuse(t *testing.T) {
	pool := NewTokenAwareResourcePool(8)
	pool.Release("resource-1")
	resource, ok := pool.Acquire()
	if ok != true {
		t.Errorf("expected ok=true after Release, got %v", ok)
	}
	if resource != "resource-1" {
		t.Errorf("expected resource-1 to be reused, got %q", resource)
	}
	// 复用后池应被清空
	if pool.GetPoolSize() != 0 {
		t.Errorf("expected pool size 0 after reuse, got %d", pool.GetPoolSize())
	}
}

// TestTokenAwareResourcePool_GetReuseRate 验证复用率计算。
// 复用率 = 复用次数 / (复用次数 + 分配次数)。
func TestTokenAwareResourcePool_GetReuseRate(t *testing.T) {
	pool := NewTokenAwareResourcePool(8)
	// 空池 Acquire -> allocCount=1, reuseCount=0
	pool.Acquire()
	// Release 再 Acquire -> reuseCount=1
	pool.Release("r1")
	pool.Acquire()
	rate := pool.GetReuseRate()
	// reuseRate = 1 / (1 + 1) = 0.5
	if rate != 0.5 {
		t.Errorf("expected reuse rate 0.5, got %v", rate)
	}
}

// TestTokenAwareResourcePool_StatsReuseCount 验证 Stats 中的 reuseCount 统计。
func TestTokenAwareResourcePool_StatsReuseCount(t *testing.T) {
	pool := NewTokenAwareResourcePool(8)
	pool.Release("r1")
	pool.Release("r2")
	pool.Acquire() // reuseCount=1
	pool.Acquire() // reuseCount=2
	stats := pool.GetStats()
	if stats["reuseCount"].(int) != 2 {
		t.Errorf("expected reuseCount=2, got %v", stats["reuseCount"])
	}
	if stats["releaseCount"].(int) != 2 {
		t.Errorf("expected releaseCount=2, got %v", stats["releaseCount"])
	}
	if stats["allocCount"].(int) != 0 {
		t.Errorf("expected allocCount=0, got %v", stats["allocCount"])
	}
}

// TestTokenAwareResourcePool_Reset 验证 Reset 清空计数和池但保留 maxPoolSize。
func TestTokenAwareResourcePool_Reset(t *testing.T) {
	pool := NewTokenAwareResourcePool(8)
	pool.Release("r1")
	pool.Acquire()
	pool.Reset()
	if pool.GetPoolSize() != 0 {
		t.Errorf("expected pool size 0 after Reset, got %d", pool.GetPoolSize())
	}
	stats := pool.GetStats()
	if stats["reuseCount"].(int) != 0 {
		t.Errorf("expected reuseCount=0 after Reset, got %v", stats["reuseCount"])
	}
	if stats["releaseCount"].(int) != 0 {
		t.Errorf("expected releaseCount=0 after Reset, got %v", stats["releaseCount"])
	}
	// maxPoolSize 配置应保留
	if stats["maxPoolSize"].(int) != 8 {
		t.Errorf("expected maxPoolSize=8 preserved after Reset, got %v", stats["maxPoolSize"])
	}
}

// ── OPT-207: CacheInvalidationBatcher 测试 ──

// TestCacheInvalidationBatcher_AddToMax 验证 Add 到 maxBatchSize 时返回 true。
func TestCacheInvalidationBatcher_AddToMax(t *testing.T) {
	b := NewCacheInvalidationBatcher(3)
	if b.Add("k1") {
		t.Errorf("expected false before reaching max, got true")
	}
	if b.Add("k2") {
		t.Errorf("expected false before reaching max, got true")
	}
	// 第 3 个 key 使批次满，应返回 true
	if !b.Add("k3") {
		t.Errorf("expected true when reaching max batch size, got false")
	}
}

// TestCacheInvalidationBatcher_Flush 验证 Flush 返回所有 key 并清空批次。
func TestCacheInvalidationBatcher_Flush(t *testing.T) {
	b := NewCacheInvalidationBatcher(8)
	b.Add("k1")
	b.Add("k2")
	b.Add("k3")
	keys := b.Flush()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys from Flush, got %d", len(keys))
	}
	// Flush 后批次应清空
	if b.GetBatchCount() != 0 {
		t.Errorf("expected batch count 0 after Flush, got %d", b.GetBatchCount())
	}
}

// TestCacheInvalidationBatcher_GetBatchCount 验证 GetBatchCount 返回当前批次大小。
func TestCacheInvalidationBatcher_GetBatchCount(t *testing.T) {
	b := NewCacheInvalidationBatcher(8)
	if b.GetBatchCount() != 0 {
		t.Errorf("expected batch count 0 initially, got %d", b.GetBatchCount())
	}
	b.Add("k1")
	b.Add("k2")
	if b.GetBatchCount() != 2 {
		t.Errorf("expected batch count 2 after two Adds, got %d", b.GetBatchCount())
	}
}

// TestCacheInvalidationBatcher_StatsBatchedFlushed 验证 Stats 中的 totalBatched/totalFlushed 统计。
func TestCacheInvalidationBatcher_StatsBatchedFlushed(t *testing.T) {
	b := NewCacheInvalidationBatcher(8)
	b.Add("k1")
	b.Flush() // totalBatched=1, totalFlushed=1
	b.Add("k2")
	b.Add("k3")
	b.Flush() // totalBatched=3, totalFlushed=3
	stats := b.GetStats()
	if stats["totalBatched"].(int) != 3 {
		t.Errorf("expected totalBatched=3, got %v", stats["totalBatched"])
	}
	if stats["totalFlushed"].(int) != 3 {
		t.Errorf("expected totalFlushed=3, got %v", stats["totalFlushed"])
	}
}

// TestCacheInvalidationBatcher_Reset 验证 Reset 清空批次和计数但保留 batchSize。
func TestCacheInvalidationBatcher_Reset(t *testing.T) {
	b := NewCacheInvalidationBatcher(8)
	b.Add("k1")
	b.Flush()
	b.Add("k2")
	b.Reset()
	if b.GetBatchCount() != 0 {
		t.Errorf("expected batch count 0 after Reset, got %d", b.GetBatchCount())
	}
	stats := b.GetStats()
	if stats["totalBatched"].(int) != 0 {
		t.Errorf("expected totalBatched=0 after Reset, got %v", stats["totalBatched"])
	}
	if stats["totalFlushed"].(int) != 0 {
		t.Errorf("expected totalFlushed=0 after Reset, got %v", stats["totalFlushed"])
	}
	// batchSize 配置应保留
	if stats["batchSize"].(int) != 8 {
		t.Errorf("expected batchSize=8 preserved after Reset, got %v", stats["batchSize"])
	}
}

// ── OPT-208: ContextFidelityMonitor 测试 ──

// TestContextFidelityMonitor_CheckFidelitySameText 验证相同文本返回 0 损失。
func TestContextFidelityMonitor_CheckFidelitySameText(t *testing.T) {
	m := NewContextFidelityMonitor()
	text := "hello world context"
	loss := m.CheckFidelity(text, text)
	if loss != 0 {
		t.Errorf("expected 0 loss for identical text, got %v", loss)
	}
}

// TestContextFidelityMonitor_CheckFidelityDifferentText 验证不同（更短）文本返回正损失。
func TestContextFidelityMonitor_CheckFidelityDifferentText(t *testing.T) {
	m := NewContextFidelityMonitor()
	original := "hello world context here"
	processed := "hello" // 明显更短
	loss := m.CheckFidelity(original, processed)
	if loss <= 0 {
		t.Errorf("expected positive loss for shortened text, got %v", loss)
	}
	if loss > 1 {
		t.Errorf("expected loss <= 1, got %v", loss)
	}
}

// TestContextFidelityMonitor_GetAvgLoss 验证平均损失计算。
func TestContextFidelityMonitor_GetAvgLoss(t *testing.T) {
	m := NewContextFidelityMonitor()
	m.CheckFidelity("hello", "hello")       // loss 0
	m.CheckFidelity("hello world", "hello") // loss = 1 - 5/11
	avg := m.GetAvgLoss()
	if avg <= 0 {
		t.Errorf("expected positive avg loss, got %v", avg)
	}
	// 空监控器应返回 0
	empty := NewContextFidelityMonitor()
	if empty.GetAvgLoss() != 0 {
		t.Errorf("expected 0 avg loss for fresh monitor, got %v", empty.GetAvgLoss())
	}
}

// TestContextFidelityMonitor_GetMaxLoss 验证最大损失追踪。
func TestContextFidelityMonitor_GetMaxLoss(t *testing.T) {
	m := NewContextFidelityMonitor()
	m.CheckFidelity("hello world foo bar", "hello world") // 较小损失
	maxBefore := m.GetMaxLoss()
	m.CheckFidelity("abcdefghij", "a") // 更大损失
	maxAfter := m.GetMaxLoss()
	if maxAfter <= maxBefore {
		t.Errorf("expected max loss to increase, before=%v after=%v", maxBefore, maxAfter)
	}
}

// TestContextFidelityMonitor_StatsChecks 验证 Stats 中的 checks 统计。
func TestContextFidelityMonitor_StatsChecks(t *testing.T) {
	m := NewContextFidelityMonitor()
	m.CheckFidelity("abc", "abc")    // loss 0
	m.CheckFidelity("abcdef", "abc") // loss 0.5 > 0
	m.CheckFidelity("xyz", "xyz")    // loss 0
	stats := m.GetStats()
	if stats["checks"].(int) != 3 {
		t.Errorf("expected checks=3, got %v", stats["checks"])
	}
	// 仅 1 次发生损失
	if stats["fidelityLosses"].(int) != 1 {
		t.Errorf("expected fidelityLosses=1, got %v", stats["fidelityLosses"])
	}
}

// TestContextFidelityMonitor_Reset 验证 Reset 清空所有计数和分数。
func TestContextFidelityMonitor_Reset(t *testing.T) {
	m := NewContextFidelityMonitor()
	m.CheckFidelity("abcdef", "abc")
	m.Reset()
	if m.GetAvgLoss() != 0 {
		t.Errorf("expected avg loss 0 after Reset, got %v", m.GetAvgLoss())
	}
	if m.GetMaxLoss() != 0 {
		t.Errorf("expected max loss 0 after Reset, got %v", m.GetMaxLoss())
	}
	stats := m.GetStats()
	if stats["checks"].(int) != 0 {
		t.Errorf("expected checks=0 after Reset, got %v", stats["checks"])
	}
	if stats["fidelityLosses"].(int) != 0 {
		t.Errorf("expected fidelityLosses=0 after Reset, got %v", stats["fidelityLosses"])
	}
}

// ── OPT-209: TokenAwareConcurrencyLimiter 测试 ──

// TestTokenAwareConcurrencyLimiter_AcquireWithinMax 验证在 maxConcurrent 内 Acquire 返回 true。
func TestTokenAwareConcurrencyLimiter_AcquireWithinMax(t *testing.T) {
	l := NewTokenAwareConcurrencyLimiter(3)
	if !l.Acquire(100) {
		t.Errorf("expected Acquire to succeed within max, got false")
	}
	if !l.Acquire(200) {
		t.Errorf("expected Acquire to succeed within max, got false")
	}
	if l.GetActiveCount() != 2 {
		t.Errorf("expected active count 2, got %d", l.GetActiveCount())
	}
}

// TestTokenAwareConcurrencyLimiter_AcquireFullReturnsFalse 验证并发满时 Acquire 返回 false。
func TestTokenAwareConcurrencyLimiter_AcquireFullReturnsFalse(t *testing.T) {
	l := NewTokenAwareConcurrencyLimiter(2)
	l.Acquire(100)
	l.Acquire(200)
	// 已满，第三次应被拒绝
	if l.Acquire(300) {
		t.Errorf("expected Acquire to fail when full, got true")
	}
}

// TestTokenAwareConcurrencyLimiter_ReleaseAndReacquire 验证 Release 释放后可重新 Acquire。
func TestTokenAwareConcurrencyLimiter_ReleaseAndReacquire(t *testing.T) {
	l := NewTokenAwareConcurrencyLimiter(1)
	l.Acquire(100)
	if l.Acquire(200) {
		t.Errorf("expected Acquire to fail when full, got true")
	}
	l.Release()
	if !l.Acquire(300) {
		t.Errorf("expected Acquire to succeed after Release, got false")
	}
}

// TestTokenAwareConcurrencyLimiter_AdjustConcurrency 验证动态调整最大并发度。
func TestTokenAwareConcurrencyLimiter_AdjustConcurrency(t *testing.T) {
	l := NewTokenAwareConcurrencyLimiter(2)
	l.Acquire(100)
	l.Acquire(200)
	// max=2 已满
	if l.Acquire(300) {
		t.Errorf("expected Acquire to fail at max 2, got true")
	}
	// 提升并发度到 4
	l.AdjustConcurrency(4)
	if !l.Acquire(400) {
		t.Errorf("expected Acquire to succeed after AdjustConcurrency to 4, got false")
	}
	if l.GetActiveCount() != 3 {
		t.Errorf("expected active count 3, got %d", l.GetActiveCount())
	}
}

// TestTokenAwareConcurrencyLimiter_StatsTotalAdmitted 验证 Stats 中的 totalAdmitted 统计。
func TestTokenAwareConcurrencyLimiter_StatsTotalAdmitted(t *testing.T) {
	l := NewTokenAwareConcurrencyLimiter(2)
	l.Acquire(100)
	l.Acquire(200)
	l.Acquire(300) // 被拒绝
	stats := l.GetStats()
	if stats["totalAdmitted"].(int) != 2 {
		t.Errorf("expected totalAdmitted=2, got %v", stats["totalAdmitted"])
	}
	if stats["totalRejected"].(int) != 1 {
		t.Errorf("expected totalRejected=1, got %v", stats["totalRejected"])
	}
	// avgTokenCost = (100 + 200) / 2 = 150
	if stats["avgTokenCost"].(int) != 150 {
		t.Errorf("expected avgTokenCost=150, got %v", stats["avgTokenCost"])
	}
}

// TestTokenAwareConcurrencyLimiter_Reset 验证 Reset 清空计数但保留 maxConcurrent。
func TestTokenAwareConcurrencyLimiter_Reset(t *testing.T) {
	l := NewTokenAwareConcurrencyLimiter(3)
	l.Acquire(100)
	l.Acquire(200)
	l.Reset()
	if l.GetActiveCount() != 0 {
		t.Errorf("expected active count 0 after Reset, got %d", l.GetActiveCount())
	}
	stats := l.GetStats()
	if stats["totalAdmitted"].(int) != 0 {
		t.Errorf("expected totalAdmitted=0 after Reset, got %v", stats["totalAdmitted"])
	}
	if stats["totalRejected"].(int) != 0 {
		t.Errorf("expected totalRejected=0 after Reset, got %v", stats["totalRejected"])
	}
	// maxConcurrent 配置应保留
	if stats["maxConcurrent"].(int) != 3 {
		t.Errorf("expected maxConcurrent=3 preserved after Reset, got %v", stats["maxConcurrent"])
	}
}

// ── OPT-210: PromptCacheWarmingScheduler 测试 ──

// TestPromptCacheWarmingScheduler_ScheduleGetNextHighestPriority 验证最高优先级先出。
func TestPromptCacheWarmingScheduler_ScheduleGetNextHighestPriority(t *testing.T) {
	s := NewPromptCacheWarmingScheduler()
	s.Schedule("low", 1)
	s.Schedule("high", 10)
	s.Schedule("mid", 5)
	key, ok := s.GetNext()
	if !ok {
		t.Errorf("expected ok=true, got false")
	}
	if key != "high" {
		t.Errorf("expected highest priority key 'high', got %q", key)
	}
}

// TestPromptCacheWarmingScheduler_MarkWarmedIsWarmed 验证 MarkWarmed + IsWarmed 标记。
func TestPromptCacheWarmingScheduler_MarkWarmedIsWarmed(t *testing.T) {
	s := NewPromptCacheWarmingScheduler()
	s.Schedule("k1", 1)
	if s.IsWarmed("k1") {
		t.Errorf("expected k1 not warmed before MarkWarmed")
	}
	s.MarkWarmed("k1")
	if !s.IsWarmed("k1") {
		t.Errorf("expected k1 warmed after MarkWarmed")
	}
	// MarkWarmed 后该 key 不再出现在待预热队列
	key, ok := s.GetNext()
	if ok {
		t.Errorf("expected ok=false after MarkWarmed, got true (key=%q)", key)
	}
}

// TestPromptCacheWarmingScheduler_GetNextEmptyReturnsFalse 验证空队列 GetNext 返回 false。
func TestPromptCacheWarmingScheduler_GetNextEmptyReturnsFalse(t *testing.T) {
	s := NewPromptCacheWarmingScheduler()
	key, ok := s.GetNext()
	if ok {
		t.Errorf("expected ok=false for empty queue, got true")
	}
	if key != "" {
		t.Errorf("expected empty key for empty queue, got %q", key)
	}
}

// TestPromptCacheWarmingScheduler_StatsScheduledCount 验证 Stats 中的 scheduledCount 统计。
func TestPromptCacheWarmingScheduler_StatsScheduledCount(t *testing.T) {
	s := NewPromptCacheWarmingScheduler()
	s.Schedule("k1", 1)
	s.Schedule("k2", 2)
	s.Schedule("k3", 3)
	stats := s.GetStats()
	if stats["scheduledCount"].(int) != 3 {
		t.Errorf("expected scheduledCount=3, got %v", stats["scheduledCount"])
	}
	if stats["pendingCount"].(int) != 3 {
		t.Errorf("expected pendingCount=3, got %v", stats["pendingCount"])
	}
}

// TestPromptCacheWarmingScheduler_ScheduleSkipsWarmed 验证对已预热 key 调度时跳过并计 skippedCount。
func TestPromptCacheWarmingScheduler_ScheduleSkipsWarmed(t *testing.T) {
	s := NewPromptCacheWarmingScheduler()
	s.Schedule("k1", 1)
	s.MarkWarmed("k1")
	// 对已预热 key 再次调度应被跳过
	s.Schedule("k1", 5)
	stats := s.GetStats()
	if stats["scheduledCount"].(int) != 1 {
		t.Errorf("expected scheduledCount=1 (no double count), got %v", stats["scheduledCount"])
	}
	if stats["skippedCount"].(int) != 1 {
		t.Errorf("expected skippedCount=1, got %v", stats["skippedCount"])
	}
}

// TestPromptCacheWarmingScheduler_Reset 验证 Reset 清空所有集合和计数。
func TestPromptCacheWarmingScheduler_Reset(t *testing.T) {
	s := NewPromptCacheWarmingScheduler()
	s.Schedule("k1", 1)
	s.Schedule("k2", 2)
	s.MarkWarmed("k1")
	s.Reset()
	key, ok := s.GetNext()
	if ok {
		t.Errorf("expected ok=false after Reset, got true (key=%q)", key)
	}
	if s.IsWarmed("k1") {
		t.Errorf("expected k1 not warmed after Reset")
	}
	stats := s.GetStats()
	if stats["scheduledCount"].(int) != 0 {
		t.Errorf("expected scheduledCount=0 after Reset, got %v", stats["scheduledCount"])
	}
	if stats["warmedCount"].(int) != 0 {
		t.Errorf("expected warmedCount=0 after Reset, got %v", stats["warmedCount"])
	}
	if stats["skippedCount"].(int) != 0 {
		t.Errorf("expected skippedCount=0 after Reset, got %v", stats["skippedCount"])
	}
}
