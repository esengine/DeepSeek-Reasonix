package agent

import (
	"testing"
)

// ==============================================================
// OPT-196: TokenAwareAdmissionController / Token感知准入控制器
// ==============================================================

// TestTokenAwareAdmissionController_TryAdmitAndRelease 验证 TryAdmit + Release 正确管理并发槽
func TestTokenAwareAdmissionController_TryAdmitAndRelease(t *testing.T) {
	c := NewTokenAwareAdmissionController(3)
	if !c.TryAdmit() {
		t.Errorf("TryAdmit() 在并发槽未满时应返回 true，实际 false")
	}
	if !c.TryAdmit() {
		t.Errorf("第二次 TryAdmit() 应返回 true，实际 false")
	}
	if got := c.GetConcurrentCount(); got != 2 {
		t.Errorf("两次 TryAdmit 后并发数 = %d，期望 2", got)
	}
	c.Release()
	if got := c.GetConcurrentCount(); got != 1 {
		t.Errorf("Release 后并发数 = %d，期望 1", got)
	}
}

// TestTokenAwareAdmissionController_TryAdmitFullReturnsFalse 验证并发槽满时 TryAdit 返回 false
func TestTokenAwareAdmissionController_TryAdmitFullReturnsFalse(t *testing.T) {
	c := NewTokenAwareAdmissionController(2)
	c.TryAdmit()
	c.TryAdmit()
	// 已满，第三次应返回 false
	if c.TryAdmit() {
		t.Errorf("并发槽已满时 TryAdmit() 应返回 false，实际 true")
	}
	if got := c.GetConcurrentCount(); got != 2 {
		t.Errorf("被拒绝后并发数应不变 = %d，期望 2", got)
	}
}

// TestTokenAwareAdmissionController_GetConcurrentCount 验证 GetConcurrentCount 返回正确并发数
func TestTokenAwareAdmissionController_GetConcurrentCount(t *testing.T) {
	c := NewTokenAwareAdmissionController(5)
	if got := c.GetConcurrentCount(); got != 0 {
		t.Errorf("初始并发数 = %d，期望 0", got)
	}
	c.TryAdmit()
	c.TryAdmit()
	c.TryAdmit()
	if got := c.GetConcurrentCount(); got != 3 {
		t.Errorf("三次 TryAdmit 后并发数 = %d，期望 3", got)
	}
	c.Release()
	if got := c.GetConcurrentCount(); got != 2 {
		t.Errorf("Release 后并发数 = %d，期望 2", got)
	}
}

// TestTokenAwareAdmissionController_GetUtilization 验证 GetUtilization 返回正确利用率
func TestTokenAwareAdmissionController_GetUtilization(t *testing.T) {
	c := NewTokenAwareAdmissionController(4)
	c.TryAdmit()
	c.TryAdmit()
	util := c.GetUtilization()
	if util != 0.5 {
		t.Errorf("GetUtilization = %v，期望 0.5（2/4）", util)
	}
}

// TestTokenAwareAdmissionController_StatsAdmittedCount 验证 Stats 中 admittedCount 和 rejectedCount 正确
func TestTokenAwareAdmissionController_StatsAdmittedCount(t *testing.T) {
	c := NewTokenAwareAdmissionController(3)
	c.TryAdmit()
	c.TryAdmit()
	c.TryAdmit()
	c.TryAdmit() // 被拒绝
	stats := c.GetStats()
	admitted, ok := stats["admittedCount"].(int)
	if !ok {
		t.Errorf("stats[\"admittedCount\"] 类型断言失败")
	}
	if admitted != 3 {
		t.Errorf("admittedCount = %d，期望 3", admitted)
	}
	rejected, ok := stats["rejectedCount"].(int)
	if !ok {
		t.Errorf("stats[\"rejectedCount\"] 类型断言失败")
	}
	if rejected != 1 {
		t.Errorf("rejectedCount = %d，期望 1", rejected)
	}
}

// TestTokenAwareAdmissionController_Reset 验证 Reset 清空所有计数和并发状态
func TestTokenAwareAdmissionController_Reset(t *testing.T) {
	c := NewTokenAwareAdmissionController(3)
	c.TryAdmit()
	c.TryAdmit()
	c.Reset()
	if got := c.GetConcurrentCount(); got != 0 {
		t.Errorf("Reset后并发数 = %d，期望 0", got)
	}
	stats := c.GetStats()
	admitted, _ := stats["admittedCount"].(int)
	if admitted != 0 {
		t.Errorf("Reset后 admittedCount = %d，期望 0", admitted)
	}
}

// ==============================================================
// OPT-197: CacheCoherenceManager / 缓存一致性管理器
// ==============================================================

// TestCacheCoherenceManager_WriteAndRead 验证 Write + Read 读写操作
func TestCacheCoherenceManager_WriteAndRead(t *testing.T) {
	m := NewCacheCoherenceManager()
	m.Write("L1", "key1", "value1")
	val, ok := m.Read("L1", "key1")
	if !ok {
		t.Errorf("Read(L1, key1) 应存在，返回 false")
	}
	if val != "value1" {
		t.Errorf("Read(L1, key1) = %q，期望 \"value1\"", val)
	}
	// 不存在的 key
	_, ok = m.Read("L1", "nonexistent")
	if ok {
		t.Errorf("Read(L1, nonexistent) 不应存在，返回 true")
	}
	// 不存在的 level
	_, ok = m.Read("L2", "key1")
	if ok {
		t.Errorf("Read(L2, key1) 不应存在，返回 true")
	}
}

// TestCacheCoherenceManager_Invalidate 验证 Invalidate 失效所有级别的指定 key
func TestCacheCoherenceManager_Invalidate(t *testing.T) {
	m := NewCacheCoherenceManager()
	m.Write("L1", "key1", "value1")
	m.Write("L2", "key1", "value2")
	m.Write("L3", "key1", "value3")
	m.Invalidate("key1")
	// 所有级别中的 key1 都应被失效
	if _, ok := m.Read("L1", "key1"); ok {
		t.Errorf("Invalidate后 L1 中 key1 应不存在")
	}
	if _, ok := m.Read("L2", "key1"); ok {
		t.Errorf("Invalidate后 L2 中 key1 应不存在")
	}
	if _, ok := m.Read("L3", "key1"); ok {
		t.Errorf("Invalidate后 L3 中 key1 应不存在")
	}
	// 验证 invalidations 计数
	stats := m.GetStats()
	invalidations, _ := stats["invalidations"].(int)
	if invalidations != 3 {
		t.Errorf("invalidations = %d，期望 3", invalidations)
	}
}

// TestCacheCoherenceManager_Sync 验证 Sync 同步所有级别缓存
func TestCacheCoherenceManager_Sync(t *testing.T) {
	m := NewCacheCoherenceManager()
	m.Write("L1", "key1", "value1")
	m.Write("L2", "key2", "value2")
	// Sync 前 L1 只有 key1，L2 只有 key2
	count := m.Sync()
	// 合并后有 2 个唯一 key
	if count != 2 {
		t.Errorf("Sync 返回唯一 key 数 = %d，期望 2", count)
	}
	// Sync 后所有级别应有所有 key
	if val, ok := m.Read("L1", "key2"); !ok || val != "value2" {
		t.Errorf("Sync后 L1 中 key2 应为 \"value2\"，实际 val=%q ok=%v", val, ok)
	}
	if val, ok := m.Read("L2", "key1"); !ok || val != "value1" {
		t.Errorf("Sync后 L2 中 key1 应为 \"value1\"，实际 val=%q ok=%v", val, ok)
	}
}

// TestCacheCoherenceManager_StatsLevelCount 验证 Stats 中 levelCount 正确
func TestCacheCoherenceManager_StatsLevelCount(t *testing.T) {
	m := NewCacheCoherenceManager()
	m.Write("L1", "key1", "value1")
	m.Write("L2", "key2", "value2")
	m.Write("L3", "key3", "value3")
	stats := m.GetStats()
	levelCount, ok := stats["levelCount"].(int)
	if !ok {
		t.Errorf("stats[\"levelCount\"] 类型断言失败")
	}
	if levelCount != 3 {
		t.Errorf("levelCount = %d，期望 3", levelCount)
	}
}

// TestCacheCoherenceManager_Reset 验证 Reset 清空所有缓存级别和统计
func TestCacheCoherenceManager_Reset(t *testing.T) {
	m := NewCacheCoherenceManager()
	m.Write("L1", "key1", "value1")
	m.Write("L2", "key2", "value2")
	m.Sync()
	m.Reset()
	stats := m.GetStats()
	levelCount, _ := stats["levelCount"].(int)
	if levelCount != 0 {
		t.Errorf("Reset后 levelCount = %d，期望 0", levelCount)
	}
	if _, ok := m.Read("L1", "key1"); ok {
		t.Errorf("Reset后 L1 中 key1 应不存在")
	}
}

// ==============================================================
// OPT-198: ContextWindowPredictorV2 / 上下文窗口预测器 V2
// ==============================================================

// TestContextWindowPredictorV2_RecordAndPredict 验证 Record + Predict 移动平均预测
func TestContextWindowPredictorV2_RecordAndPredict(t *testing.T) {
	p := NewContextWindowPredictorV2(1000, 10)
	p.Record(100)
	p.Record(200)
	p.Record(300)
	// 移动平均 = (100 + 200 + 300) / 3 = 200
	pred := p.Predict()
	if pred != 200 {
		t.Errorf("Predict() = %d，期望 200（移动平均）", pred)
	}
}

// TestContextWindowPredictorV2_PredictNoHistory 验证无历史数据时 Predict 返回 0
func TestContextWindowPredictorV2_PredictNoHistory(t *testing.T) {
	p := NewContextWindowPredictorV2(1000, 10)
	pred := p.Predict()
	if pred != 0 {
		t.Errorf("无历史数据时 Predict() = %d，期望 0", pred)
	}
}

// TestContextWindowPredictorV2_MultipleRecordsAccuracy 验证多次 Record 后预测准确性
func TestContextWindowPredictorV2_MultipleRecordsAccuracy(t *testing.T) {
	p := NewContextWindowPredictorV2(1000, 10)
	p.Record(100)
	p.Record(200)
	// Predict -> 移动平均 [100, 200] = 150
	pred := p.Predict()
	if pred != 150 {
		t.Errorf("Predict() = %d，期望 150", pred)
	}
	// Record(150) -> |150 - 150| = 0 <= 阈值100，准确
	p.Record(150)
	// Predict -> 移动平均 [100, 200, 150] = 150
	pred2 := p.Predict()
	if pred2 != 150 {
		t.Errorf("第二次 Predict() = %d，期望 150", pred2)
	}
	// Record(150) -> |150 - 150| = 0 <= 阈值100，准确
	p.Record(150)
	acc := p.GetAccuracy()
	if acc != 1.0 {
		t.Errorf("两次准确预测后 GetAccuracy = %v，期望 1.0", acc)
	}
}

// TestContextWindowPredictorV2_GetAccuracy 验证 GetAccuracy 返回正确准确率
func TestContextWindowPredictorV2_GetAccuracy(t *testing.T) {
	p := NewContextWindowPredictorV2(1000, 10)
	// 无预测时准确率应为 0
	if acc := p.GetAccuracy(); acc != 0 {
		t.Errorf("无预测时 GetAccuracy = %v，期望 0", acc)
	}
	p.Record(100)
	p.Predict()   // prediction = 100, predictions = 1
	p.Record(100) // |100 - 100| = 0 <= 100，准确
	if acc := p.GetAccuracy(); acc != 1.0 {
		t.Errorf("一次准确预测后 GetAccuracy = %v，期望 1.0", acc)
	}
}

// TestContextWindowPredictorV2_StatsPredictions 验证 Stats 中 predictions 正确
func TestContextWindowPredictorV2_StatsPredictions(t *testing.T) {
	p := NewContextWindowPredictorV2(1000, 10)
	p.Record(100)
	p.Predict()
	p.Predict()
	stats := p.GetStats()
	predictions, ok := stats["predictions"].(int)
	if !ok {
		t.Errorf("stats[\"predictions\"] 类型断言失败")
	}
	if predictions != 2 {
		t.Errorf("predictions = %d，期望 2", predictions)
	}
	historySize, ok := stats["historySize"].(int)
	if !ok {
		t.Errorf("stats[\"historySize\"] 类型断言失败")
	}
	if historySize != 1 {
		t.Errorf("historySize = %d，期望 1", historySize)
	}
}

// TestContextWindowPredictorV2_Reset 验证 Reset 清空历史记录和统计
func TestContextWindowPredictorV2_Reset(t *testing.T) {
	p := NewContextWindowPredictorV2(1000, 10)
	p.Record(100)
	p.Record(200)
	p.Predict()
	p.Reset()
	stats := p.GetStats()
	predictions, _ := stats["predictions"].(int)
	if predictions != 0 {
		t.Errorf("Reset后 predictions = %d，期望 0", predictions)
	}
	historySize, _ := stats["historySize"].(int)
	if historySize != 0 {
		t.Errorf("Reset后 historySize = %d，期望 0", historySize)
	}
	if pred := p.Predict(); pred != 0 {
		t.Errorf("Reset后 Predict() = %d，期望 0（无历史）", pred)
	}
}

// ==============================================================
// OPT-199: TokenAwareCircuitBreaker / Token感知熔断器
// ==============================================================

// TestTokenAwareCircuitBreaker_AllowClosed 验证 closed 状态下 Allow 返回 true
func TestTokenAwareCircuitBreaker_AllowClosed(t *testing.T) {
	cb := NewTokenAwareCircuitBreaker(3, 5)
	if !cb.Allow() {
		t.Errorf("closed 状态 Allow() 应返回 true，实际 false")
	}
	if state := cb.GetState(); state != "closed" {
		t.Errorf("初始状态 GetState() = %q，期望 \"closed\"", state)
	}
}

// TestTokenAwareCircuitBreaker_RecordFailureTrips 验证 RecordFailure 超过阈值后熔断
func TestTokenAwareCircuitBreaker_RecordFailureTrips(t *testing.T) {
	cb := NewTokenAwareCircuitBreaker(3, 5)
	cb.RecordFailure(1)
	cb.RecordFailure(2)
	cb.RecordFailure(3) // failureCount=3 >= threshold=3，熔断
	if state := cb.GetState(); state != "open" {
		t.Errorf("超过阈值后 GetState() = %q，期望 \"open\"", state)
	}
}

// TestTokenAwareCircuitBreaker_AllowOpenReturnsFalse 验证熔断后 Allow 返回 false
func TestTokenAwareCircuitBreaker_AllowOpenReturnsFalse(t *testing.T) {
	cb := NewTokenAwareCircuitBreaker(2, 5)
	cb.RecordFailure(1)
	cb.RecordFailure(2) // 熔断，state=open
	if cb.Allow() {
		t.Errorf("open 状态 Allow() 应返回 false，实际 true")
	}
}

// TestTokenAwareCircuitBreaker_RecordSuccessRecovery 验证 RecordSuccess 从 half-open 恢复到 closed
func TestTokenAwareCircuitBreaker_RecordSuccessRecovery(t *testing.T) {
	cb := NewTokenAwareCircuitBreaker(2, 5)
	cb.RecordFailure(1)
	cb.RecordFailure(2) // 熔断，state=open
	// 模拟冷却期过后进入 half-open 状态（公开API中 RecordFailure 过冷却期后会转为 half-open
	// 但同时记录失败导致再次熔断，因此直接设置状态以测试 RecordSuccess 恢复逻辑）
	cb.mu.Lock()
	cb.state = "half-open"
	cb.mu.Unlock()
	cb.RecordSuccess()
	if state := cb.GetState(); state != "closed" {
		t.Errorf("half-open 状态 RecordSuccess 后 GetState() = %q，期望 \"closed\"", state)
	}
}

// TestTokenAwareCircuitBreaker_StatsTrips 验证 Stats 中 trips 正确
func TestTokenAwareCircuitBreaker_StatsTrips(t *testing.T) {
	cb := NewTokenAwareCircuitBreaker(2, 5)
	cb.RecordFailure(1)
	cb.RecordFailure(2) // 熔断，trips=1
	cb.RecordFailure(3) // open 状态，冷却期未过（3-2=1 < 5），直接返回
	stats := cb.GetStats()
	trips, ok := stats["trips"].(int)
	if !ok {
		t.Errorf("stats[\"trips\"] 类型断言失败")
	}
	if trips != 1 {
		t.Errorf("trips = %d，期望 1", trips)
	}
	failureCount, ok := stats["failureCount"].(int)
	if !ok {
		t.Errorf("stats[\"failureCount\"] 类型断言失败")
	}
	if failureCount != 2 {
		t.Errorf("failureCount = %d，期望 2", failureCount)
	}
}

// TestTokenAwareCircuitBreaker_Reset 验证 Reset 恢复初始状态
func TestTokenAwareCircuitBreaker_Reset(t *testing.T) {
	cb := NewTokenAwareCircuitBreaker(2, 5)
	cb.RecordFailure(1)
	cb.RecordFailure(2) // 熔断
	cb.RecordSuccess()
	cb.Reset()
	stats := cb.GetStats()
	trips, _ := stats["trips"].(int)
	if trips != 0 {
		t.Errorf("Reset后 trips = %d，期望 0", trips)
	}
	failureCount, _ := stats["failureCount"].(int)
	if failureCount != 0 {
		t.Errorf("Reset后 failureCount = %d，期望 0", failureCount)
	}
	if state := cb.GetState(); state != "closed" {
		t.Errorf("Reset后 GetState() = %q，期望 \"closed\"", state)
	}
}

// ==============================================================
// OPT-200: PromptTokenDistributor / 提示Token分配器
// ==============================================================

// TestPromptTokenDistributor_AllocateAndGet 验证 Allocate 分配和 GetAllocation 查询
func TestPromptTokenDistributor_AllocateAndGet(t *testing.T) {
	d := NewPromptTokenDistributor(1000)
	if !d.Allocate("comp1", 300) {
		t.Errorf("Allocate(comp1, 300) 在预算1000内应返回 true，实际 false")
	}
	if !d.Allocate("comp2", 500) {
		t.Errorf("Allocate(comp2, 500) 在预算1000内应返回 true，实际 false")
	}
	if got := d.GetAllocation("comp1"); got != 300 {
		t.Errorf("GetAllocation(comp1) = %d，期望 300", got)
	}
	if got := d.GetAllocation("comp2"); got != 500 {
		t.Errorf("GetAllocation(comp2) = %d，期望 500", got)
	}
	// 不存在的组件返回 0
	if got := d.GetAllocation("unknown"); got != 0 {
		t.Errorf("GetAllocation(unknown) = %d，期望 0", got)
	}
}

// TestPromptTokenDistributor_AllocateExceedBudget 验证超出 totalBudget 时 Allocate 返回 false
func TestPromptTokenDistributor_AllocateExceedBudget(t *testing.T) {
	d := NewPromptTokenDistributor(1000)
	d.Allocate("comp1", 600)
	d.Allocate("comp2", 300)
	// 600 + 300 + 200 = 1100 > 1000
	if d.Allocate("comp3", 200) {
		t.Errorf("Allocate(comp3, 200) 累计超出预算1000应返回 false，实际 true")
	}
	// 被拒绝后 comp3 不应有分配
	if got := d.GetAllocation("comp3"); got != 0 {
		t.Errorf("被拒绝后 GetAllocation(comp3) = %d，期望 0", got)
	}
}

// TestPromptTokenDistributor_Rebalance 验证 Rebalance 按比例重新平衡分配
func TestPromptTokenDistributor_Rebalance(t *testing.T) {
	d := NewPromptTokenDistributor(1000)
	d.Allocate("comp1", 100)
	d.Allocate("comp2", 100)
	// 总分配 = 200，预算 = 1000
	// Rebalance 后按比例缩放: scale = 1000 / 200 = 5
	// comp1 = 100 * 5 = 500, comp2 = 100 * 5 = 500
	d.Rebalance()
	if got := d.GetAllocation("comp1"); got != 500 {
		t.Errorf("Rebalance后 GetAllocation(comp1) = %d，期望 500", got)
	}
	if got := d.GetAllocation("comp2"); got != 500 {
		t.Errorf("Rebalance后 GetAllocation(comp2) = %d，期望 500", got)
	}
	total := d.GetTotalAllocated()
	if total != 1000 {
		t.Errorf("Rebalance后总分配 = %d，期望 1000", total)
	}
}

// TestPromptTokenDistributor_StatsComponentCount 验证 Stats 中 componentCount 正确
func TestPromptTokenDistributor_StatsComponentCount(t *testing.T) {
	d := NewPromptTokenDistributor(1000)
	d.Allocate("comp1", 100)
	d.Allocate("comp2", 200)
	d.Allocate("comp3", 300)
	stats := d.GetStats()
	componentCount, ok := stats["componentCount"].(int)
	if !ok {
		t.Errorf("stats[\"componentCount\"] 类型断言失败")
	}
	if componentCount != 3 {
		t.Errorf("componentCount = %d，期望 3", componentCount)
	}
	totalAllocated, ok := stats["totalAllocated"].(int)
	if !ok {
		t.Errorf("stats[\"totalAllocated\"] 类型断言失败")
	}
	if totalAllocated != 600 {
		t.Errorf("totalAllocated = %d，期望 600", totalAllocated)
	}
}

// TestPromptTokenDistributor_Reset 验证 Reset 清空所有分配和统计
func TestPromptTokenDistributor_Reset(t *testing.T) {
	d := NewPromptTokenDistributor(1000)
	d.Allocate("comp1", 100)
	d.Allocate("comp2", 200)
	d.Rebalance()
	d.Reset()
	stats := d.GetStats()
	componentCount, _ := stats["componentCount"].(int)
	if componentCount != 0 {
		t.Errorf("Reset后 componentCount = %d，期望 0", componentCount)
	}
	if got := d.GetAllocation("comp1"); got != 0 {
		t.Errorf("Reset后 GetAllocation(comp1) = %d，期望 0", got)
	}
}
