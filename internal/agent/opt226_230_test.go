package agent

import "testing"

// ── OPT-226: TokenAwareSlotManager ──

// TestTASM2_AcquireFreeSlot 验证 Acquire 能获取空闲槽位并标记为占用。
func TestTASM2_AcquireFreeSlot(t *testing.T) {
	m := NewTokenAwareSlotManager(3)
	id, ok := m.Acquire()
	if !ok {
		t.Errorf("Acquire 应返回 true，实际返回 false")
	}
	if id < 0 {
		t.Errorf("Acquire 应返回有效槽位 ID，实际返回 %d", id)
	}
	if !m.IsOccupied(id) {
		t.Errorf("Acquire 后槽位 %d 应被占用", id)
	}
}

// TestTASM2_AcquireWhenFull 验证所有槽位满后 Acquire 返回 false。
func TestTASM2_AcquireWhenFull(t *testing.T) {
	m := NewTokenAwareSlotManager(2)
	m.Acquire()
	m.Acquire()
	id, ok := m.Acquire()
	if ok {
		t.Errorf("所有槽位已满时 Acquire 应返回 false，实际返回 true")
	}
	if id != -1 {
		t.Errorf("槽位满时 Acquire 应返回 -1，实际返回 %d", id)
	}
}

// TestTASM2_ReleaseAndReacquire 验证 Release 释放后可重新 Acquire。
func TestTASM2_ReleaseAndReacquire(t *testing.T) {
	m := NewTokenAwareSlotManager(1)
	id, _ := m.Acquire()
	released := m.Release(id)
	if !released {
		t.Errorf("Release 占用中的槽位应返回 true，实际返回 false")
	}
	if m.IsOccupied(id) {
		t.Errorf("Release 后槽位 %d 不应被占用", id)
	}
	id2, ok := m.Acquire()
	if !ok {
		t.Errorf("Release 后应能再次 Acquire，实际返回 false")
	}
	if id2 != id {
		t.Errorf("Release 后重新 Acquire 应复用同一槽位 %d，实际返回 %d", id, id2)
	}
}

// TestTASM2_IsOccupied 验证 IsOccupied 正确反映占用状态。
func TestTASM2_IsOccupied(t *testing.T) {
	m := NewTokenAwareSlotManager(3)
	if m.IsOccupied(0) {
		t.Errorf("未分配的槽位 0 不应被占用")
	}
	id, _ := m.Acquire()
	if !m.IsOccupied(id) {
		t.Errorf("已分配的槽位 %d 应被占用", id)
	}
	if m.IsOccupied(2) {
		t.Errorf("未分配的槽位 2 不应被占用")
	}
}

// TestTASM2_GetFreeSlotCount 验证 GetFreeSlotCount 随分配/释放正确变化。
func TestTASM2_GetFreeSlotCount(t *testing.T) {
	m := NewTokenAwareSlotManager(4)
	if got := m.GetFreeSlotCount(); got != 4 {
		t.Errorf("初始空闲槽位应为 4，实际 %d", got)
	}
	m.Acquire()
	m.Acquire()
	if got := m.GetFreeSlotCount(); got != 2 {
		t.Errorf("分配 2 个后空闲槽位应为 2，实际 %d", got)
	}
}

// TestTASM2_StatsAndReset 验证 Stats 中 totalAllocations 统计及 Reset 归零。
func TestTASM2_StatsAndReset(t *testing.T) {
	m := NewTokenAwareSlotManager(3)
	m.Acquire()
	m.Acquire()
	stats := m.GetStats()
	totalAlloc, ok := stats["totalAllocations"].(int)
	if !ok {
		t.Errorf("stats 缺少 totalAllocations 字段或类型不正确")
	}
	if totalAlloc != 2 {
		t.Errorf("totalAllocations 应为 2，实际 %d", totalAlloc)
	}
	m.Reset()
	stats = m.GetStats()
	if totalAlloc, _ := stats["totalAllocations"].(int); totalAlloc != 0 {
		t.Errorf("Reset 后 totalAllocations 应为 0，实际 %d", totalAlloc)
	}
	if got := m.GetFreeSlotCount(); got != 3 {
		t.Errorf("Reset 后空闲槽位应为 3，实际 %d", got)
	}
}

// ── OPT-227: CacheInvalidationDeduplicator ──

// TestCID_FirstCheckTrue 验证首次 Check 返回 true。
func TestCID_FirstCheckTrue(t *testing.T) {
	d := NewCacheInvalidationDeduplicator()
	if !d.Check("key-a") {
		t.Errorf("首次 Check 应返回 true，实际返回 false")
	}
}

// TestCID_DuplicateFalse 验证重复 Check 同一 key 返回 false。
func TestCID_DuplicateFalse(t *testing.T) {
	d := NewCacheInvalidationDeduplicator()
	d.Check("key-a")
	if d.Check("key-a") {
		t.Errorf("重复 Check 同一 key 应返回 false，实际返回 true")
	}
}

// TestCID_ResetAllowsAgain 验证 Reset 清除后再次 Check 返回 true。
func TestCID_ResetAllowsAgain(t *testing.T) {
	d := NewCacheInvalidationDeduplicator()
	d.Check("key-a")
	d.Check("key-a")
	d.Reset()
	if !d.Check("key-a") {
		t.Errorf("Reset 后再次 Check 同一 key 应返回 true，实际返回 false")
	}
}

// TestCID_DeduplicationRate 验证 GetDeduplicationRate 计算正确。
func TestCID_DeduplicationRate(t *testing.T) {
	d := NewCacheInvalidationDeduplicator()
	// 空状态去重率应为 0
	if rate := d.GetDeduplicationRate(); rate != 0 {
		t.Errorf("空状态去重率应为 0，实际 %f", rate)
	}
	d.Check("k1") // pass
	d.Check("k2") // pass
	d.Check("k1") // dedup
	d.Check("k2") // dedup
	// total = 4, dedup = 2, rate = 0.5
	if rate := d.GetDeduplicationRate(); rate != 0.5 {
		t.Errorf("去重率应为 0.5，实际 %f", rate)
	}
}

// TestCID_Stats 验证 Stats 中 deduplicatedCount 等字段统计正确。
func TestCID_Stats(t *testing.T) {
	d := NewCacheInvalidationDeduplicator()
	d.Check("k1")
	d.Check("k1")
	d.Check("k1")
	stats := d.GetStats()
	dedup, ok := stats["deduplicatedCount"].(int)
	if !ok {
		t.Errorf("stats 缺少 deduplicatedCount 字段或类型不正确")
	}
	if dedup != 2 {
		t.Errorf("deduplicatedCount 应为 2，实际 %d", dedup)
	}
	passed, _ := stats["passedCount"].(int)
	if passed != 1 {
		t.Errorf("passedCount 应为 1，实际 %d", passed)
	}
	seen, _ := stats["seenCount"].(int)
	if seen != 1 {
		t.Errorf("seenCount 应为 1，实际 %d", seen)
	}
}

// ── OPT-228: ContextThermalCompressor ──

// TestCTC_RecordAccessAndGetHeat 验证 RecordAccess 记录热度与 GetHeat 读取。
func TestCTC_RecordAccessAndGetHeat(t *testing.T) {
	c := NewContextThermalCompressor(2)
	c.RecordAccess("frag-a")
	c.RecordAccess("frag-a")
	c.RecordAccess("frag-a")
	if heat := c.GetHeat("frag-a"); heat != 3 {
		t.Errorf("frag-a 热度应为 3，实际 %d", heat)
	}
	if heat := c.GetHeat("frag-b"); heat != 0 {
		t.Errorf("未记录的 frag-b 热度应为 0，实际 %d", heat)
	}
}

// TestCTC_CompressKeepsHighHeat 验证 Compress 保留高热度片段。
func TestCTC_CompressKeepsHighHeat(t *testing.T) {
	c := NewContextThermalCompressor(2)
	c.RecordAccess("hot")
	c.RecordAccess("hot")
	// heat=2 达到 threshold，应被保留
	kept := c.Compress([]string{"hot"})
	if len(kept) != 1 || kept[0] != "hot" {
		t.Errorf("高热度片段应被保留，实际 %v", kept)
	}
}

// TestCTC_CompressRemovesLowHeat 验证 Compress 移除低于 threshold 的片段。
func TestCTC_CompressRemovesLowHeat(t *testing.T) {
	c := NewContextThermalCompressor(2)
	c.RecordAccess("hot")
	c.RecordAccess("hot")
	// cold 从未访问，heat=0，低于 threshold 应被移除
	kept := c.Compress([]string{"hot", "cold"})
	found := false
	for _, f := range kept {
		if f == "cold" {
			found = true
		}
	}
	if found {
		t.Errorf("低热度片段 cold 应被移除，但仍保留在 %v", kept)
	}
	if len(kept) != 1 {
		t.Errorf("应只保留 1 个片段，实际 %d", len(kept))
	}
}

// TestCTC_Stats 验证 Stats 中 compressedCount 等字段统计正确。
func TestCTC_Stats(t *testing.T) {
	c := NewContextThermalCompressor(2)
	c.RecordAccess("hot")
	c.RecordAccess("hot")
	c.RecordAccess("cold")
	// hot heat=2 保留，cold heat=1 移除
	c.Compress([]string{"hot", "cold"})
	stats := c.GetStats()
	cc, ok := stats["compressedCount"].(int)
	if !ok {
		t.Errorf("stats 缺少 compressedCount 字段或类型不正确")
	}
	if cc != 1 {
		t.Errorf("compressedCount 应为 1，实际 %d", cc)
	}
	freed, _ := stats["totalFreed"].(int)
	if freed != 1 {
		t.Errorf("totalFreed 应为 1，实际 %d", freed)
	}
}

// TestCTC_Reset 验证 Reset 清空热度图与统计但保留阈值配置。
func TestCTC_Reset(t *testing.T) {
	c := NewContextThermalCompressor(2)
	c.RecordAccess("frag")
	c.RecordAccess("frag")
	c.Compress([]string{"frag", "missing"})
	c.Reset()
	if heat := c.GetHeat("frag"); heat != 0 {
		t.Errorf("Reset 后 frag 热度应为 0，实际 %d", heat)
	}
	stats := c.GetStats()
	if cc, _ := stats["compressedCount"].(int); cc != 0 {
		t.Errorf("Reset 后 compressedCount 应为 0，实际 %d", cc)
	}
	if th, _ := stats["threshold"].(int); th != 2 {
		t.Errorf("Reset 后 threshold 应保留为 2，实际 %d", th)
	}
}

// ── OPT-229: TokenAwareDegradationManager ──

// TestTADM_CheckLowUsageLevel0 验证低 usage 时 Check 返回 0 级。
func TestTADM_CheckLowUsageLevel0(t *testing.T) {
	m := NewTokenAwareDegradationManager([]int{100, 200})
	if level := m.Check(50); level != 0 {
		t.Errorf("tokenUsage=50 应返回级别 0，实际 %d", level)
	}
}

// TestTADM_CheckFirstThresholdLevel1 验证超过第一个阈值返回 1 级。
func TestTADM_CheckFirstThresholdLevel1(t *testing.T) {
	m := NewTokenAwareDegradationManager([]int{100, 200})
	if level := m.Check(100); level != 1 {
		t.Errorf("tokenUsage=100 应返回级别 1，实际 %d", level)
	}
	if level := m.Check(150); level != 1 {
		t.Errorf("tokenUsage=150 应返回级别 1，实际 %d", level)
	}
}

// TestTADM_CheckSecondThresholdLevel2 验证超过第二个阈值返回 2 级。
func TestTADM_CheckSecondThresholdLevel2(t *testing.T) {
	m := NewTokenAwareDegradationManager([]int{100, 200})
	if level := m.Check(200); level != 2 {
		t.Errorf("tokenUsage=200 应返回级别 2，实际 %d", level)
	}
	if level := m.Check(300); level != 2 {
		t.Errorf("tokenUsage=300 应返回级别 2，实际 %d", level)
	}
}

// TestTADM_DegradeRecover 验证 Degrade/Recover 手动调整级别及边界。
func TestTADM_DegradeRecover(t *testing.T) {
	m := NewTokenAwareDegradationManager([]int{100, 200})
	m.Degrade()
	if level := m.GetLevel(); level != 1 {
		t.Errorf("Degrade 后级别应为 1，实际 %d", level)
	}
	m.Degrade()
	if level := m.GetLevel(); level != 2 {
		t.Errorf("第二次 Degrade 后级别应为 2，实际 %d", level)
	}
	// 超过阈值数量上限不应再升
	m.Degrade()
	if level := m.GetLevel(); level != 2 {
		t.Errorf("超过上限后 Degrade 应保持级别 2，实际 %d", level)
	}
	m.Recover()
	if level := m.GetLevel(); level != 1 {
		t.Errorf("Recover 后级别应为 1，实际 %d", level)
	}
	m.Recover()
	m.Recover()
	if level := m.GetLevel(); level != 0 {
		t.Errorf("Recover 不应低于 0，实际 %d", level)
	}
}

// TestTADM_GetLevel 验证 GetLevel 反映 Check 后的当前降级级别。
func TestTADM_GetLevel(t *testing.T) {
	m := NewTokenAwareDegradationManager([]int{100, 200})
	if level := m.GetLevel(); level != 0 {
		t.Errorf("初始级别应为 0，实际 %d", level)
	}
	m.Check(150)
	if level := m.GetLevel(); level != 1 {
		t.Errorf("Check(150) 后级别应为 1，实际 %d", level)
	}
	m.Check(250)
	if level := m.GetLevel(); level != 2 {
		t.Errorf("Check(250) 后级别应为 2，实际 %d", level)
	}
}

// TestTADM_StatsAndReset 验证 Stats 中 degradedCount 统计及 Reset 归零。
func TestTADM_StatsAndReset(t *testing.T) {
	m := NewTokenAwareDegradationManager([]int{100, 200})
	m.Check(100) // level 0->1, degradedCount 1
	m.Check(200) // level 1->2, degradedCount 2
	stats := m.GetStats()
	deg, ok := stats["degradedCount"].(int)
	if !ok {
		t.Errorf("stats 缺少 degradedCount 字段或类型不正确")
	}
	if deg != 2 {
		t.Errorf("degradedCount 应为 2，实际 %d", deg)
	}
	m.Reset()
	stats = m.GetStats()
	if deg, _ := stats["degradedCount"].(int); deg != 0 {
		t.Errorf("Reset 后 degradedCount 应为 0，实际 %d", deg)
	}
	if level := m.GetLevel(); level != 0 {
		t.Errorf("Reset 后级别应为 0，实际 %d", level)
	}
}

// ── OPT-230: PromptCacheLifecycleManager ──

// TestPCLM_CreateAndAccess 验证 Create 后 Access 更新访问时间且返回有效。
func TestPCLM_CreateAndAccess(t *testing.T) {
	m := NewPromptCacheLifecycleManager()
	m.Create("item-1", 0, 100)
	valid := m.Access("item-1", 50)
	if !valid {
		t.Errorf("在 TTL 内 Access 应返回 true，实际返回 false")
	}
	valid = m.Access("item-1", 80)
	if !valid {
		t.Errorf("仍在 TTL 内 Access 应返回 true，实际返回 false")
	}
	// 访问不存在的 key 应返回 false
	if m.Access("missing", 10) {
		t.Errorf("访问不存在的 key 应返回 false，实际返回 true")
	}
}

// TestPCLM_AccessExpiredReturnsFalse 验证 Access 过期项返回 false。
func TestPCLM_AccessExpiredReturnsFalse(t *testing.T) {
	m := NewPromptCacheLifecycleManager()
	m.Create("item-1", 0, 100)
	// 101 - 0 = 101 > 100，已过期
	valid := m.Access("item-1", 101)
	if valid {
		t.Errorf("过期项 Access 应返回 false，实际返回 true")
	}
}

// TestPCLM_Refresh 验证 Refresh 刷新 TTL 后原过期时刻仍有效。
func TestPCLM_Refresh(t *testing.T) {
	m := NewPromptCacheLifecycleManager()
	m.Create("item-1", 0, 100)
	ok := m.Refresh("item-1", 500)
	if !ok {
		t.Errorf("Refresh 已存在的 key 应返回 true，实际返回 false")
	}
	// 刷新后 101 时刻不再过期 (101-0=101 <= 500)
	valid := m.Access("item-1", 101)
	if !valid {
		t.Errorf("Refresh 后 101 时刻应有效，实际返回 false")
	}
	// 刷新不存在的 key 应返回 false
	if m.Refresh("missing", 1000) {
		t.Errorf("Refresh 不存在的 key 应返回 false，实际返回 true")
	}
}

// TestPCLM_Expire 验证 Expire 过期移除正确的项。
func TestPCLM_Expire(t *testing.T) {
	m := NewPromptCacheLifecycleManager()
	m.Create("item-1", 0, 100)
	m.Create("item-2", 0, 50)
	// currentTime=60: item-2 过期 (60>50)，item-1 仍有效 (60<=100)
	expired := m.Expire(60)
	if len(expired) != 1 {
		t.Errorf("应过期 1 个项，实际过期 %d 个: %v", len(expired), expired)
	}
	if len(expired) > 0 && expired[0] != "item-2" {
		t.Errorf("应过期 item-2，实际过期 %s", expired[0])
	}
	// 过期后再次 Access 应返回 false
	if m.Access("item-2", 60) {
		t.Errorf("过期移除后 Access item-2 应返回 false，实际返回 true")
	}
}

// TestPCLM_Stats 验证 Stats 中 createdCount 与 expiredCount 统计正确。
func TestPCLM_Stats(t *testing.T) {
	m := NewPromptCacheLifecycleManager()
	m.Create("item-1", 0, 100)
	m.Create("item-2", 0, 50)
	m.Expire(60) // item-2 过期
	stats := m.GetStats()
	created, ok := stats["createdCount"].(int)
	if !ok {
		t.Errorf("stats 缺少 createdCount 字段或类型不正确")
	}
	if created != 2 {
		t.Errorf("createdCount 应为 2，实际 %d", created)
	}
	exp, _ := stats["expiredCount"].(int)
	if exp != 1 {
		t.Errorf("expiredCount 应为 1，实际 %d", exp)
	}
	entry, _ := stats["entryCount"].(int)
	if entry != 1 {
		t.Errorf("entryCount 应为 1，实际 %d", entry)
	}
}

// TestPCLM_Reset 验证 Reset 清空所有缓存项与统计。
func TestPCLM_Reset(t *testing.T) {
	m := NewPromptCacheLifecycleManager()
	m.Create("item-1", 0, 100)
	m.Create("item-2", 0, 50)
	m.Expire(60)
	m.Reset()
	stats := m.GetStats()
	if created, _ := stats["createdCount"].(int); created != 0 {
		t.Errorf("Reset 后 createdCount 应为 0，实际 %d", created)
	}
	if exp, _ := stats["expiredCount"].(int); exp != 0 {
		t.Errorf("Reset 后 expiredCount 应为 0，实际 %d", exp)
	}
	if entry, _ := stats["entryCount"].(int); entry != 0 {
		t.Errorf("Reset 后 entryCount 应为 0，实际 %d", entry)
	}
}
