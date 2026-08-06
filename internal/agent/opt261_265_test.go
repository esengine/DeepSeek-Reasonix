package agent

import (
	"math"
	"testing"
)

// opt265FloatEq 判断两个浮点数是否在容差范围内相等。
func opt265FloatEq(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-261: TokenAwareLoadShedder 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestTALS_NewDefaults 验证构造后默认状态及非法策略归一化。
func TestTALS_NewDefaults(t *testing.T) {
	s := NewTokenAwareLoadShedder(100, "oldest")
	if s.GetLoad() != 0 {
		t.Errorf("GetLoad() = %d, 期望 0", s.GetLoad())
	}
	stats := s.GetStats()
	if stats["threshold"].(int) != 100 {
		t.Errorf("threshold = %v, 期望 100", stats["threshold"])
	}
	if stats["currentLoad"].(int) != 0 {
		t.Errorf("currentLoad = %v, 期望 0", stats["currentLoad"])
	}
	if stats["shedCount"].(int) != 0 {
		t.Errorf("shedCount = %v, 期望 0", stats["shedCount"])
	}
	if stats["totalShedTokens"].(int) != 0 {
		t.Errorf("totalShedTokens = %v, 期望 0", stats["totalShedTokens"])
	}
	if stats["shedStrategy"].(string) != "oldest" {
		t.Errorf("shedStrategy = %v, 期望 oldest", stats["shedStrategy"])
	}
	// 非法策略应回退为 oldest
	s2 := NewTokenAwareLoadShedder(50, "bogus")
	if s2.GetStats()["shedStrategy"].(string) != "oldest" {
		t.Errorf("非法策略应回退为 oldest")
	}
}

// TestTALS_ShedNoShedding 验证负载未超阈值时不执行脱落。
func TestTALS_ShedNoShedding(t *testing.T) {
	s := NewTokenAwareLoadShedder(100, "oldest")
	shed, ok := s.Shed(50)
	if ok {
		t.Errorf("未超阈值时 ok 应为 false")
	}
	if shed != 0 {
		t.Errorf("未超阈值时 shed = %d, 期望 0", shed)
	}
	if s.GetLoad() != 50 {
		t.Errorf("GetLoad() = %d, 期望 50", s.GetLoad())
	}
	stats := s.GetStats()
	if stats["shedCount"].(int) != 0 {
		t.Errorf("未脱落时 shedCount = %v, 期望 0", stats["shedCount"])
	}
}

// TestTALS_ShedOldest 验证 oldest 策略脱落全部溢出量并将负载降至阈值。
func TestTALS_ShedOldest(t *testing.T) {
	s := NewTokenAwareLoadShedder(100, "oldest")
	s.Shed(80) // currentLoad = 80
	shed, ok := s.Shed(50) // 80+50=130, excess=30
	if !ok {
		t.Errorf("超阈值时 ok 应为 true")
	}
	if shed != 30 {
		t.Errorf("oldest shed = %d, 期望 30", shed)
	}
	if s.GetLoad() != 100 {
		t.Errorf("oldest 脱落后 GetLoad() = %d, 期望 100", s.GetLoad())
	}
	stats := s.GetStats()
	if stats["totalShedTokens"].(int) != 30 {
		t.Errorf("totalShedTokens = %v, 期望 30", stats["totalShedTokens"])
	}
	if stats["shedCount"].(int) != 1 {
		t.Errorf("shedCount = %v, 期望 1", stats["shedCount"])
	}
}

// TestTALS_ShedRandom 验证 random 策略脱落约一半溢出量。
func TestTALS_ShedRandom(t *testing.T) {
	s := NewTokenAwareLoadShedder(100, "random")
	s.Shed(80) // currentLoad = 80
	shed, ok := s.Shed(50) // 80+50=130, excess=30, random -> (30+1)/2=15
	if !ok {
		t.Errorf("超阈值时 ok 应为 true")
	}
	if shed != 15 {
		t.Errorf("random shed = %d, 期望 15", shed)
	}
	// 80+50-15 = 115
	if s.GetLoad() != 115 {
		t.Errorf("random 脱落后 GetLoad() = %d, 期望 115", s.GetLoad())
	}
}

// TestTALS_ShouldShedAndThreshold 验证 ShouldShed 判定与 SetThreshold 动态调整。
func TestTALS_ShouldShedAndThreshold(t *testing.T) {
	s := NewTokenAwareLoadShedder(100, "oldest")
	if s.ShouldShed(50) {
		t.Errorf("0+50 未超 100, ShouldShed 应为 false")
	}
	if !s.ShouldShed(150) {
		t.Errorf("0+150 超 100, ShouldShed 应为 true")
	}
	s.SetThreshold(40)
	if !s.ShouldShed(50) {
		t.Errorf("阈值降为 40 后 0+50 超 40, ShouldShed 应为 true")
	}
	if s.ShouldShed(40) {
		t.Errorf("0+40 未超 40, ShouldShed 应为 false")
	}
}

// TestTALS_GetStatsAndReset 验证统计快照与 Reset 保留配置。
func TestTALS_GetStatsAndReset(t *testing.T) {
	s := NewTokenAwareLoadShedder(100, "random")
	s.Shed(80)
	s.Shed(50) // 触发脱落
	stats := s.GetStats()
	if stats["shedStrategy"].(string) != "random" {
		t.Errorf("shedStrategy = %v, 期望 random", stats["shedStrategy"])
	}
	if stats["threshold"].(int) != 100 {
		t.Errorf("threshold = %v, 期望 100", stats["threshold"])
	}
	if stats["shedCount"].(int) != 1 {
		t.Errorf("shedCount = %v, 期望 1", stats["shedCount"])
	}
	s.Reset()
	stats = s.GetStats()
	if stats["currentLoad"].(int) != 0 {
		t.Errorf("Reset 后 currentLoad = %v, 期望 0", stats["currentLoad"])
	}
	if stats["shedCount"].(int) != 0 {
		t.Errorf("Reset 后 shedCount = %v, 期望 0", stats["shedCount"])
	}
	if stats["totalShedTokens"].(int) != 0 {
		t.Errorf("Reset 后 totalShedTokens = %v, 期望 0", stats["totalShedTokens"])
	}
	if stats["threshold"].(int) != 100 {
		t.Errorf("Reset 后 threshold = %v, 期望 100（应保留配置）", stats["threshold"])
	}
	if stats["shedStrategy"].(string) != "random" {
		t.Errorf("Reset 后 shedStrategy = %v, 期望 random（应保留配置）", stats["shedStrategy"])
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-262: CacheInvalidationCompactor 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestCIC_NewDefaults 验证构造后默认状态。
func TestCIC_NewDefaults(t *testing.T) {
	c := NewCacheInvalidationCompactor()
	if c.GetCompactedCount() != 0 {
		t.Errorf("GetCompactedCount() = %d, 期望 0", c.GetCompactedCount())
	}
	if !opt265FloatEq(c.GetReductionRate(), 0) {
		t.Errorf("GetReductionRate() = %v, 期望 0", c.GetReductionRate())
	}
	stats := c.GetStats()
	if stats["totalCompacted"].(int) != 0 {
		t.Errorf("totalCompacted = %v, 期望 0", stats["totalCompacted"])
	}
	if stats["totalReduced"].(int) != 0 {
		t.Errorf("totalReduced = %v, 期望 0", stats["totalReduced"])
	}
	if stats["compactionCount"].(int) != 0 {
		t.Errorf("compactionCount = %v, 期望 0", stats["compactionCount"])
	}
	if stats["uniqueKeys"].(int) != 0 {
		t.Errorf("uniqueKeys = %v, 期望 0", stats["uniqueKeys"])
	}
}

// TestCIC_CompactDedup 验证 Compact 去重并保持首次出现顺序。
func TestCIC_CompactDedup(t *testing.T) {
	c := NewCacheInvalidationCompactor()
	out := c.Compact([]string{"a", "b", "a", "c", "b"})
	if len(out) != 3 {
		t.Fatalf("去重后长度 = %d, 期望 3", len(out))
	}
	if out[0] != "a" || out[1] != "b" || out[2] != "c" {
		t.Errorf("去重顺序 = %v, 期望 [a b c]", out)
	}
	if c.GetCompactedCount() != 3 {
		t.Errorf("GetCompactedCount() = %d, 期望 3", c.GetCompactedCount())
	}
}

// TestCIC_ReductionRate 验证重复消除率计算。
func TestCIC_ReductionRate(t *testing.T) {
	c := NewCacheInvalidationCompactor()
	c.Compact([]string{"a", "b", "a", "c", "b"}) // 输入 5, 去重 3, 减少 2
	if !opt265FloatEq(c.GetReductionRate(), 0.4) {
		t.Errorf("GetReductionRate() = %v, 期望 0.4", c.GetReductionRate())
	}
}

// TestCIC_MultipleCompacts 验证多次压缩累计统计与唯一 key 累积。
func TestCIC_MultipleCompacts(t *testing.T) {
	c := NewCacheInvalidationCompactor()
	c.Compact([]string{"a", "b", "a", "c", "b"}) // totalCompacted=5, reduced=2, count=1, unique=3
	c.Compact([]string{"a", "d"})                // totalCompacted=7, reduced=2, count=2, unique=4
	stats := c.GetStats()
	if stats["totalCompacted"].(int) != 7 {
		t.Errorf("totalCompacted = %v, 期望 7", stats["totalCompacted"])
	}
	if stats["totalReduced"].(int) != 2 {
		t.Errorf("totalReduced = %v, 期望 2", stats["totalReduced"])
	}
	if stats["compactionCount"].(int) != 2 {
		t.Errorf("compactionCount = %v, 期望 2", stats["compactionCount"])
	}
	if stats["uniqueKeys"].(int) != 4 {
		t.Errorf("uniqueKeys = %v, 期望 4", stats["uniqueKeys"])
	}
}

// TestCIC_EmptyInput 验证空输入仍计一次压缩且不新增 key。
func TestCIC_EmptyInput(t *testing.T) {
	c := NewCacheInvalidationCompactor()
	out := c.Compact([]string{})
	if len(out) != 0 {
		t.Errorf("空输入应返回空切片, 得到 %v", out)
	}
	stats := c.GetStats()
	if stats["compactionCount"].(int) != 1 {
		t.Errorf("空输入仍应计一次 compactionCount = %v, 期望 1", stats["compactionCount"])
	}
	if stats["uniqueKeys"].(int) != 0 {
		t.Errorf("空输入 uniqueKeys = %v, 期望 0", stats["uniqueKeys"])
	}
	if !opt265FloatEq(c.GetReductionRate(), 0) {
		t.Errorf("空输入 GetReductionRate() = %v, 期望 0", c.GetReductionRate())
	}
}

// TestCIC_GetStatsAndReset 验证统计快照与 Reset 清空。
func TestCIC_GetStatsAndReset(t *testing.T) {
	c := NewCacheInvalidationCompactor()
	c.Compact([]string{"a", "a", "b"})
	c.Reset()
	if c.GetCompactedCount() != 0 {
		t.Errorf("Reset 后 GetCompactedCount() = %d, 期望 0", c.GetCompactedCount())
	}
	stats := c.GetStats()
	if stats["totalCompacted"].(int) != 0 {
		t.Errorf("Reset 后 totalCompacted = %v, 期望 0", stats["totalCompacted"])
	}
	if stats["compactionCount"].(int) != 0 {
		t.Errorf("Reset 后 compactionCount = %v, 期望 0", stats["compactionCount"])
	}
	if stats["uniqueKeys"].(int) != 0 {
		t.Errorf("Reset 后 uniqueKeys = %v, 期望 0", stats["uniqueKeys"])
	}
	if !opt265FloatEq(c.GetReductionRate(), 0) {
		t.Errorf("Reset 后 GetReductionRate() = %v, 期望 0", c.GetReductionRate())
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-263: ContextWindowDynamicResizer 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestCWDR_NewDefaults 验证构造后初始大小钳制与默认统计。
func TestCWDR_NewDefaults(t *testing.T) {
	r := NewContextWindowDynamicResizer(10, 100, 50)
	if r.GetCurrentSize() != 50 {
		t.Errorf("GetCurrentSize() = %d, 期望 50", r.GetCurrentSize())
	}
	// 低于下限应钳制到 min
	r2 := NewContextWindowDynamicResizer(10, 100, 5)
	if r2.GetCurrentSize() != 10 {
		t.Errorf("低于下限应钳制为 10, 得到 %d", r2.GetCurrentSize())
	}
	// 高于上限应钳制到 max
	r3 := NewContextWindowDynamicResizer(10, 100, 200)
	if r3.GetCurrentSize() != 100 {
		t.Errorf("高于上限应钳制为 100, 得到 %d", r3.GetCurrentSize())
	}
	stats := r.GetStats()
	if stats["resizeCount"].(int) != 0 {
		t.Errorf("resizeCount = %v, 期望 0", stats["resizeCount"])
	}
	if stats["totalResized"].(int) != 0 {
		t.Errorf("totalResized = %v, 期望 0", stats["totalResized"])
	}
}

// TestCWDR_ResizeClamp 验证 Resize 钳制到 min/max 范围。
func TestCWDR_ResizeClamp(t *testing.T) {
	r := NewContextWindowDynamicResizer(10, 100, 50)
	if r.Resize(150) != 100 {
		t.Errorf("Resize(150) 应钳制为 100, 得到 %d", r.GetCurrentSize())
	}
	if r.Resize(5) != 10 {
		t.Errorf("Resize(5) 应钳制为 10, 得到 %d", r.GetCurrentSize())
	}
	stats := r.GetStats()
	// 50->100 (delta 50), 100->10 (delta 90), totalResized=140, resizeCount=2
	if stats["resizeCount"].(int) != 2 {
		t.Errorf("resizeCount = %v, 期望 2", stats["resizeCount"])
	}
	if stats["totalResized"].(int) != 140 {
		t.Errorf("totalResized = %v, 期望 140", stats["totalResized"])
	}
}

// TestCWDR_GrowAndShrink 验证按量增长与缩小。
func TestCWDR_GrowAndShrink(t *testing.T) {
	r := NewContextWindowDynamicResizer(10, 100, 50)
	if r.Grow(30) != 80 {
		t.Errorf("Grow(30) 应为 80, 得到 %d", r.GetCurrentSize())
	}
	if r.Shrink(100) != 10 {
		t.Errorf("Shrink(100) 应钳制为 10, 得到 %d", r.GetCurrentSize())
	}
	stats := r.GetStats()
	if stats["resizeCount"].(int) != 2 {
		t.Errorf("resizeCount = %v, 期望 2", stats["resizeCount"])
	}
}

// TestCWDR_NoOpResize 验证目标等于当前值时不计调整。
func TestCWDR_NoOpResize(t *testing.T) {
	r := NewContextWindowDynamicResizer(10, 100, 50)
	if r.Resize(50) != 50 {
		t.Errorf("Resize(50) 应保持 50, got %d", r.GetCurrentSize())
	}
	stats := r.GetStats()
	if stats["resizeCount"].(int) != 0 {
		t.Errorf("无变化时 resizeCount = %v, 期望 0", stats["resizeCount"])
	}
	if stats["totalResized"].(int) != 0 {
		t.Errorf("无变化时 totalResized = %v, 期望 0", stats["totalResized"])
	}
}

// TestCWDR_MinMaxSwap 验证 min>max 时自动交换。
func TestCWDR_MinMaxSwap(t *testing.T) {
	r := NewContextWindowDynamicResizer(100, 10, 50)
	stats := r.GetStats()
	if stats["minSize"].(int) != 10 {
		t.Errorf("min>max 时 minSize 应交换为 10, 得到 %v", stats["minSize"])
	}
	if stats["maxSize"].(int) != 100 {
		t.Errorf("min>max 时 maxSize 应交换为 100, 得到 %v", stats["maxSize"])
	}
	// 50 在 [10,100] 范围内
	if r.GetCurrentSize() != 50 {
		t.Errorf("initialSize 50 应保留, 得到 %d", r.GetCurrentSize())
	}
}

// TestCWDR_GetStatsAndReset 验证统计快照与 Reset 恢复到 minSize。
func TestCWDR_GetStatsAndReset(t *testing.T) {
	r := NewContextWindowDynamicResizer(10, 100, 50)
	r.Grow(40) // 50->90
	r.Reset()
	if r.GetCurrentSize() != 10 {
		t.Errorf("Reset 后 GetCurrentSize() = %d, 期望恢复为 minSize 10", r.GetCurrentSize())
	}
	stats := r.GetStats()
	if stats["resizeCount"].(int) != 0 {
		t.Errorf("Reset 后 resizeCount = %v, 期望 0", stats["resizeCount"])
	}
	if stats["totalResized"].(int) != 0 {
		t.Errorf("Reset 后 totalResized = %v, 期望 0", stats["totalResized"])
	}
	if stats["minSize"].(int) != 10 {
		t.Errorf("Reset 后 minSize = %v, 期望 10（应保留配置）", stats["minSize"])
	}
	if stats["maxSize"].(int) != 100 {
		t.Errorf("Reset 后 maxSize = %v, 期望 100（应保留配置）", stats["maxSize"])
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-264: TokenAwareAdmissionGatekeeper 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestTAAG_NewDefaults 验证构造后默认状态。
func TestTAAG_NewDefaults(t *testing.T) {
	g := NewTokenAwareAdmissionGatekeeper(100)
	if g.IsFull() {
		t.Errorf("初始 IsFull() 应为 false")
	}
	if !opt265FloatEq(g.GetAdmissionRate(), 0) {
		t.Errorf("初始 GetAdmissionRate() = %v, 期望 0", g.GetAdmissionRate())
	}
	stats := g.GetStats()
	if stats["capacity"].(int) != 100 {
		t.Errorf("capacity = %v, 期望 100", stats["capacity"])
	}
	if stats["currentAdmissions"].(int) != 0 {
		t.Errorf("currentAdmissions = %v, 期望 0", stats["currentAdmissions"])
	}
	if stats["admittedCount"].(int) != 0 {
		t.Errorf("admittedCount = %v, 期望 0", stats["admittedCount"])
	}
	if stats["rejectedCount"].(int) != 0 {
		t.Errorf("rejectedCount = %v, 期望 0", stats["rejectedCount"])
	}
}

// TestTAAG_AdmitAndReject 验证准入与拒绝逻辑。
func TestTAAG_AdmitAndReject(t *testing.T) {
	g := NewTokenAwareAdmissionGatekeeper(100)
	if !g.Admit(60) {
		t.Errorf("Admit(60) 应准入成功")
	}
	if g.Admit(50) {
		t.Errorf("60+50=110 超 100, Admit(50) 应拒绝")
	}
	stats := g.GetStats()
	if stats["currentAdmissions"].(int) != 60 {
		t.Errorf("currentAdmissions = %v, 期望 60", stats["currentAdmissions"])
	}
	if stats["admittedCount"].(int) != 1 {
		t.Errorf("admittedCount = %v, 期望 1", stats["admittedCount"])
	}
	if stats["rejectedCount"].(int) != 1 {
		t.Errorf("rejectedCount = %v, 期望 1", stats["rejectedCount"])
	}
}

// TestTAAG_Release 验证释放预算且不低于 0。
func TestTAAG_Release(t *testing.T) {
	g := NewTokenAwareAdmissionGatekeeper(100)
	g.Admit(60)
	g.Release(60)
	stats := g.GetStats()
	if stats["currentAdmissions"].(int) != 0 {
		t.Errorf("Release 后 currentAdmissions = %v, 期望 0", stats["currentAdmissions"])
	}
	// 释放超过当前不应变为负数
	g.Release(100)
	if g.GetStats()["currentAdmissions"].(int) != 0 {
		t.Errorf("过度释放后 currentAdmissions 应钳制为 0")
	}
}

// TestTAAG_IsFull 验证容量满载判定。
func TestTAAG_IsFull(t *testing.T) {
	g := NewTokenAwareAdmissionGatekeeper(100)
	g.Admit(100)
	if !g.IsFull() {
		t.Errorf("Admit(100) 后 IsFull() 应为 true")
	}
	g.Release(50)
	if g.IsFull() {
		t.Errorf("Release(50) 后 IsFull() 应为 false")
	}
}

// TestTAAG_AdmissionRate 验证准入成功率计算。
func TestTAAG_AdmissionRate(t *testing.T) {
	g := NewTokenAwareAdmissionGatekeeper(100)
	g.Admit(60) // 准入, admitted=1
	g.Admit(50) // 拒绝, rejected=1
	g.Admit(10) // 60+10=70<=100 准入, admitted=2
	// rate = 2/(2+1) = 2/3
	if !opt265FloatEq(g.GetAdmissionRate(), 2.0/3.0) {
		t.Errorf("GetAdmissionRate() = %v, 期望 %v", g.GetAdmissionRate(), 2.0/3.0)
	}
	stats := g.GetStats()
	if !opt265FloatEq(stats["admissionRate"].(float64), 2.0/3.0) {
		t.Errorf("stats admissionRate = %v, 期望 %v", stats["admissionRate"], 2.0/3.0)
	}
}

// TestTAAG_GetStatsAndReset 验证统计快照与 Reset 保留 capacity。
func TestTAAG_GetStatsAndReset(t *testing.T) {
	g := NewTokenAwareAdmissionGatekeeper(100)
	g.Admit(60)
	g.Admit(50)
	g.Reset()
	stats := g.GetStats()
	if stats["currentAdmissions"].(int) != 0 {
		t.Errorf("Reset 后 currentAdmissions = %v, 期望 0", stats["currentAdmissions"])
	}
	if stats["admittedCount"].(int) != 0 {
		t.Errorf("Reset 后 admittedCount = %v, 期望 0", stats["admittedCount"])
	}
	if stats["rejectedCount"].(int) != 0 {
		t.Errorf("Reset 后 rejectedCount = %v, 期望 0", stats["rejectedCount"])
	}
	if stats["capacity"].(int) != 100 {
		t.Errorf("Reset 后 capacity = %v, 期望 100（应保留配置）", stats["capacity"])
	}
	if !opt265FloatEq(g.GetAdmissionRate(), 0) {
		t.Errorf("Reset 后 GetAdmissionRate() = %v, 期望 0", g.GetAdmissionRate())
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-265: PromptCacheProactiveWarmer 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestPCPW_NewDefaults 验证构造后默认状态及空策略回退。
func TestPCPW_NewDefaults(t *testing.T) {
	w := NewPromptCacheProactiveWarmer("priority")
	if w.GetTargetCount() != 0 {
		t.Errorf("GetTargetCount() = %d, 期望 0", w.GetTargetCount())
	}
	stats := w.GetStats()
	if stats["targetCount"].(int) != 0 {
		t.Errorf("targetCount = %v, 期望 0", stats["targetCount"])
	}
	if stats["warmedCount"].(int) != 0 {
		t.Errorf("warmedCount = %v, 期望 0", stats["warmedCount"])
	}
	if stats["totalWarmed"].(int) != 0 {
		t.Errorf("totalWarmed = %v, 期望 0", stats["totalWarmed"])
	}
	if stats["warmingStrategy"].(string) != "priority" {
		t.Errorf("warmingStrategy = %v, 期望 priority", stats["warmingStrategy"])
	}
	// 空策略应回退为 priority
	w2 := NewPromptCacheProactiveWarmer("")
	if w2.GetStats()["warmingStrategy"].(string) != "priority" {
		t.Errorf("空策略应回退为 priority")
	}
}

// TestPCPW_AddTarget 验证添加目标与重复更新。
func TestPCPW_AddTarget(t *testing.T) {
	w := NewPromptCacheProactiveWarmer("priority")
	w.AddTarget("k1", 10)
	w.AddTarget("k2", 20)
	if w.GetTargetCount() != 2 {
		t.Errorf("GetTargetCount() = %d, 期望 2", w.GetTargetCount())
	}
	// 重复添加同一 key 应更新而非新增
	w.AddTarget("k1", 99)
	if w.GetTargetCount() != 2 {
		t.Errorf("重复 AddTarget 后 GetTargetCount() = %d, 期望 2", w.GetTargetCount())
	}
}

// TestPCPW_WarmUp 验证对已注册目标的预热。
func TestPCPW_WarmUp(t *testing.T) {
	w := NewPromptCacheProactiveWarmer("priority")
	w.AddTarget("k1", 10)
	if !w.WarmUp("k1") {
		t.Errorf("WarmUp 已注册目标应返回 true")
	}
	if !w.IsWarmed("k1") {
		t.Errorf("WarmUp 后 IsWarmed(\"k1\") 应为 true")
	}
	stats := w.GetStats()
	if stats["totalWarmed"].(int) != 1 {
		t.Errorf("totalWarmed = %v, 期望 1", stats["totalWarmed"])
	}
	if stats["warmedCount"].(int) != 1 {
		t.Errorf("warmedCount = %v, 期望 1", stats["warmedCount"])
	}
}

// TestPCPW_WarmUpSkips 验证重复预热与非目标 key 被跳过。
func TestPCPW_WarmUpSkips(t *testing.T) {
	w := NewPromptCacheProactiveWarmer("priority")
	w.AddTarget("k1", 10)
	w.WarmUp("k1")
	// 重复预热应跳过
	if w.WarmUp("k1") {
		t.Errorf("重复 WarmUp 应返回 false")
	}
	// 非目标 key 应跳过
	if w.WarmUp("unknown") {
		t.Errorf("非目标 key WarmUp 应返回 false")
	}
	stats := w.GetStats()
	if stats["totalSkipped"].(int) != 2 {
		t.Errorf("totalSkipped = %v, 期望 2", stats["totalSkipped"])
	}
	if stats["totalWarmed"].(int) != 1 {
		t.Errorf("totalWarmed = %v, 期望 1", stats["totalWarmed"])
	}
}

// TestPCPW_IsWarmed 验证预热状态查询。
func TestPCPW_IsWarmed(t *testing.T) {
	w := NewPromptCacheProactiveWarmer("priority")
	w.AddTarget("k1", 10)
	w.AddTarget("k2", 20)
	if w.IsWarmed("k1") {
		t.Errorf("未预热时 IsWarmed(\"k1\") 应为 false")
	}
	w.WarmUp("k1")
	if !w.IsWarmed("k1") {
		t.Errorf("预热后 IsWarmed(\"k1\") 应为 true")
	}
	if w.IsWarmed("k2") {
		t.Errorf("未预热的 k2 IsWarmed 应为 false")
	}
}

// TestPCPW_GetStatsAndReset 验证统计快照与 Reset 保留策略。
func TestPCPW_GetStatsAndReset(t *testing.T) {
	w := NewPromptCacheProactiveWarmer("lru")
	w.AddTarget("k1", 10)
	w.AddTarget("k2", 20)
	w.WarmUp("k1")
	w.WarmUp("k1")  // 跳过
	w.WarmUp("k3")  // 跳过（非目标）
	stats := w.GetStats()
	if stats["targetCount"].(int) != 2 {
		t.Errorf("targetCount = %v, 期望 2", stats["targetCount"])
	}
	if stats["warmedCount"].(int) != 1 {
		t.Errorf("warmedCount = %v, 期望 1", stats["warmedCount"])
	}
	if stats["totalWarmed"].(int) != 1 {
		t.Errorf("totalWarmed = %v, 期望 1", stats["totalWarmed"])
	}
	if stats["totalSkipped"].(int) != 2 {
		t.Errorf("totalSkipped = %v, 期望 2", stats["totalSkipped"])
	}
	if stats["warmingStrategy"].(string) != "lru" {
		t.Errorf("warmingStrategy = %v, 期望 lru", stats["warmingStrategy"])
	}
	w.Reset()
	stats = w.GetStats()
	if stats["targetCount"].(int) != 0 {
		t.Errorf("Reset 后 targetCount = %v, 期望 0", stats["targetCount"])
	}
	if stats["warmedCount"].(int) != 0 {
		t.Errorf("Reset 后 warmedCount = %v, 期望 0", stats["warmedCount"])
	}
	if stats["totalWarmed"].(int) != 0 {
		t.Errorf("Reset 后 totalWarmed = %v, 期望 0", stats["totalWarmed"])
	}
	if stats["warmingStrategy"].(string) != "lru" {
		t.Errorf("Reset 后 warmingStrategy = %v, 期望 lru（应保留配置）", stats["warmingStrategy"])
	}
}
