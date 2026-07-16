package agent

import "testing"

// ═══════════════════════════════════════════════════════════════════════════
// OPT-256: TokenAwareGracefulDegrader 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestTAGD_NewDefaults 验证构造后默认状态。
func TestTAGD_NewDefaults(t *testing.T) {
	d := NewTokenAwareGracefulDegrader(3)
	if d.GetLevel() != 0 {
		t.Errorf("GetLevel() = %d, 期望 0", d.GetLevel())
	}
	stats := d.GetStats()
	if stats["maxLevel"].(int) != 3 {
		t.Errorf("maxLevel = %v, 期望 3", stats["maxLevel"])
	}
	if stats["degradedSteps"].(int) != 0 {
		t.Errorf("degradedSteps = %v, 期望 0", stats["degradedSteps"])
	}
	if stats["disabledFeatureCount"].(int) != 0 {
		t.Errorf("disabledFeatureCount = %v, 期望 0", stats["disabledFeatureCount"])
	}
}

// TestTAGD_DegradeAndRecover 验证降级/恢复级别的上限与下限钳制。
func TestTAGD_DegradeAndRecover(t *testing.T) {
	d := NewTokenAwareGracefulDegrader(2)
	if lvl := d.Degrade(); lvl != 1 {
		t.Errorf("首次 Degrade 返回 %d, 期望 1", lvl)
	}
	if lvl := d.Degrade(); lvl != 2 {
		t.Errorf("第二次 Degrade 返回 %d, 期望 2", lvl)
	}
	if lvl := d.Degrade(); lvl != 2 {
		t.Errorf("达到上限后 Degrade 返回 %d, 期望 2", lvl)
	}
	if lvl := d.Recover(); lvl != 1 {
		t.Errorf("首次 Recover 返回 %d, 期望 1", lvl)
	}
	if lvl := d.Recover(); lvl != 0 {
		t.Errorf("第二次 Recover 返回 %d, 期望 0", lvl)
	}
	if lvl := d.Recover(); lvl != 0 {
		t.Errorf("达到下限后 Recover 返回 %d, 期望 0", lvl)
	}
}

// TestTAGD_FeatureToggle 验证功能启用/禁用切换，未注册功能默认启用。
func TestTAGD_FeatureToggle(t *testing.T) {
	d := NewTokenAwareGracefulDegrader(1)
	if !d.IsFeatureEnabled("search") {
		t.Errorf("未注册功能应默认启用")
	}
	d.DisableFeature("search")
	if d.IsFeatureEnabled("search") {
		t.Errorf("禁用后 IsFeatureEnabled 应为 false")
	}
	d.EnableFeature("search")
	if !d.IsFeatureEnabled("search") {
		t.Errorf("重新启用后 IsFeatureEnabled 应为 true")
	}
}

// TestTAGD_DisabledFeatureCount 验证被禁用功能数量的统计。
func TestTAGD_DisabledFeatureCount(t *testing.T) {
	d := NewTokenAwareGracefulDegrader(1)
	d.DisableFeature("a")
	d.DisableFeature("b")
	d.EnableFeature("c")
	stats := d.GetStats()
	if stats["disabledFeatureCount"].(int) != 2 {
		t.Errorf("disabledFeatureCount = %v, 期望 2", stats["disabledFeatureCount"])
	}
}

// TestTAGD_StepsAccounting 验证降级/恢复步数累计。
func TestTAGD_StepsAccounting(t *testing.T) {
	d := NewTokenAwareGracefulDegrader(5)
	d.Degrade()
	d.Degrade()
	d.Recover()
	stats := d.GetStats()
	if stats["degradedSteps"].(int) != 2 {
		t.Errorf("degradedSteps = %v, 期望 2", stats["degradedSteps"])
	}
	if stats["recoveredSteps"].(int) != 1 {
		t.Errorf("recoveredSteps = %v, 期望 1", stats["recoveredSteps"])
	}
	if stats["level"].(int) != 1 {
		t.Errorf("level = %v, 期望 1", stats["level"])
	}
}

// TestTAGD_Reset 验证 Reset 清空状态但保留 maxLevel。
func TestTAGD_Reset(t *testing.T) {
	d := NewTokenAwareGracefulDegrader(3)
	d.Degrade()
	d.DisableFeature("x")
	d.Reset()
	if d.GetLevel() != 0 {
		t.Errorf("Reset 后 GetLevel() = %d, 期望 0", d.GetLevel())
	}
	stats := d.GetStats()
	if stats["degradedSteps"].(int) != 0 {
		t.Errorf("Reset 后 degradedSteps = %v, 期望 0", stats["degradedSteps"])
	}
	if stats["disabledFeatureCount"].(int) != 0 {
		t.Errorf("Reset 后 disabledFeatureCount = %v, 期望 0", stats["disabledFeatureCount"])
	}
	if stats["maxLevel"].(int) != 3 {
		t.Errorf("Reset 后 maxLevel = %v, 期望 3（应保留配置）", stats["maxLevel"])
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-257: CacheInvalidationScheduler 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestCIS_NewDefaults 验证构造后默认状态。
func TestCIS_NewDefaults(t *testing.T) {
	s := NewCacheInvalidationScheduler()
	if s.GetPendingCount() != 0 {
		t.Errorf("GetPendingCount() = %d, 期望 0", s.GetPendingCount())
	}
	stats := s.GetStats()
	if stats["executedCount"].(int) != 0 {
		t.Errorf("executedCount = %v, 期望 0", stats["executedCount"])
	}
	if stats["cancelledCount"].(int) != 0 {
		t.Errorf("cancelledCount = %v, 期望 0", stats["cancelledCount"])
	}
}

// TestCIS_ScheduleAndPending 验证调度后待执行计数。
func TestCIS_ScheduleAndPending(t *testing.T) {
	s := NewCacheInvalidationScheduler()
	s.Schedule("k1", 100)
	s.Schedule("k2", 200)
	if s.GetPendingCount() != 2 {
		t.Errorf("GetPendingCount() = %d, 期望 2", s.GetPendingCount())
	}
}

// TestCIS_ExecuteExpired 验证只执行到期任务并累计 executedCount。
func TestCIS_ExecuteExpired(t *testing.T) {
	s := NewCacheInvalidationScheduler()
	s.Schedule("k1", 100)
	s.Schedule("k2", 200)
	s.Schedule("k3", 300)

	executed := s.Execute(150)
	if len(executed) != 1 || executed[0] != "k1" {
		t.Errorf("Execute(150) = %v, 期望 [k1]", executed)
	}
	executed2 := s.Execute(300)
	if len(executed2) != 2 {
		t.Errorf("Execute(300) 返回 %d 个 key, 期望 2", len(executed2))
	}
	if s.GetPendingCount() != 0 {
		t.Errorf("全部执行后 GetPendingCount() = %d, 期望 0", s.GetPendingCount())
	}
	stats := s.GetStats()
	if stats["executedCount"].(int) != 3 {
		t.Errorf("executedCount = %v, 期望 3", stats["executedCount"])
	}
}

// TestCIS_Cancel 验证取消待执行任务及重复取消返回 false。
func TestCIS_Cancel(t *testing.T) {
	s := NewCacheInvalidationScheduler()
	s.Schedule("k1", 100)
	if !s.Cancel("k1") {
		t.Errorf("Cancel(\"k1\") 返回 false, 期望 true")
	}
	if s.Cancel("k1") {
		t.Errorf("对已取消 key 再次 Cancel 返回 true, 期望 false")
	}
	if s.GetPendingCount() != 0 {
		t.Errorf("Cancel 后 GetPendingCount() = %d, 期望 0", s.GetPendingCount())
	}
	stats := s.GetStats()
	if stats["cancelledCount"].(int) != 1 {
		t.Errorf("cancelledCount = %v, 期望 1", stats["cancelledCount"])
	}
}

// TestCIS_RescheduleUpdatesTime 验证重新调度更新时间且不重复计数。
func TestCIS_RescheduleUpdatesTime(t *testing.T) {
	s := NewCacheInvalidationScheduler()
	s.Schedule("k1", 100)
	s.Schedule("k1", 300)
	if s.GetPendingCount() != 1 {
		t.Errorf("重新调度后 GetPendingCount() = %d, 期望 1", s.GetPendingCount())
	}
	executed := s.Execute(200)
	if len(executed) != 0 {
		t.Errorf("更新到 300 后 Execute(200) 返回 %v, 期望空", executed)
	}
}

// TestCIS_Reset 验证 Reset 清空任务与计数。
func TestCIS_Reset(t *testing.T) {
	s := NewCacheInvalidationScheduler()
	s.Schedule("k1", 100)
	s.Execute(100)
	s.Schedule("k2", 200)
	s.Cancel("k2")
	s.Reset()
	if s.GetPendingCount() != 0 {
		t.Errorf("Reset 后 GetPendingCount() = %d, 期望 0", s.GetPendingCount())
	}
	stats := s.GetStats()
	if stats["executedCount"].(int) != 0 {
		t.Errorf("Reset 后 executedCount = %v, 期望 0", stats["executedCount"])
	}
	if stats["cancelledCount"].(int) != 0 {
		t.Errorf("Reset 后 cancelledCount = %v, 期望 0", stats["cancelledCount"])
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-258: ContextWindowProactiveAdjuster 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestCWPA_NewDefaults 验证构造后默认状态。
func TestCWPA_NewDefaults(t *testing.T) {
	a := NewContextWindowProactiveAdjuster(1000, 2000, 3)
	if a.GetCurrentSize() != 1000 {
		t.Errorf("GetCurrentSize() = %d, 期望 1000", a.GetCurrentSize())
	}
	if a.GetTrend() != 0 {
		t.Errorf("GetTrend() = %d, 期望 0", a.GetTrend())
	}
	stats := a.GetStats()
	if stats["targetSize"].(int) != 2000 {
		t.Errorf("targetSize = %v, 期望 2000", stats["targetSize"])
	}
	if stats["predictionWindow"].(int) != 3 {
		t.Errorf("predictionWindow = %v, 期望 3", stats["predictionWindow"])
	}
}

// TestCWPA_GrowingTrend 验证增长趋势下建议大小高于目标。
func TestCWPA_GrowingTrend(t *testing.T) {
	a := NewContextWindowProactiveAdjuster(1000, 2000, 3)
	a.Observe(1000) // 基线，趋势仍为稳定
	suggested := a.Observe(1500)
	if a.GetTrend() != 1 {
		t.Errorf("GetTrend() = %d, 期望 1（增长）", a.GetTrend())
	}
	if suggested <= 2000 {
		t.Errorf("增长趋势建议大小 = %d, 应大于目标 2000", suggested)
	}
}

// TestCWPA_ShrinkingTrend 验证收缩趋势下建议大小低于目标。
func TestCWPA_ShrinkingTrend(t *testing.T) {
	a := NewContextWindowProactiveAdjuster(2000, 2000, 3)
	a.Observe(2000) // 基线
	suggested := a.Observe(1500)
	if a.GetTrend() != -1 {
		t.Errorf("GetTrend() = %d, 期望 -1（收缩）", a.GetTrend())
	}
	if suggested >= 2000 {
		t.Errorf("收缩趋势建议大小 = %d, 应小于目标 2000", suggested)
	}
}

// TestCWPA_StableTrend 验证稳定趋势下建议大小贴合目标。
func TestCWPA_StableTrend(t *testing.T) {
	a := NewContextWindowProactiveAdjuster(1000, 2000, 3)
	a.Observe(1500)
	suggested := a.Observe(1500)
	if a.GetTrend() != 0 {
		t.Errorf("GetTrend() = %d, 期望 0（稳定）", a.GetTrend())
	}
	if suggested != 2000 {
		t.Errorf("稳定趋势建议大小 = %d, 期望 2000", suggested)
	}
}

// TestCWPA_AdjustmentsCounter 验证调整次数计数。
func TestCWPA_AdjustmentsCounter(t *testing.T) {
	a := NewContextWindowProactiveAdjuster(1000, 2000, 3)
	a.Observe(1500)
	stats := a.GetStats()
	if stats["adjustments"].(int) < 1 {
		t.Errorf("adjustments = %v, 期望至少 1", stats["adjustments"])
	}
}

// TestCWPA_Reset 验证 Reset 恢复初始状态并保留配置。
func TestCWPA_Reset(t *testing.T) {
	a := NewContextWindowProactiveAdjuster(1000, 2000, 3)
	a.Observe(1500)
	a.Observe(1800)
	a.Reset()
	if a.GetTrend() != 0 {
		t.Errorf("Reset 后 GetTrend() = %d, 期望 0", a.GetTrend())
	}
	if a.GetCurrentSize() != 1000 {
		t.Errorf("Reset 后 GetCurrentSize() = %d, 期望 1000（恢复初始）", a.GetCurrentSize())
	}
	stats := a.GetStats()
	if stats["adjustments"].(int) != 0 {
		t.Errorf("Reset 后 adjustments = %v, 期望 0", stats["adjustments"])
	}
	if stats["targetSize"].(int) != 2000 {
		t.Errorf("Reset 后 targetSize = %v, 期望 2000（应保留配置）", stats["targetSize"])
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-259: TokenAwareResourceArbiter 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestTARA_NewDefaults 验证构造后默认状态。
func TestTARA_NewDefaults(t *testing.T) {
	a := NewTokenAwareResourceArbiter(1000)
	if a.GetAvailableResource() != 1000 {
		t.Errorf("GetAvailableResource() = %d, 期望 1000", a.GetAvailableResource())
	}
	stats := a.GetStats()
	if stats["totalResource"].(int) != 1000 {
		t.Errorf("totalResource = %v, 期望 1000", stats["totalResource"])
	}
	if stats["requesterCount"].(int) != 0 {
		t.Errorf("requesterCount = %v, 期望 0", stats["requesterCount"])
	}
}

// TestTARA_RequestAndAllocation 验证请求批准与分配记账。
func TestTARA_RequestAndAllocation(t *testing.T) {
	a := NewTokenAwareResourceArbiter(1000)
	if !a.Request("r1", 300) {
		t.Errorf("Request(\"r1\", 300) 返回 false, 期望 true")
	}
	if a.GetAllocation("r1") != 300 {
		t.Errorf("GetAllocation(\"r1\") = %d, 期望 300", a.GetAllocation("r1"))
	}
	if a.GetAvailableResource() != 700 {
		t.Errorf("GetAvailableResource() = %d, 期望 700", a.GetAvailableResource())
	}
}

// TestTARA_RequestDeniedWhenInsufficient 验证资源不足时拒绝并累计 deniedCount。
func TestTARA_RequestDeniedWhenInsufficient(t *testing.T) {
	a := NewTokenAwareResourceArbiter(1000)
	if !a.Request("r1", 800) {
		t.Errorf("首次 Request 应批准")
	}
	if a.Request("r2", 300) {
		t.Errorf("资源不足时 Request 应拒绝")
	}
	stats := a.GetStats()
	if stats["deniedCount"].(int) != 1 {
		t.Errorf("deniedCount = %v, 期望 1", stats["deniedCount"])
	}
	if stats["arbitratedCount"].(int) != 2 {
		t.Errorf("arbitratedCount = %v, 期望 2", stats["arbitratedCount"])
	}
}

// TestTARA_Release 验证部分释放与过量释放钳制。
func TestTARA_Release(t *testing.T) {
	a := NewTokenAwareResourceArbiter(1000)
	a.Request("r1", 500)
	a.Release("r1", 200)
	if a.GetAllocation("r1") != 300 {
		t.Errorf("部分释放后 GetAllocation = %d, 期望 300", a.GetAllocation("r1"))
	}
	a.Release("r1", 1000) // 过量释放，钳制为 300
	if a.GetAllocation("r1") != 0 {
		t.Errorf("过量释放后 GetAllocation = %d, 期望 0", a.GetAllocation("r1"))
	}
	if a.GetAvailableResource() != 1000 {
		t.Errorf("全部释放后 GetAvailableResource = %d, 期望 1000", a.GetAvailableResource())
	}
}

// TestTARA_RequesterCount 验证活跃请求者数量随释放变化。
func TestTARA_RequesterCount(t *testing.T) {
	a := NewTokenAwareResourceArbiter(1000)
	a.Request("r1", 100)
	a.Request("r2", 200)
	stats := a.GetStats()
	if stats["requesterCount"].(int) != 2 {
		t.Errorf("requesterCount = %v, 期望 2", stats["requesterCount"])
	}
	a.Release("r1", 100)
	stats = a.GetStats()
	if stats["requesterCount"].(int) != 1 {
		t.Errorf("释放后 requesterCount = %v, 期望 1", stats["requesterCount"])
	}
}

// TestTARA_Reset 验证 Reset 清空分配与计数并保留 totalResource。
func TestTARA_Reset(t *testing.T) {
	a := NewTokenAwareResourceArbiter(1000)
	a.Request("r1", 500)
	a.Request("r2", 200)
	a.Reset()
	if a.GetAvailableResource() != 1000 {
		t.Errorf("Reset 后 GetAvailableResource = %d, 期望 1000", a.GetAvailableResource())
	}
	stats := a.GetStats()
	if stats["arbitratedCount"].(int) != 0 {
		t.Errorf("Reset 后 arbitratedCount = %v, 期望 0", stats["arbitratedCount"])
	}
	if stats["requesterCount"].(int) != 0 {
		t.Errorf("Reset 后 requesterCount = %v, 期望 0", stats["requesterCount"])
	}
	if stats["totalResource"].(int) != 1000 {
		t.Errorf("Reset 后 totalResource = %v, 期望 1000（应保留配置）", stats["totalResource"])
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-260: PromptCacheWarmupScheduler 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestPCWS_NewDefaults 验证构造后默认状态。
func TestPCWS_NewDefaults(t *testing.T) {
	s := NewPromptCacheWarmupScheduler(5)
	if s.GetQueueSize() != 0 {
		t.Errorf("GetQueueSize() = %d, 期望 0", s.GetQueueSize())
	}
	stats := s.GetStats()
	if stats["maxQueueSize"].(int) != 5 {
		t.Errorf("maxQueueSize = %v, 期望 5", stats["maxQueueSize"])
	}
	if stats["warmedCount"].(int) != 0 {
		t.Errorf("warmedCount = %v, 期望 0", stats["warmedCount"])
	}
}

// TestPCWS_ScheduleAndQueue 验证调度入队与队列大小。
func TestPCWS_ScheduleAndQueue(t *testing.T) {
	s := NewPromptCacheWarmupScheduler(5)
	if !s.Schedule("k1") {
		t.Errorf("Schedule(\"k1\") 返回 false, 期望 true")
	}
	if !s.Schedule("k2") {
		t.Errorf("Schedule(\"k2\") 返回 false, 期望 true")
	}
	if s.GetQueueSize() != 2 {
		t.Errorf("GetQueueSize() = %d, 期望 2", s.GetQueueSize())
	}
}

// TestPCWS_ScheduleRejectsDuplicate 验证重复入队被拒绝。
func TestPCWS_ScheduleRejectsDuplicate(t *testing.T) {
	s := NewPromptCacheWarmupScheduler(5)
	s.Schedule("k1")
	if s.Schedule("k1") {
		t.Errorf("对已在队列中的 key 重复 Schedule 应返回 false")
	}
}

// TestPCWS_ScheduleRejectsWhenFull 验证队列满时拒绝入队。
func TestPCWS_ScheduleRejectsWhenFull(t *testing.T) {
	s := NewPromptCacheWarmupScheduler(2)
	s.Schedule("k1")
	s.Schedule("k2")
	if s.Schedule("k3") {
		t.Errorf("队列满后 Schedule 应返回 false")
	}
}

// TestPCWS_WarmUpNext 验证按序预热并标记已预热状态。
func TestPCWS_WarmUpNext(t *testing.T) {
	s := NewPromptCacheWarmupScheduler(5)
	s.Schedule("k1")
	s.Schedule("k2")
	key, ok := s.WarmUpNext()
	if !ok || key != "k1" {
		t.Errorf("WarmUpNext 返回 (%q, %v), 期望 (\"k1\", true)", key, ok)
	}
	if !s.IsWarmedUp("k1") {
		t.Errorf("预热后 IsWarmedUp(\"k1\") 应为 true")
	}
	if s.GetQueueSize() != 1 {
		t.Errorf("预热一个后 GetQueueSize() = %d, 期望 1", s.GetQueueSize())
	}
	stats := s.GetStats()
	if stats["totalWarmed"].(int) != 1 {
		t.Errorf("totalWarmed = %v, 期望 1", stats["totalWarmed"])
	}
	if stats["warmedCount"].(int) != 1 {
		t.Errorf("warmedCount = %v, 期望 1", stats["warmedCount"])
	}
}

// TestPCWS_Reset 验证 Reset 清空队列与计数并保留 maxQueueSize。
func TestPCWS_Reset(t *testing.T) {
	s := NewPromptCacheWarmupScheduler(5)
	s.Schedule("k1")
	s.WarmUpNext()
	s.Reset()
	if s.GetQueueSize() != 0 {
		t.Errorf("Reset 后 GetQueueSize() = %d, 期望 0", s.GetQueueSize())
	}
	stats := s.GetStats()
	if stats["totalScheduled"].(int) != 0 {
		t.Errorf("Reset 后 totalScheduled = %v, 期望 0", stats["totalScheduled"])
	}
	if stats["totalWarmed"].(int) != 0 {
		t.Errorf("Reset 后 totalWarmed = %v, 期望 0", stats["totalWarmed"])
	}
	if stats["maxQueueSize"].(int) != 5 {
		t.Errorf("Reset 后 maxQueueSize = %v, 期望 5（应保留配置）", stats["maxQueueSize"])
	}
}
