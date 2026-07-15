package agent

import "testing"

// ════════════════════════════════════════════════════════════
// OPT-231: TokenAwareOverflowHandler 测试
// ════════════════════════════════════════════════════════════

// TestTAOH_Handle_ReturnsConfiguredStrategy 验证 Handle 在溢出量不超过 maxOverflow 时返回配置的策略名。
func TestTAOH_Handle_ReturnsConfiguredStrategy(t *testing.T) {
	h := NewTokenAwareOverflowHandler(100, "defer")
	strategy := h.Handle(50)
	if strategy != "defer" {
		t.Errorf("Handle(50) = %q, want %q", strategy, "defer")
	}

	h2 := NewTokenAwareOverflowHandler(100, "compress")
	s2 := h2.Handle(80)
	if s2 != "compress" {
		t.Errorf("Handle(80) = %q, want %q", s2, "compress")
	}
}

// TestTAOH_CanHandle_WithinMaxOverflow 验证 CanHandle 在溢出量不超过 maxOverflow 时返回 true。
func TestTAOH_CanHandle_WithinMaxOverflow(t *testing.T) {
	h := NewTokenAwareOverflowHandler(100, "drop")
	if !h.CanHandle(50) {
		t.Errorf("CanHandle(50) = false, want true")
	}
	if !h.CanHandle(100) {
		t.Errorf("CanHandle(100) = false, want true (boundary)")
	}
}

// TestTAOH_CanHandle_ExceedsMaxOverflow 验证 CanHandle 在溢出量超出 maxOverflow 时返回 false。
func TestTAOH_CanHandle_ExceedsMaxOverflow(t *testing.T) {
	h := NewTokenAwareOverflowHandler(100, "drop")
	if h.CanHandle(150) {
		t.Errorf("CanHandle(150) = true, want false")
	}
}

// TestTAOH_Handle_ForcesDropWhenExceedingMax 验证 Handle 在溢出量超出 maxOverflow 时强制返回 "drop"。
func TestTAOH_Handle_ForcesDropWhenExceedingMax(t *testing.T) {
	h := NewTokenAwareOverflowHandler(100, "compress")
	strategy := h.Handle(150)
	if strategy != "drop" {
		t.Errorf("Handle(150) with maxOverflow=100, strategy=compress = %q, want %q", strategy, "drop")
	}
}

// TestTAOH_GetOverflowRatio 验证 GetOverflowRatio 返回正确的平均溢出比率。
func TestTAOH_GetOverflowRatio(t *testing.T) {
	h := NewTokenAwareOverflowHandler(100, "defer")
	// 无溢出记录时比率应为 0
	if r := h.GetOverflowRatio(); r != 0 {
		t.Errorf("GetOverflowRatio() = %v, want 0 (no overflow yet)", r)
	}

	h.Handle(50)
	h.Handle(50)
	// 平均溢出 = (50+50)/2 = 50，比率 = 50/100 = 0.5
	if r := h.GetOverflowRatio(); r != 0.5 {
		t.Errorf("GetOverflowRatio() = %v, want 0.5", r)
	}
}

// TestTAOH_Stats_AndReset 验证 Stats 中的 overflowCount 以及 Reset 的行为。
func TestTAOH_Stats_AndReset(t *testing.T) {
	h := NewTokenAwareOverflowHandler(100, "defer")
	h.Handle(30)
	h.Handle(40)

	stats := h.GetStats()
	if oc, ok := stats["overflowCount"].(int); !ok || oc != 2 {
		t.Errorf("stats[overflowCount] = %v, want 2", stats["overflowCount"])
	}
	if tot, ok := stats["totalOverflowTokens"].(int); !ok || tot != 70 {
		t.Errorf("stats[totalOverflowTokens] = %v, want 70", stats["totalOverflowTokens"])
	}

	h.Reset()
	stats2 := h.GetStats()
	if oc, ok := stats2["overflowCount"].(int); !ok || oc != 0 {
		t.Errorf("after Reset, stats[overflowCount] = %v, want 0", stats2["overflowCount"])
	}
	// Reset 不应重置 maxOverflow 和 strategy 配置
	if mx, ok := stats2["maxOverflow"].(int); !ok || mx != 100 {
		t.Errorf("after Reset, stats[maxOverflow] = %v, want 100", stats2["maxOverflow"])
	}
}

// ════════════════════════════════════════════════════════════
// OPT-232: CacheInvalidationTrackerV2 测试
// ════════════════════════════════════════════════════════════

// TestCITV2_Track_AndGetPatternCount 验证 Track 后 GetPatternCount 返回正确的计数。
func TestCITV2_Track_AndGetPatternCount(t *testing.T) {
	tracker := NewCacheInvalidationTrackerV2()
	tracker.Track("key1", "ttl_expire")

	if c := tracker.GetPatternCount("ttl_expire"); c != 1 {
		t.Errorf("GetPatternCount(ttl_expire) = %d, want 1", c)
	}
	if c := tracker.GetPatternCount("manual"); c != 0 {
		t.Errorf("GetPatternCount(manual) = %d, want 0", c)
	}
}

// TestCITV2_Track_MultipleIncrement 验证多次 Track 同一 pattern 计数递增。
func TestCITV2_Track_MultipleIncrement(t *testing.T) {
	tracker := NewCacheInvalidationTrackerV2()
	tracker.Track("k1", "ttl_expire")
	tracker.Track("k2", "ttl_expire")
	tracker.Track("k3", "ttl_expire")

	if c := tracker.GetPatternCount("ttl_expire"); c != 3 {
		t.Errorf("GetPatternCount(ttl_expire) = %d, want 3", c)
	}
}

// TestCITV2_GetTopPatterns_SortedByCount 验证 GetTopPatterns 按计数降序返回模式。
func TestCITV2_GetTopPatterns_SortedByCount(t *testing.T) {
	tracker := NewCacheInvalidationTrackerV2()
	// pattern "a" 出现 3 次
	tracker.Track("k1", "a")
	tracker.Track("k2", "a")
	tracker.Track("k3", "a")
	// pattern "b" 出现 1 次
	tracker.Track("k4", "b")
	// pattern "c" 出现 2 次
	tracker.Track("k5", "c")
	tracker.Track("k6", "c")

	top := tracker.GetTopPatterns(2)
	if len(top) != 2 {
		t.Fatalf("GetTopPatterns(2) returned %d items, want 2", len(top))
	}
	if top[0] != "a" {
		t.Errorf("GetTopPatterns(2)[0] = %q, want %q", top[0], "a")
	}
	if top[1] != "c" {
		t.Errorf("GetTopPatterns(2)[1] = %q, want %q", top[1], "c")
	}
}

// TestCITV2_GetTopPatterns_TieBreaker 验证同频时按字母序排列。
func TestCITV2_GetTopPatterns_TieBreaker(t *testing.T) {
	tracker := NewCacheInvalidationTrackerV2()
	tracker.Track("k1", "zebra")
	tracker.Track("k2", "alpha")
	// 两个 pattern 各出现 1 次，字母序 alpha < zebra

	top := tracker.GetTopPatterns(2)
	if len(top) != 2 {
		t.Fatalf("GetTopPatterns(2) returned %d items, want 2", len(top))
	}
	if top[0] != "alpha" {
		t.Errorf("GetTopPatterns(2)[0] = %q, want %q (alphabetical tie-break)", top[0], "alpha")
	}
	if top[1] != "zebra" {
		t.Errorf("GetTopPatterns(2)[1] = %q, want %q", top[1], "zebra")
	}
}

// TestCITV2_Stats_TotalTracked 验证 Stats 中的 totalTracked。
func TestCITV2_Stats_TotalTracked(t *testing.T) {
	tracker := NewCacheInvalidationTrackerV2()
	tracker.Track("k1", "ttl_expire")
	tracker.Track("k2", "manual")

	stats := tracker.GetStats()
	if tt, ok := stats["totalTracked"].(int); !ok || tt != 2 {
		t.Errorf("stats[totalTracked] = %v, want 2", stats["totalTracked"])
	}
	if pc, ok := stats["patternCount"].(int); !ok || pc != 2 {
		t.Errorf("stats[patternCount] = %v, want 2", stats["patternCount"])
	}
	if lk, ok := stats["lastInvalidatedKey"].(string); !ok || lk != "k2" {
		t.Errorf("stats[lastInvalidatedKey] = %v, want %q", stats["lastInvalidatedKey"], "k2")
	}
}

// TestCITV2_Reset 验证 Reset 清空所有追踪状态。
func TestCITV2_Reset(t *testing.T) {
	tracker := NewCacheInvalidationTrackerV2()
	tracker.Track("k1", "ttl_expire")
	tracker.Track("k2", "manual")

	tracker.Reset()

	if c := tracker.GetPatternCount("ttl_expire"); c != 0 {
		t.Errorf("after Reset, GetPatternCount(ttl_expire) = %d, want 0", c)
	}
	stats := tracker.GetStats()
	if tt, ok := stats["totalTracked"].(int); !ok || tt != 0 {
		t.Errorf("after Reset, stats[totalTracked] = %v, want 0", stats["totalTracked"])
	}
	if pc, ok := stats["patternCount"].(int); !ok || pc != 0 {
		t.Errorf("after Reset, stats[patternCount] = %v, want 0", stats["patternCount"])
	}
}

// ════════════════════════════════════════════════════════════
// OPT-233: ContextWindowCalibratorV3 测试
// ════════════════════════════════════════════════════════════

// TestCWCV3_Calibrate_AdjustsTarget 验证 Calibrate 根据观察值调整目标大小。
func TestCWCV3_Calibrate_AdjustsTarget(t *testing.T) {
	c := NewContextWindowCalibratorV3(1000, 100)
	// observed=1050, delta=50, clamped=50, new target=1050
	result := c.Calibrate(1050)
	if result != 1050 {
		t.Errorf("Calibrate(1050) = %d, want 1050", result)
	}
}

// TestCWCV3_Calibrate_ClampedByMaxAdjustment 验证 Calibrate 受 maxAdjustment 限制。
func TestCWCV3_Calibrate_ClampedByMaxAdjustment(t *testing.T) {
	c := NewContextWindowCalibratorV3(1000, 100)
	// observed=5000, delta=4000, clamped=100, new target=1100
	result := c.Calibrate(5000)
	if result != 1100 {
		t.Errorf("Calibrate(5000) with maxAdjustment=100 = %d, want 1100", result)
	}
}

// TestCWCV3_GetCalibrationDelta 验证 GetCalibrationDelta 返回实际值与目标值的差距。
func TestCWCV3_GetCalibrationDelta(t *testing.T) {
	c := NewContextWindowCalibratorV3(1000, 100)
	// observed=1200, delta=200, clamped=100, new target=1100, actual=1200
	c.Calibrate(1200)
	// delta = actual - target = 1200 - 1100 = 100
	if d := c.GetCalibrationDelta(); d != 100 {
		t.Errorf("GetCalibrationDelta() = %d, want 100", d)
	}
}

// TestCWCV3_GetAdjustmentHistory 验证 GetAdjustmentHistory 累计调整量。
func TestCWCV3_GetAdjustmentHistory(t *testing.T) {
	c := NewContextWindowCalibratorV3(1000, 100)
	// Calibrate(1200): delta=200, clamped=100, totalAdjustment=100
	c.Calibrate(1200)
	// Calibrate(1050): delta=1050-1100=-50, clamped=-50, totalAdjustment=100+50=150
	c.Calibrate(1050)
	if h := c.GetAdjustmentHistory(); h != 150 {
		t.Errorf("GetAdjustmentHistory() = %d, want 150", h)
	}
}

// TestCWCV3_Stats_CalibrationCount 验证 Stats 中的 calibrationCount。
func TestCWCV3_Stats_CalibrationCount(t *testing.T) {
	c := NewContextWindowCalibratorV3(1000, 100)
	c.Calibrate(1100) // target 1000→1100, totalAdjustment=100
	c.Calibrate(1000) // target 1100→1000, totalAdjustment=200

	stats := c.GetStats()
	if cc, ok := stats["calibrationCount"].(int); !ok || cc != 2 {
		t.Errorf("stats[calibrationCount] = %v, want 2", stats["calibrationCount"])
	}
	if ta, ok := stats["totalAdjustment"].(int); !ok || ta != 200 {
		t.Errorf("stats[totalAdjustment] = %v, want 200", stats["totalAdjustment"])
	}
}

// TestCWCV3_Reset 验证 Reset 清空统计但保留 targetSize 和 maxAdjustment 配置。
func TestCWCV3_Reset(t *testing.T) {
	c := NewContextWindowCalibratorV3(1000, 100)
	c.Calibrate(1100)
	c.Calibrate(1200) // target=1200, totalAdjustment=200

	c.Reset()

	stats := c.GetStats()
	if cc, ok := stats["calibrationCount"].(int); !ok || cc != 0 {
		t.Errorf("after Reset, stats[calibrationCount] = %v, want 0", stats["calibrationCount"])
	}
	if ta, ok := stats["totalAdjustment"].(int); !ok || ta != 0 {
		t.Errorf("after Reset, stats[totalAdjustment] = %v, want 0", stats["totalAdjustment"])
	}
	// targetSize 不应被重置
	if ts, ok := stats["targetSize"].(int); !ok || ts != 1200 {
		t.Errorf("after Reset, stats[targetSize] = %v, want 1200 (not reset)", stats["targetSize"])
	}
	// maxAdjustment 不应被重置
	if ma, ok := stats["maxAdjustment"].(int); !ok || ma != 100 {
		t.Errorf("after Reset, stats[maxAdjustment] = %v, want 100 (not reset)", stats["maxAdjustment"])
	}
}

// ════════════════════════════════════════════════════════════
// OPT-234: TokenAwareLeakDetector 测试
// ════════════════════════════════════════════════════════════

// TestTALD_SetBaseline_CheckUsage_DetectsLeak 验证 SetBaseline + CheckUsage 检测到泄漏。
func TestTALD_SetBaseline_CheckUsage_DetectsLeak(t *testing.T) {
	d := NewTokenAwareLeakDetector()
	d.SetBaseline("source1", 100)
	leak := d.CheckUsage("source1", 150)
	if leak != 50 {
		t.Errorf("CheckUsage(source1, 150) = %d, want 50", leak)
	}
}

// TestTALD_CheckUsage_NoLeak_ReturnsZero 验证无泄漏时返回 0。
func TestTALD_CheckUsage_NoLeak_ReturnsZero(t *testing.T) {
	d := NewTokenAwareLeakDetector()
	d.SetBaseline("source1", 100)

	// 低于基线，无泄漏
	leak := d.CheckUsage("source1", 80)
	if leak != 0 {
		t.Errorf("CheckUsage(source1, 80) = %d, want 0 (below baseline)", leak)
	}

	// 等于基线，无泄漏
	leak2 := d.CheckUsage("source1", 100)
	if leak2 != 0 {
		t.Errorf("CheckUsage(source1, 100) = %d, want 0 (equals baseline)", leak2)
	}

	// 未设置基线的来源
	leak3 := d.CheckUsage("unknown", 200)
	if leak3 != 0 {
		t.Errorf("CheckUsage(unknown, 200) = %d, want 0 (no baseline set)", leak3)
	}
}

// TestTALD_GetLeakSources 验证 GetLeakSources 返回有泄漏的来源。
func TestTALD_GetLeakSources(t *testing.T) {
	d := NewTokenAwareLeakDetector()
	d.SetBaseline("a", 100)
	d.SetBaseline("b", 100)
	d.SetBaseline("c", 100)

	d.CheckUsage("a", 150) // leak
	d.CheckUsage("b", 80)  // no leak
	d.CheckUsage("c", 200) // leak

	sources := d.GetLeakSources()
	if len(sources) != 2 {
		t.Errorf("GetLeakSources() returned %d sources, want 2", len(sources))
	}
	found := map[string]bool{}
	for _, s := range sources {
		found[s] = true
	}
	if !found["a"] {
		t.Errorf("GetLeakSources() missing %q", "a")
	}
	if !found["c"] {
		t.Errorf("GetLeakSources() missing %q", "c")
	}
}

// TestTALD_Stats_LeaksDetected 验证 Stats 中的 leaksDetected。
func TestTALD_Stats_LeaksDetected(t *testing.T) {
	d := NewTokenAwareLeakDetector()
	d.SetBaseline("a", 100)
	d.SetBaseline("b", 100)

	d.CheckUsage("a", 150) // leak 50
	d.CheckUsage("b", 120) // leak 20

	stats := d.GetStats()
	if ld, ok := stats["leaksDetected"].(int); !ok || ld != 2 {
		t.Errorf("stats[leaksDetected] = %v, want 2", stats["leaksDetected"])
	}
	if tlt, ok := stats["totalLeakedTokens"].(int); !ok || tlt != 70 {
		t.Errorf("stats[totalLeakedTokens] = %v, want 70", stats["totalLeakedTokens"])
	}
	if ts, ok := stats["trackedSources"].(int); !ok || ts != 2 {
		t.Errorf("stats[trackedSources] = %v, want 2", stats["trackedSources"])
	}
}

// TestTALD_Reset 验证 Reset 清空所有状态。
func TestTALD_Reset(t *testing.T) {
	d := NewTokenAwareLeakDetector()
	d.SetBaseline("a", 100)
	d.CheckUsage("a", 150)

	d.Reset()

	stats := d.GetStats()
	if ld, ok := stats["leaksDetected"].(int); !ok || ld != 0 {
		t.Errorf("after Reset, stats[leaksDetected] = %v, want 0", stats["leaksDetected"])
	}
	if ts, ok := stats["trackedSources"].(int); !ok || ts != 0 {
		t.Errorf("after Reset, stats[trackedSources] = %v, want 0", stats["trackedSources"])
	}
	// Reset 后无泄漏来源
	if sources := d.GetLeakSources(); len(sources) != 0 {
		t.Errorf("after Reset, GetLeakSources() returned %d sources, want 0", len(sources))
	}
}

// ════════════════════════════════════════════════════════════
// OPT-235: PromptCacheWarmthTracker 测试
// ════════════════════════════════════════════════════════════

// TestPCWT_Warm_GetWarmth 验证 Warm + GetWarmth 温度增加。
func TestPCWT_Warm_GetWarmth(t *testing.T) {
	tracker := NewPromptCacheWarmthTracker(5)
	tracker.Warm("key1")
	tracker.Warm("key1")
	tracker.Warm("key1")
	if w := tracker.GetWarmth("key1"); w != 3 {
		t.Errorf("GetWarmth(key1) = %d, want 3", w)
	}
	// 未操作过的 key 温度应为 0
	if w := tracker.GetWarmth("key2"); w != 0 {
		t.Errorf("GetWarmth(key2) = %d, want 0", w)
	}
}

// TestPCWT_Cool_DecreasesTemperature 验证 Cool 降低温度且不低于 0。
func TestPCWT_Cool_DecreasesTemperature(t *testing.T) {
	tracker := NewPromptCacheWarmthTracker(5)
	tracker.Warm("key1")
	tracker.Warm("key1")
	tracker.Warm("key1") // warmth=3
	tracker.Cool("key1") // warmth=2
	if w := tracker.GetWarmth("key1"); w != 2 {
		t.Errorf("after Cool, GetWarmth(key1) = %d, want 2", w)
	}
	// Cool 到 0 后不再降低
	tracker.Cool("key1")
	tracker.Cool("key1")
	tracker.Cool("key1") // warmth should be 0, not negative
	if w := tracker.GetWarmth("key1"); w != 0 {
		t.Errorf("after multiple Cool, GetWarmth(key1) = %d, want 0 (floor at 0)", w)
	}
}

// TestPCWT_IsWarm_ExceedsThreshold 验证 IsWarm 在超过阈值时返回 true。
func TestPCWT_IsWarm_ExceedsThreshold(t *testing.T) {
	tracker := NewPromptCacheWarmthTracker(5)
	// warmth=5，不严格超过阈值（IsWarm 使用 > 比较）
	for i := 0; i < 5; i++ {
		tracker.Warm("key1")
	}
	if tracker.IsWarm("key1") {
		t.Errorf("IsWarm(key1) with warmth=5, threshold=5 = true, want false (not strictly greater)")
	}
	// warmth=6，超过阈值
	tracker.Warm("key1")
	if !tracker.IsWarm("key1") {
		t.Errorf("IsWarm(key1) with warmth=6, threshold=5 = false, want true")
	}
}

// TestPCWT_Stats_WarmedCount 验证 Stats 中的 warmedCount。
func TestPCWT_Stats_WarmedCount(t *testing.T) {
	tracker := NewPromptCacheWarmthTracker(2)
	tracker.Warm("key1")
	tracker.Warm("key1")
	tracker.Warm("key1") // warmth=3
	tracker.Cool("key2") // cooledCount=1

	stats := tracker.GetStats()
	if wc, ok := stats["warmedCount"].(int); !ok || wc != 3 {
		t.Errorf("stats[warmedCount] = %v, want 3", stats["warmedCount"])
	}
	if cc, ok := stats["cooledCount"].(int); !ok || cc != 1 {
		t.Errorf("stats[cooledCount] = %v, want 1", stats["cooledCount"])
	}
	// warmKeyCount: key1 warmth=3 > 2 → warm, key2 warmth=0 → not warm
	if wkc, ok := stats["warmKeyCount"].(int); !ok || wkc != 1 {
		t.Errorf("stats[warmKeyCount] = %v, want 1", stats["warmKeyCount"])
	}
}

// TestPCWT_Reset 验证 Reset 清空状态但保留 threshold。
func TestPCWT_Reset(t *testing.T) {
	tracker := NewPromptCacheWarmthTracker(5)
	tracker.Warm("key1")
	tracker.Warm("key1")

	tracker.Reset()

	if w := tracker.GetWarmth("key1"); w != 0 {
		t.Errorf("after Reset, GetWarmth(key1) = %d, want 0", w)
	}
	stats := tracker.GetStats()
	if wc, ok := stats["warmedCount"].(int); !ok || wc != 0 {
		t.Errorf("after Reset, stats[warmedCount] = %v, want 0", stats["warmedCount"])
	}
	if cc, ok := stats["cooledCount"].(int); !ok || cc != 0 {
		t.Errorf("after Reset, stats[cooledCount] = %v, want 0", stats["cooledCount"])
	}
	// threshold 应保留
	if th, ok := stats["threshold"].(int); !ok || th != 5 {
		t.Errorf("after Reset, stats[threshold] = %v, want 5 (not reset)", stats["threshold"])
	}
}
