package agent

import (
	"math"
	"testing"
)

// ── 辅助函数 ──

// optTestFloatEqual 判断两个浮点数是否近似相等（容差 1e-9）。
func optTestFloatEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// optTestContainsString 检查字符串切片是否包含指定字符串。
func optTestContainsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ════════════════════════════════════════════════════════════════
// OPT-211: TokenAwarePriorityQueueV3 测试
// ════════════════════════════════════════════════════════════════

// TestPQ3V3_EnqueueDequeuePriority: Enqueue + Dequeue 验证优先级最高的先出。
func TestPQ3V3_EnqueueDequeuePriority(t *testing.T) {
	q := NewTokenAwarePriorityQueueV3(10)
	q.Enqueue("a", 1, 100)
	q.Enqueue("b", 5, 200)
	q.Enqueue("c", 3, 150)

	// 优先级最高的 b (priority=5) 应先出
	item, ok := q.Dequeue()
	if !ok {
		t.Errorf("first Dequeue returned ok=false, expected true")
	}
	if item.ID != "b" {
		t.Errorf("expected first Dequeue ID=b (priority 5), got %q", item.ID)
	}
	if item.Priority != 5 {
		t.Errorf("expected first Dequeue priority=5, got %d", item.Priority)
	}

	// 其次是 c (priority=3)
	item, ok = q.Dequeue()
	if !ok {
		t.Errorf("second Dequeue returned ok=false, expected true")
	}
	if item.ID != "c" {
		t.Errorf("expected second Dequeue ID=c (priority 3), got %q", item.ID)
	}

	// 最后是 a (priority=1)
	item, ok = q.Dequeue()
	if !ok {
		t.Errorf("third Dequeue returned ok=false, expected true")
	}
	if item.ID != "a" {
		t.Errorf("expected third Dequeue ID=a (priority 1), got %q", item.ID)
	}

	// 队列已空
	_, ok = q.Dequeue()
	if ok {
		t.Errorf("expected Dequeue on empty queue to return ok=false")
	}
}

// TestPQ3V3_EnqueueExceedsMaxItems: 超过 maxItems 时 Enqueue 返回 false。
func TestPQ3V3_EnqueueExceedsMaxItems(t *testing.T) {
	q := NewTokenAwarePriorityQueueV3(2)
	if !q.Enqueue("a", 1, 100) {
		t.Errorf("Enqueue a should return true, got false")
	}
	if !q.Enqueue("b", 2, 100) {
		t.Errorf("Enqueue b should return true, got false")
	}
	// 队列已满，第三次应返回 false
	if q.Enqueue("c", 3, 100) {
		t.Errorf("Enqueue c should return false (queue full at maxItems=2), got true")
	}
	if q.Count() != 2 {
		t.Errorf("expected Count=2 after rejected Enqueue, got %d", q.Count())
	}
}

// TestPQ3V3_PeekNoRemove: Peek 查看优先级最高的元素但不移除。
func TestPQ3V3_PeekNoRemove(t *testing.T) {
	q := NewTokenAwarePriorityQueueV3(10)
	q.Enqueue("a", 1, 100)
	q.Enqueue("b", 5, 200)
	q.Enqueue("c", 3, 150)

	item, ok := q.Peek()
	if !ok {
		t.Errorf("Peek returned ok=false, expected true")
	}
	if item.ID != "b" {
		t.Errorf("expected Peek ID=b (highest priority), got %q", item.ID)
	}
	// Peek 不应移除元素
	if q.Count() != 3 {
		t.Errorf("expected Count=3 after Peek (element not removed), got %d", q.Count())
	}
	// 再次 Peek 应返回相同元素
	item2, ok := q.Peek()
	if !ok {
		t.Errorf("second Peek returned ok=false, expected true")
	}
	if item2.ID != "b" {
		t.Errorf("expected second Peek ID=b, got %q", item2.ID)
	}
}

// TestPQ3V3_Count: Count 验证当前队列元素数量随操作变化。
func TestPQ3V3_Count(t *testing.T) {
	q := NewTokenAwarePriorityQueueV3(10)
	if q.Count() != 0 {
		t.Errorf("expected Count=0 on empty queue, got %d", q.Count())
	}
	q.Enqueue("a", 1, 100)
	q.Enqueue("b", 2, 100)
	q.Enqueue("c", 3, 100)
	if q.Count() != 3 {
		t.Errorf("expected Count=3 after 3 Enqueue, got %d", q.Count())
	}
	q.Dequeue()
	if q.Count() != 2 {
		t.Errorf("expected Count=2 after 1 Dequeue, got %d", q.Count())
	}
}

// TestPQ3V3_StatsTotalEnqueued: Stats 验证 totalEnqueued 统计。
func TestPQ3V3_StatsTotalEnqueued(t *testing.T) {
	q := NewTokenAwarePriorityQueueV3(10)
	q.Enqueue("a", 1, 100)
	q.Enqueue("b", 2, 100)
	q.Enqueue("c", 3, 100)

	stats := q.GetStats()
	if stats["totalEnqueued"].(int) != 3 {
		t.Errorf("expected totalEnqueued=3, got %v", stats["totalEnqueued"])
	}
	// Dequeue 不应减少 totalEnqueued
	q.Dequeue()
	stats = q.GetStats()
	if stats["totalEnqueued"].(int) != 3 {
		t.Errorf("totalEnqueued should remain 3 after Dequeue, got %v", stats["totalEnqueued"])
	}
	if stats["totalDequeued"].(int) != 1 {
		t.Errorf("expected totalDequeued=1, got %v", stats["totalDequeued"])
	}
}

// TestPQ3V3_Reset: Reset 重置队列为初始状态。
func TestPQ3V3_Reset(t *testing.T) {
	q := NewTokenAwarePriorityQueueV3(10)
	q.Enqueue("a", 1, 100)
	q.Enqueue("b", 2, 100)
	q.Dequeue()
	q.Reset()

	if q.Count() != 0 {
		t.Errorf("expected Count=0 after Reset, got %d", q.Count())
	}
	stats := q.GetStats()
	if stats["totalEnqueued"].(int) != 0 {
		t.Errorf("expected totalEnqueued=0 after Reset, got %v", stats["totalEnqueued"])
	}
	if stats["totalDequeued"].(int) != 0 {
		t.Errorf("expected totalDequeued=0 after Reset, got %v", stats["totalDequeued"])
	}
}

// ════════════════════════════════════════════════════════════════
// OPT-212: CacheInvalidationCascade 测试
// ════════════════════════════════════════════════════════════════

// TestCIC_CascadeInvalidation: AddDependency + Invalidate 验证级联失效。
func TestCIC_CascadeInvalidation(t *testing.T) {
	c := NewCacheInvalidationCascade(5)
	c.AddDependency("A", "B") // B 依赖于 A
	c.AddDependency("B", "C") // C 依赖于 B

	invalidated := c.Invalidate("A")
	if len(invalidated) != 3 {
		t.Errorf("expected 3 invalidated keys (A->B->C), got %d: %v", len(invalidated), invalidated)
	}
	if !optTestContainsString(invalidated, "A") {
		t.Errorf("invalidated list should contain A, got %v", invalidated)
	}
	if !optTestContainsString(invalidated, "B") {
		t.Errorf("invalidated list should contain B (cascade dep), got %v", invalidated)
	}
	if !optTestContainsString(invalidated, "C") {
		t.Errorf("invalidated list should contain C (cascade dep), got %v", invalidated)
	}
}

// TestCIC_NoDependenciesOnlySelf: 无依赖时只失效自身。
func TestCIC_NoDependenciesOnlySelf(t *testing.T) {
	c := NewCacheInvalidationCascade(5)
	// X 没有任何依赖关系
	invalidated := c.Invalidate("X")
	if len(invalidated) != 1 {
		t.Errorf("expected 1 invalidated key (self only), got %d: %v", len(invalidated), invalidated)
	}
	if len(invalidated) > 0 && invalidated[0] != "X" {
		t.Errorf("expected invalidated[0]=X, got %q", invalidated[0])
	}
}

// TestCIC_GetDependencies: GetDependencies 返回指定 key 的直接依赖列表。
func TestCIC_GetDependencies(t *testing.T) {
	c := NewCacheInvalidationCascade(5)
	c.AddDependency("A", "B")
	c.AddDependency("A", "C")

	deps := c.GetDependencies("A")
	if len(deps) != 2 {
		t.Errorf("expected 2 dependencies for A, got %d: %v", len(deps), deps)
	}
	if !optTestContainsString(deps, "B") {
		t.Errorf("dependencies of A should contain B, got %v", deps)
	}
	if !optTestContainsString(deps, "C") {
		t.Errorf("dependencies of A should contain C, got %v", deps)
	}
	// 不存在的 key 返回空列表
	depsNone := c.GetDependencies("nonexistent")
	if len(depsNone) != 0 {
		t.Errorf("expected 0 dependencies for nonexistent key, got %d: %v", len(depsNone), depsNone)
	}
}

// TestCIC_StatsCascadeCount: Stats 验证 cascadeCount 统计。
func TestCIC_StatsCascadeCount(t *testing.T) {
	c := NewCacheInvalidationCascade(5)
	c.AddDependency("A", "B")
	c.Invalidate("A") // 失效 [A, B] -> cascadeCount=1, totalPropagated += 2
	c.Invalidate("B") // 失效 [B]    -> cascadeCount=2, totalPropagated += 1

	stats := c.GetStats()
	if stats["cascadeCount"].(int) != 2 {
		t.Errorf("expected cascadeCount=2, got %v", stats["cascadeCount"])
	}
	if stats["totalPropagated"].(int) != 3 {
		t.Errorf("expected totalPropagated=3, got %v", stats["totalPropagated"])
	}
}

// TestCIC_Reset: Reset 重置级联器为初始状态。
func TestCIC_Reset(t *testing.T) {
	c := NewCacheInvalidationCascade(5)
	c.AddDependency("A", "B")
	c.Invalidate("A")
	c.Reset()

	stats := c.GetStats()
	if stats["cascadeCount"].(int) != 0 {
		t.Errorf("expected cascadeCount=0 after Reset, got %v", stats["cascadeCount"])
	}
	if stats["totalPropagated"].(int) != 0 {
		t.Errorf("expected totalPropagated=0 after Reset, got %v", stats["totalPropagated"])
	}
	deps := c.GetDependencies("A")
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies after Reset, got %d: %v", len(deps), deps)
	}
}

// ════════════════════════════════════════════════════════════════
// OPT-213: ContextBudgetAllocator 测试
// ════════════════════════════════════════════════════════════════

// TestCBA_Allocate: Allocate 验证 token 预算分配。
func TestCBA_Allocate(t *testing.T) {
	a := NewContextBudgetAllocator(1000)
	if !a.Allocate("system", 200) {
		t.Errorf("Allocate system 200 should return true")
	}
	if a.GetAllocation("system") != 200 {
		t.Errorf("expected GetAllocation(system)=200, got %d", a.GetAllocation("system"))
	}
	// 覆盖已有 section 的分配值
	if !a.Allocate("system", 300) {
		t.Errorf("Allocate system 300 (override) should return true")
	}
	if a.GetAllocation("system") != 300 {
		t.Errorf("expected GetAllocation(system)=300 after override, got %d", a.GetAllocation("system"))
	}
}

// TestCBA_AllocateExceedsBudget: 超出 totalBudget 时 Allocate 返回 false。
func TestCBA_AllocateExceedsBudget(t *testing.T) {
	a := NewContextBudgetAllocator(1000)
	if !a.Allocate("a", 600) {
		t.Errorf("Allocate a 600 should return true")
	}
	// 600 + 500 = 1100 > 1000
	if a.Allocate("b", 500) {
		t.Errorf("Allocate b 500 should return false (exceeds totalBudget=1000)")
	}
	if a.GetAllocation("b") != 0 {
		t.Errorf("expected GetAllocation(b)=0 (rejected), got %d", a.GetAllocation("b"))
	}
	if a.GetTotalAllocated() != 600 {
		t.Errorf("expected totalAllocated=600, got %d", a.GetTotalAllocated())
	}
}

// TestCBA_GetAllocation: GetAllocation 查询指定 section 的当前分配。
func TestCBA_GetAllocation(t *testing.T) {
	a := NewContextBudgetAllocator(1000)
	a.Allocate("x", 300)
	if a.GetAllocation("x") != 300 {
		t.Errorf("expected GetAllocation(x)=300, got %d", a.GetAllocation("x"))
	}
	// 未分配的 section 返回 0
	if a.GetAllocation("nonexistent") != 0 {
		t.Errorf("expected GetAllocation(nonexistent)=0, got %d", a.GetAllocation("nonexistent"))
	}
}

// TestCBA_Rebalance: Rebalance 按比例重新平衡所有 section 的分配。
func TestCBA_Rebalance(t *testing.T) {
	a := NewContextBudgetAllocator(1000)
	a.Allocate("a", 100) // 当前比例 1/4
	a.Allocate("b", 300) // 当前比例 3/4
	// totalAllocated = 400

	a.Rebalance()
	// a: int(100/400 * 1000) = 250
	// b: int(300/400 * 1000) = 750
	if a.GetAllocation("a") != 250 {
		t.Errorf("expected GetAllocation(a)=250 after Rebalance, got %d", a.GetAllocation("a"))
	}
	if a.GetAllocation("b") != 750 {
		t.Errorf("expected GetAllocation(b)=750 after Rebalance, got %d", a.GetAllocation("b"))
	}
	if a.GetTotalAllocated() != 1000 {
		t.Errorf("expected totalAllocated=1000 after Rebalance, got %d", a.GetTotalAllocated())
	}
}

// TestCBA_StatsSectionCount: Stats 验证 sectionCount 统计。
func TestCBA_StatsSectionCount(t *testing.T) {
	a := NewContextBudgetAllocator(1000)
	a.Allocate("a", 100)
	a.Allocate("b", 200)
	a.Allocate("c", 300)

	stats := a.GetStats()
	if stats["sectionCount"].(int) != 3 {
		t.Errorf("expected sectionCount=3, got %v", stats["sectionCount"])
	}
	if stats["totalAllocated"].(int) != 600 {
		t.Errorf("expected totalAllocated=600, got %v", stats["totalAllocated"])
	}
}

// TestCBA_Reset: Reset 重置分配器为初始状态。
func TestCBA_Reset(t *testing.T) {
	a := NewContextBudgetAllocator(1000)
	a.Allocate("a", 100)
	a.Allocate("b", 200)
	a.Rebalance()
	a.Reset()

	stats := a.GetStats()
	if stats["sectionCount"].(int) != 0 {
		t.Errorf("expected sectionCount=0 after Reset, got %v", stats["sectionCount"])
	}
	if stats["allocatedCount"].(int) != 0 {
		t.Errorf("expected allocatedCount=0 after Reset, got %v", stats["allocatedCount"])
	}
	if a.GetTotalAllocated() != 0 {
		t.Errorf("expected totalAllocated=0 after Reset, got %d", a.GetTotalAllocated())
	}
}

// ════════════════════════════════════════════════════════════════
// OPT-214: TokenAwarePartitioner 测试
// ════════════════════════════════════════════════════════════════

// TestTAP_PartitionMinLoad: Partition 验证分配到负载最低的分区。
func TestTAP_PartitionMinLoad(t *testing.T) {
	p := NewTokenAwarePartitioner(2, 100)
	// 初始两个分区负载均为 0，第一次分配到其中一个
	id1 := p.Partition("k1", 10)
	if id1 == "" {
		t.Errorf("Partition should return non-empty partition ID, got empty")
	}
	// 第一个分区现在负载为 10，第二个仍为 0，第二次应分配到负载最低（0）的分区
	id2 := p.Partition("k2", 10)
	if id2 == "" {
		t.Errorf("second Partition should return non-empty partition ID, got empty")
	}
	if id1 == id2 {
		t.Errorf("second Partition should go to lower-load partition, both returned %q", id1)
	}
	// 两个分区负载都应为 10
	if p.GetPartitionLoad(id1) != 10 {
		t.Errorf("expected partition %q load=10, got %d", id1, p.GetPartitionLoad(id1))
	}
	if p.GetPartitionLoad(id2) != 10 {
		t.Errorf("expected partition %q load=10, got %d", id2, p.GetPartitionLoad(id2))
	}
}

// TestTAP_GetPartitionLoad: GetPartitionLoad 查询指定分区的当前负载。
func TestTAP_GetPartitionLoad(t *testing.T) {
	p := NewTokenAwarePartitioner(3, 100)
	id := p.Partition("k1", 50)
	if p.GetPartitionLoad(id) != 50 {
		t.Errorf("expected partition %q load=50, got %d", id, p.GetPartitionLoad(id))
	}
	// 不存在的分区返回 0
	if p.GetPartitionLoad("nonexistent") != 0 {
		t.Errorf("expected GetPartitionLoad(nonexistent)=0, got %d", p.GetPartitionLoad("nonexistent"))
	}
}

// TestTAP_BalancedAfterMultiple: 多次 Partition 后负载均衡。
func TestTAP_BalancedAfterMultiple(t *testing.T) {
	p := NewTokenAwarePartitioner(3, 100)
	// 6 次分区，每次 10 token，3 个分区 -> 每个分区恰好 20
	for i := 0; i < 6; i++ {
		p.Partition("k", 10)
	}
	// 所有分区负载应均衡为 20
	for _, pid := range []string{"p0", "p1", "p2"} {
		if p.GetPartitionLoad(pid) != 20 {
			t.Errorf("expected partition %q load=20 (balanced), got %d", pid, p.GetPartitionLoad(pid))
		}
	}
}

// TestTAP_Rebalance: Rebalance 将总负载平均分配到所有分区。
func TestTAP_Rebalance(t *testing.T) {
	p := NewTokenAwarePartitioner(2, 100)
	// 制造不均衡：一次 10，一次 90
	id1 := p.Partition("k1", 10)
	id2 := p.Partition("k2", 90)
	if id1 == id2 {
		t.Errorf("expected different partitions for k1 and k2, both returned %q", id1)
	}
	// Rebalance 前负载不均
	if p.GetPartitionLoad(id1) != 10 {
		t.Errorf("expected partition %q load=10 before Rebalance, got %d", id1, p.GetPartitionLoad(id1))
	}
	if p.GetPartitionLoad(id2) != 90 {
		t.Errorf("expected partition %q load=90 before Rebalance, got %d", id2, p.GetPartitionLoad(id2))
	}
	p.Rebalance()
	// Rebalance 后总负载 100 / 2 = 50，两个分区均变为 50
	if p.GetPartitionLoad(id1) != 50 {
		t.Errorf("expected partition %q load=50 after Rebalance, got %d", id1, p.GetPartitionLoad(id1))
	}
	if p.GetPartitionLoad(id2) != 50 {
		t.Errorf("expected partition %q load=50 after Rebalance, got %d", id2, p.GetPartitionLoad(id2))
	}
}

// TestTAP_StatsTotalPartitioned: Stats 验证 totalPartitioned 统计。
func TestTAP_StatsTotalPartitioned(t *testing.T) {
	p := NewTokenAwarePartitioner(3, 100)
	p.Partition("k1", 10)
	p.Partition("k2", 20)
	p.Partition("k3", 30)

	stats := p.GetStats()
	if stats["totalPartitioned"].(int) != 3 {
		t.Errorf("expected totalPartitioned=3, got %v", stats["totalPartitioned"])
	}
	if stats["partitionCount"].(int) != 3 {
		t.Errorf("expected partitionCount=3, got %v", stats["partitionCount"])
	}
}

// TestTAP_Reset: Reset 重置分区器为初始状态。
func TestTAP_Reset(t *testing.T) {
	p := NewTokenAwarePartitioner(3, 100)
	p.Partition("k1", 50)
	p.Partition("k2", 60)
	p.Reset()

	stats := p.GetStats()
	if stats["totalPartitioned"].(int) != 0 {
		t.Errorf("expected totalPartitioned=0 after Reset, got %v", stats["totalPartitioned"])
	}
	for _, pid := range []string{"p0", "p1", "p2"} {
		if p.GetPartitionLoad(pid) != 0 {
			t.Errorf("expected partition %q load=0 after Reset, got %d", pid, p.GetPartitionLoad(pid))
		}
	}
}

// ════════════════════════════════════════════════════════════════
// OPT-215: PromptCacheHitAnalyzer 测试
// ════════════════════════════════════════════════════════════════

// TestPCHA_RecordHitGetHitRate: RecordHit + GetHitRate 验证命中率计算。
func TestPCHA_RecordHitGetHitRate(t *testing.T) {
	a := NewPromptCacheHitAnalyzer()
	a.RecordHit("patternA", true)
	a.RecordHit("patternA", false)
	a.RecordHit("patternA", true)

	rate := a.GetHitRate("patternA")
	expected := 2.0 / 3.0
	if !optTestFloatEqual(rate, expected) {
		t.Errorf("expected GetHitRate(patternA)=%.6f, got %.6f", expected, rate)
	}
	// 未记录的模式返回 0
	if a.GetHitRate("nonexistent") != 0 {
		t.Errorf("expected GetHitRate(nonexistent)=0, got %.6f", a.GetHitRate("nonexistent"))
	}
}

// TestPCHA_GetOverallHitRate: GetOverallHitRate 验证全局命中率。
func TestPCHA_GetOverallHitRate(t *testing.T) {
	a := NewPromptCacheHitAnalyzer()
	a.RecordHit("a", true)
	a.RecordHit("a", false)
	a.RecordHit("b", true)
	a.RecordHit("b", true)

	rate := a.GetOverallHitRate()
	// 3 hits / 4 total = 0.75
	if !optTestFloatEqual(rate, 0.75) {
		t.Errorf("expected GetOverallHitRate=0.75, got %.6f", rate)
	}
}

// TestPCHA_GetBestPattern: GetBestPattern 返回命中率最高的模式。
func TestPCHA_GetBestPattern(t *testing.T) {
	a := NewPromptCacheHitAnalyzer()
	a.RecordHit("a", true)
	a.RecordHit("a", false) // rate = 0.5
	a.RecordHit("b", true)
	a.RecordHit("b", true) // rate = 1.0

	best := a.GetBestPattern()
	if best != "b" {
		t.Errorf("expected GetBestPattern=b (rate 1.0), got %q", best)
	}
}

// TestPCHA_StatsTotalHits: Stats 验证 totalHits 统计。
func TestPCHA_StatsTotalHits(t *testing.T) {
	a := NewPromptCacheHitAnalyzer()
	a.RecordHit("a", true)
	a.RecordHit("a", true)
	a.RecordHit("a", true)
	a.RecordHit("a", false)

	stats := a.GetStats()
	if stats["totalHits"].(int) != 3 {
		t.Errorf("expected totalHits=3, got %v", stats["totalHits"])
	}
	if stats["totalMisses"].(int) != 1 {
		t.Errorf("expected totalMisses=1, got %v", stats["totalMisses"])
	}
	if stats["patterns"].(int) != 1 {
		t.Errorf("expected patterns=1, got %v", stats["patterns"])
	}
}

// TestPCHA_Reset: Reset 重置分析器为初始状态。
func TestPCHA_Reset(t *testing.T) {
	a := NewPromptCacheHitAnalyzer()
	a.RecordHit("a", true)
	a.RecordHit("a", false)
	a.RecordHit("b", true)
	a.Reset()

	stats := a.GetStats()
	if stats["totalHits"].(int) != 0 {
		t.Errorf("expected totalHits=0 after Reset, got %v", stats["totalHits"])
	}
	if stats["totalMisses"].(int) != 0 {
		t.Errorf("expected totalMisses=0 after Reset, got %v", stats["totalMisses"])
	}
	if stats["patterns"].(int) != 0 {
		t.Errorf("expected patterns=0 after Reset, got %v", stats["patterns"])
	}
	if a.GetOverallHitRate() != 0 {
		t.Errorf("expected GetOverallHitRate=0 after Reset, got %.6f", a.GetOverallHitRate())
	}
	if a.GetBestPattern() != "" {
		t.Errorf("expected GetBestPattern='' after Reset, got %q", a.GetBestPattern())
	}
}
