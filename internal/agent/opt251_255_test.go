package agent

import "testing"

// ── OPT-251: TokenAwareThrottleController 测试 ──

// TestTATC_AllowConsumesToken 验证 Allow 消耗令牌并返回 true。
func TestTATC_AllowConsumesToken(t *testing.T) {
	c := NewTokenAwareThrottleController(1000, 5)
	if !c.Allow() {
		t.Errorf("Allow() = false, want true (tokens available)")
	}
	if c.GetAvailableTokens() != 4 {
		t.Errorf("GetAvailableTokens = %d, want 4 after one Allow", c.GetAvailableTokens())
	}
}

// TestTATC_AllowThrottlesWhenEmpty 验证令牌耗尽时 Allow 节流。
func TestTATC_AllowThrottlesWhenEmpty(t *testing.T) {
	c := NewTokenAwareThrottleController(1000, 2)
	if !c.Allow() {
		t.Errorf("first Allow = false, want true")
	}
	if !c.Allow() {
		t.Errorf("second Allow = false, want true")
	}
	if c.Allow() {
		t.Errorf("third Allow = true, want false (throttled)")
	}
	stats := c.GetStats()
	if stats["passedCount"].(int) != 2 {
		t.Errorf("passedCount = %d, want 2", stats["passedCount"])
	}
	if stats["throttledCount"].(int) != 1 {
		t.Errorf("throttledCount = %d, want 1", stats["throttledCount"])
	}
}

// TestTATC_Refill 验证 Refill 补充令牌。
func TestTATC_Refill(t *testing.T) {
	c := NewTokenAwareThrottleController(1000, 5)
	for i := 0; i < 5; i++ {
		c.Allow()
	}
	if c.GetAvailableTokens() != 0 {
		t.Errorf("availableTokens = %d, want 0 after exhaustion", c.GetAvailableTokens())
	}
	c.Refill(3)
	if c.GetAvailableTokens() != 3 {
		t.Errorf("after Refill(3) availableTokens = %d, want 3", c.GetAvailableTokens())
	}
}

// TestTATC_RefillClampedToBurst 验证 Refill 不超过桶容量。
func TestTATC_RefillClampedToBurst(t *testing.T) {
	c := NewTokenAwareThrottleController(1000, 5)
	c.Refill(100)
	if c.GetAvailableTokens() != 5 {
		t.Errorf("after Refill(100) availableTokens = %d, want 5 (clamped to burst)", c.GetAvailableTokens())
	}
}

// TestTATC_GetAvailableTokens 验证初始与消耗后的可用令牌数。
func TestTATC_GetAvailableTokens(t *testing.T) {
	c := NewTokenAwareThrottleController(1000, 10)
	if c.GetAvailableTokens() != 10 {
		t.Errorf("initial availableTokens = %d, want 10", c.GetAvailableTokens())
	}
	c.Allow()
	if c.GetAvailableTokens() != 9 {
		t.Errorf("after one Allow availableTokens = %d, want 9", c.GetAvailableTokens())
	}
}

// TestTATC_StatsAndReset 验证统计字段与 Reset 恢复。
func TestTATC_StatsAndReset(t *testing.T) {
	c := NewTokenAwareThrottleController(1000, 3)
	c.Allow()
	c.Allow()
	c.Allow() // tokens 耗尽
	c.Allow() // 被节流
	stats := c.GetStats()
	if stats["passedCount"].(int) != 3 {
		t.Errorf("passedCount = %d, want 3", stats["passedCount"])
	}
	if stats["throttledCount"].(int) != 1 {
		t.Errorf("throttledCount = %d, want 1", stats["throttledCount"])
	}
	if stats["burst"].(int) != 3 {
		t.Errorf("burst = %d, want 3", stats["burst"])
	}
	if stats["rate"].(int) != 1000 {
		t.Errorf("rate = %d, want 1000", stats["rate"])
	}
	c.Reset()
	stats = c.GetStats()
	if stats["passedCount"].(int) != 0 {
		t.Errorf("after Reset passedCount = %d, want 0", stats["passedCount"])
	}
	if stats["throttledCount"].(int) != 0 {
		t.Errorf("after Reset throttledCount = %d, want 0", stats["throttledCount"])
	}
	if stats["availableTokens"].(int) != 3 {
		t.Errorf("after Reset availableTokens = %d, want 3", stats["availableTokens"])
	}
}

// ── OPT-252: CacheInvalidationBatcher 测试 ──

// TestCIB2_AddReturnsFalseBelowThreshold 验证未达阈值时 Add 返回 false。
func TestCIB2_AddReturnsFalseBelowThreshold(t *testing.T) {
	b := NewCacheInvalidationBatcher(3)
	if b.Add("a") {
		t.Errorf("Add(a) = true, want false before reaching threshold")
	}
	if b.Add("b") {
		t.Errorf("Add(b) = true, want false before reaching threshold")
	}
}

// TestCIB2_AddReturnsTrueAtThreshold 验证达到阈值时 Add 返回 true。
func TestCIB2_AddReturnsTrueAtThreshold(t *testing.T) {
	b := NewCacheInvalidationBatcher(3)
	b.Add("a")
	b.Add("b")
	if !b.Add("c") {
		t.Errorf("Add(c) = false, want true when reaching threshold")
	}
}

// TestCIB2_AddDeduplicates 验证重复 key 不会增加批次数量。
func TestCIB2_AddDeduplicates(t *testing.T) {
	b := NewCacheInvalidationBatcher(8)
	b.Add("a")
	b.Add("a")
	b.Add("a")
	if b.GetBatchCount() != 1 {
		t.Errorf("GetBatchCount = %d, want 1 (deduplicated)", b.GetBatchCount())
	}
	stats := b.GetStats()
	if stats["totalBatched"].(int) != 3 {
		t.Errorf("totalBatched = %d, want 3", stats["totalBatched"])
	}
}

// TestCIB2_FlushReturnsKeys 验证 Flush 返回所有 key 并清空批次。
func TestCIB2_FlushReturnsKeys(t *testing.T) {
	b := NewCacheInvalidationBatcher(8)
	b.Add("a")
	b.Add("b")
	b.Add("c")
	keys := b.Flush()
	if len(keys) != 3 {
		t.Errorf("Flush returned %d keys, want 3", len(keys))
	}
	if b.GetBatchCount() != 0 {
		t.Errorf("after Flush GetBatchCount = %d, want 0", b.GetBatchCount())
	}
}

// TestCIB2_GetBatchCount 验证当前批次数量。
func TestCIB2_GetBatchCount(t *testing.T) {
	b := NewCacheInvalidationBatcher(8)
	if b.GetBatchCount() != 0 {
		t.Errorf("initial GetBatchCount = %d, want 0", b.GetBatchCount())
	}
	b.Add("k1")
	b.Add("k2")
	if b.GetBatchCount() != 2 {
		t.Errorf("GetBatchCount = %d, want 2 after two Adds", b.GetBatchCount())
	}
}

// TestCIB2_StatsAndReset 验证统计字段与 Reset 恢复。
func TestCIB2_StatsAndReset(t *testing.T) {
	b := NewCacheInvalidationBatcher(8)
	b.Add("a")
	b.Flush() // totalBatched=1, totalFlushed=1
	b.Add("b")
	b.Add("c")
	b.Flush() // totalBatched=3, totalFlushed=3
	stats := b.GetStats()
	if stats["totalBatched"].(int) != 3 {
		t.Errorf("totalBatched = %d, want 3", stats["totalBatched"])
	}
	if stats["totalFlushed"].(int) != 3 {
		t.Errorf("totalFlushed = %d, want 3", stats["totalFlushed"])
	}
	if stats["batchSize"].(int) != 8 {
		t.Errorf("batchSize = %d, want 8", stats["batchSize"])
	}
	b.Reset()
	stats = b.GetStats()
	if stats["totalBatched"].(int) != 0 {
		t.Errorf("after Reset totalBatched = %d, want 0", stats["totalBatched"])
	}
	if stats["totalFlushed"].(int) != 0 {
		t.Errorf("after Reset totalFlushed = %d, want 0", stats["totalFlushed"])
	}
	if stats["currentBatchSize"].(int) != 0 {
		t.Errorf("after Reset currentBatchSize = %d, want 0", stats["currentBatchSize"])
	}
	if stats["batchSize"].(int) != 8 {
		t.Errorf("after Reset batchSize = %d, want 8 (preserved)", stats["batchSize"])
	}
}

// ── OPT-253: ContextWindowStabilizer 测试 ──

// TestCWS_RecordAndStabilize 验证基于历史平均值计算稳定窗口大小。
func TestCWS_RecordAndStabilize(t *testing.T) {
	s := NewContextWindowStabilizer(8192, 16)
	s.Record(100)
	s.Record(200)
	s.Record(300)
	size := s.Stabilize()
	if size != 200 {
		t.Errorf("Stabilize = %d, want 200 (avg)", size)
	}
}

// TestCWS_StabilizeEmptyHistory 验证无历史时返回 targetSize。
func TestCWS_StabilizeEmptyHistory(t *testing.T) {
	s := NewContextWindowStabilizer(8192, 16)
	size := s.Stabilize()
	if size != 8192 {
		t.Errorf("Stabilize with empty history = %d, want 8192 (targetSize)", size)
	}
	if score := s.GetStabilityScore(); score != 1.0 {
		t.Errorf("stabilityScore with empty history = %v, want 1.0", score)
	}
}

// TestCWS_StabilityScoreStable 验证相等历史值稳定度为 1.0。
func TestCWS_StabilityScoreStable(t *testing.T) {
	s := NewContextWindowStabilizer(8192, 16)
	s.Record(100)
	s.Record(100)
	s.Record(100)
	s.Stabilize()
	if score := s.GetStabilityScore(); score < 0.99 {
		t.Errorf("stabilityScore (equal values) = %v, want ~1.0", score)
	}
}

// TestCWS_StabilityScoreUnstable 验证高方差历史值稳定度为 0。
func TestCWS_StabilityScoreUnstable(t *testing.T) {
	s := NewContextWindowStabilizer(8192, 16)
	s.Record(0)
	s.Record(1000)
	s.Stabilize()
	if score := s.GetStabilityScore(); score > 0.01 {
		t.Errorf("stabilityScore (high variance) = %v, want ~0.0", score)
	}
}

// TestCWS_Stats 验证统计字段。
func TestCWS_Stats(t *testing.T) {
	s := NewContextWindowStabilizer(4096, 8)
	s.Record(100)
	s.Record(200)
	s.Stabilize()
	stats := s.GetStats()
	if stats["targetSize"].(int) != 4096 {
		t.Errorf("targetSize = %d, want 4096", stats["targetSize"])
	}
	if stats["currentSize"].(int) != 150 {
		t.Errorf("currentSize = %d, want 150", stats["currentSize"])
	}
	if stats["adjustments"].(int) != 1 {
		t.Errorf("adjustments = %d, want 1", stats["adjustments"])
	}
	if stats["historySize"].(int) != 2 {
		t.Errorf("historySize = %d, want 2", stats["historySize"])
	}
}

// TestCWS_Reset 验证 Reset 恢复状态但保留配置。
func TestCWS_Reset(t *testing.T) {
	s := NewContextWindowStabilizer(4096, 8)
	s.Record(100)
	s.Stabilize()
	s.Reset()
	stats := s.GetStats()
	if stats["adjustments"].(int) != 0 {
		t.Errorf("after Reset adjustments = %d, want 0", stats["adjustments"])
	}
	if stats["historySize"].(int) != 0 {
		t.Errorf("after Reset historySize = %d, want 0", stats["historySize"])
	}
	if stats["currentSize"].(int) != 4096 {
		t.Errorf("after Reset currentSize = %d, want 4096 (targetSize)", stats["currentSize"])
	}
	if stats["targetSize"].(int) != 4096 {
		t.Errorf("after Reset targetSize = %d, want 4096 (preserved)", stats["targetSize"])
	}
}

// ── OPT-254: TokenAwareBackpressureRelay 测试 ──

// TestTABR_RelaySuccess 验证容量内中继成功。
func TestTABR_RelaySuccess(t *testing.T) {
	r := NewTokenAwareBackpressureRelay(1000)
	if !r.Relay(300) {
		t.Errorf("Relay(300) = false, want true")
	}
	if r.GetLoad() != 300 {
		t.Errorf("GetLoad = %d, want 300", r.GetLoad())
	}
}

// TestTABR_RelayExceedsCapacityDrops 验证超容量时丢弃。
func TestTABR_RelayExceedsCapacityDrops(t *testing.T) {
	r := NewTokenAwareBackpressureRelay(1000)
	if !r.Relay(800) {
		t.Errorf("Relay(800) = false, want true")
	}
	if r.Relay(300) {
		t.Errorf("Relay(300) = true, want false (exceeds capacity)")
	}
	stats := r.GetStats()
	if stats["droppedCount"].(int) != 1 {
		t.Errorf("droppedCount = %d, want 1", stats["droppedCount"])
	}
	if stats["currentLoad"].(int) != 800 {
		t.Errorf("currentLoad = %d, want 800", stats["currentLoad"])
	}
}

// TestTABR_Release 验证释放负载。
func TestTABR_Release(t *testing.T) {
	r := NewTokenAwareBackpressureRelay(1000)
	r.Relay(500)
	r.Release(200)
	if r.GetLoad() != 300 {
		t.Errorf("after Release(200) GetLoad = %d, want 300", r.GetLoad())
	}
}

// TestTABR_GetUtilization 验证利用率计算。
func TestTABR_GetUtilization(t *testing.T) {
	r := NewTokenAwareBackpressureRelay(1000)
	r.Relay(500)
	if u := r.GetUtilization(); u != 0.5 {
		t.Errorf("GetUtilization = %v, want 0.5", u)
	}
}

// TestTABR_StatsAndReset 验证统计字段与 Reset 恢复。
func TestTABR_StatsAndReset(t *testing.T) {
	r := NewTokenAwareBackpressureRelay(1000)
	r.Relay(300)
	r.Relay(200) // load=500, relayCount=2, totalRelayed=500
	r.Relay(600) // dropped
	stats := r.GetStats()
	if stats["capacity"].(int) != 1000 {
		t.Errorf("capacity = %d, want 1000", stats["capacity"])
	}
	if stats["currentLoad"].(int) != 500 {
		t.Errorf("currentLoad = %d, want 500", stats["currentLoad"])
	}
	if stats["relayCount"].(int) != 2 {
		t.Errorf("relayCount = %d, want 2", stats["relayCount"])
	}
	if stats["droppedCount"].(int) != 1 {
		t.Errorf("droppedCount = %d, want 1", stats["droppedCount"])
	}
	if stats["totalRelayed"].(int) != 500 {
		t.Errorf("totalRelayed = %d, want 500", stats["totalRelayed"])
	}
	r.Reset()
	stats = r.GetStats()
	if stats["currentLoad"].(int) != 0 {
		t.Errorf("after Reset currentLoad = %d, want 0", stats["currentLoad"])
	}
	if stats["totalRelayed"].(int) != 0 {
		t.Errorf("after Reset totalRelayed = %d, want 0", stats["totalRelayed"])
	}
	if stats["capacity"].(int) != 1000 {
		t.Errorf("after Reset capacity = %d, want 1000 (preserved)", stats["capacity"])
	}
}

// ── OPT-255: PromptCacheHitPredictorV2 测试 ──

// TestPCHPV2_PredictNoHistory 验证无历史时预测不命中。
func TestPCHPV2_PredictNoHistory(t *testing.T) {
	p := NewPromptCacheHitPredictorV2(0.5)
	if p.Predict("k1") {
		t.Errorf("Predict(k1) with no history = true, want false")
	}
	stats := p.GetStats()
	if stats["totalPredictions"].(int) != 1 {
		t.Errorf("totalPredictions = %d, want 1", stats["totalPredictions"])
	}
}

// TestPCHPV2_PredictAfterHit 验证记录命中后预测命中。
func TestPCHPV2_PredictAfterHit(t *testing.T) {
	p := NewPromptCacheHitPredictorV2(0.5)
	p.RecordResult("k1", true) // patterns["k1"]=1
	if !p.Predict("k1") {
		t.Errorf("Predict(k1) after a recorded hit = false, want true")
	}
}

// TestPCHPV2_RecordResultAndAccuracy 验证记录结果与准确率计算。
func TestPCHPV2_RecordResultAndAccuracy(t *testing.T) {
	p := NewPromptCacheHitPredictorV2(0.5)
	p.Predict("x")             // total=1, predict false (no history)
	p.RecordResult("x", false) // predicted false == actual false -> correct=1
	if a := p.GetAccuracy(); a != 1.0 {
		t.Errorf("accuracy = %v, want 1.0", a)
	}
	p.Predict("x")            // total=2, predict false (patterns["x"] still 0)
	p.RecordResult("x", true) // predicted false != actual true -> no match, patterns["x"]=1
	if a := p.GetAccuracy(); a != 0.5 {
		t.Errorf("accuracy = %v, want 0.5", a)
	}
}

// TestPCHPV2_Stats 验证统计字段。
func TestPCHPV2_Stats(t *testing.T) {
	p := NewPromptCacheHitPredictorV2(0.5)
	p.RecordResult("a", true) // patterns["a"]=1
	p.Predict("a")            // total=1, predict true
	p.RecordResult("a", true) // predicted true == actual true -> correct=1, patterns["a"]=2
	stats := p.GetStats()
	if stats["patternCount"].(int) != 1 {
		t.Errorf("patternCount = %d, want 1", stats["patternCount"])
	}
	if stats["totalPredictions"].(int) != 1 {
		t.Errorf("totalPredictions = %d, want 1", stats["totalPredictions"])
	}
	if stats["correctPredictions"].(int) != 1 {
		t.Errorf("correctPredictions = %d, want 1", stats["correctPredictions"])
	}
	if stats["accuracy"].(float64) != 1.0 {
		t.Errorf("accuracy = %v, want 1.0", stats["accuracy"])
	}
	if stats["confidenceThreshold"].(float64) != 0.5 {
		t.Errorf("confidenceThreshold = %v, want 0.5", stats["confidenceThreshold"])
	}
}

// TestPCHPV2_Reset 验证 Reset 清空模式与计数但保留阈值。
func TestPCHPV2_Reset(t *testing.T) {
	p := NewPromptCacheHitPredictorV2(0.5)
	p.RecordResult("a", true)
	p.Predict("a")
	p.Reset()
	stats := p.GetStats()
	if stats["patternCount"].(int) != 0 {
		t.Errorf("after Reset patternCount = %d, want 0", stats["patternCount"])
	}
	if stats["totalPredictions"].(int) != 0 {
		t.Errorf("after Reset totalPredictions = %d, want 0", stats["totalPredictions"])
	}
	if stats["correctPredictions"].(int) != 0 {
		t.Errorf("after Reset correctPredictions = %d, want 0", stats["correctPredictions"])
	}
	if stats["confidenceThreshold"].(float64) != 0.5 {
		t.Errorf("after Reset confidenceThreshold = %v, want 0.5 (preserved)", stats["confidenceThreshold"])
	}
}
