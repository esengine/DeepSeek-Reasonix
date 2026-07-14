package agent

import (
	"testing"
)

// ── OPT-141: TokenAwareGrouper ──

func TestTokenAwareGrouper_Group(t *testing.T) {
	g := NewTokenAwareGrouper(10)
	items := []GrouperItem{
		{Content: "a", Tokens: 3},
		{Content: "b", Tokens: 4},
		{Content: "c", Tokens: 5},
		{Content: "d", Tokens: 2},
	}
	groups := g.Group(items)
	if len(groups) < 2 {
		t.Errorf("should have at least 2 groups, got %d", len(groups))
	}
}

func TestTokenAwareGrouper_SingleGroup(t *testing.T) {
	g := NewTokenAwareGrouper(100)
	items := []GrouperItem{
		{Content: "a", Tokens: 3},
		{Content: "b", Tokens: 4},
	}
	groups := g.Group(items)
	if len(groups) != 1 {
		t.Errorf("should have 1 group, got %d", len(groups))
	}
}

func TestTokenAwareGrouper_LargeItem(t *testing.T) {
	g := NewTokenAwareGrouper(2)
	items := []GrouperItem{
		{Content: "big", Tokens: 10},
	}
	groups := g.Group(items)
	if len(groups) != 1 {
		t.Errorf("should still group large item alone, got %d groups", len(groups))
	}
}

func TestTokenAwareGrouper_Stats(t *testing.T) {
	g := NewTokenAwareGrouper(10)
	g.Group([]GrouperItem{{Content: "a", Tokens: 3}, {Content: "b", Tokens: 4}})
	stats := g.GetStats()
	if stats["totalMessages"].(int) != 2 {
		t.Errorf("totalMessages should be 2, got %v", stats["totalMessages"])
	}
}

func TestTokenAwareGrouper_Reset(t *testing.T) {
	g := NewTokenAwareGrouper(10)
	g.Group([]GrouperItem{{Content: "a", Tokens: 3}})
	g.Reset()
	stats := g.GetStats()
	if stats["totalGroups"].(int) != 0 {
		t.Errorf("totalGroups should be 0 after reset")
	}
}

// ── OPT-142: ContextPriorityQueue ──

func TestContextPriorityQueue_EnqueueDequeue(t *testing.T) {
	q := NewContextPriorityQueue(10)
	q.Enqueue(PriorityItem{Content: "low", Priority: 1, Tokens: 10})
	q.Enqueue(PriorityItem{Content: "high", Priority: 10, Tokens: 10})
	q.Enqueue(PriorityItem{Content: "med", Priority: 5, Tokens: 10})

	item, ok := q.Dequeue()
	if !ok {
		t.Fatal("should dequeue an item")
	}
	if item.Priority != 10 {
		t.Errorf("should dequeue highest priority (10), got %d", item.Priority)
	}
}

func TestContextPriorityQueue_MaxSize(t *testing.T) {
	q := NewContextPriorityQueue(2)
	q.Enqueue(PriorityItem{Content: "a", Priority: 1, Tokens: 1})
	q.Enqueue(PriorityItem{Content: "b", Priority: 2, Tokens: 1})
	if q.Enqueue(PriorityItem{Content: "c", Priority: 3, Tokens: 1}) {
		t.Errorf("should reject when full")
	}
}

func TestContextPriorityQueue_Peek(t *testing.T) {
	q := NewContextPriorityQueue(10)
	q.Enqueue(PriorityItem{Content: "a", Priority: 5, Tokens: 1})
	item, ok := q.Peek()
	if !ok {
		t.Fatal("should peek an item")
	}
	if item.Priority != 5 {
		t.Errorf("peek should return priority 5, got %d", item.Priority)
	}
	// Queue should still have the item
	item2, ok := q.Dequeue()
	if !ok || item2.Priority != 5 {
		t.Errorf("dequeue should still work after peek")
	}
}

func TestContextPriorityQueue_Empty(t *testing.T) {
	q := NewContextPriorityQueue(10)
	_, ok := q.Dequeue()
	if ok {
		t.Errorf("should not dequeue from empty queue")
	}
}

func TestContextPriorityQueue_Stats(t *testing.T) {
	q := NewContextPriorityQueue(10)
	q.Enqueue(PriorityItem{Content: "a", Priority: 1, Tokens: 1})
	q.Enqueue(PriorityItem{Content: "b", Priority: 2, Tokens: 1})
	q.Dequeue()
	stats := q.GetStats()
	if stats["totalEnqueued"].(int) != 2 {
		t.Errorf("totalEnqueued should be 2, got %v", stats["totalEnqueued"])
	}
	if stats["totalDequeued"].(int) != 1 {
		t.Errorf("totalDequeued should be 1, got %v", stats["totalDequeued"])
	}
}

func TestContextPriorityQueue_Reset(t *testing.T) {
	q := NewContextPriorityQueue(10)
	q.Enqueue(PriorityItem{Content: "a", Priority: 1, Tokens: 1})
	q.Reset()
	stats := q.GetStats()
	if stats["totalEnqueued"].(int) != 0 {
		t.Errorf("totalEnqueued should be 0 after reset")
	}
}

// ── OPT-143: TokenSavingsCalculator ──

func TestTokenSavingsCalculator_RecordAndQuery(t *testing.T) {
	c := NewTokenSavingsCalculator()
	c.RecordSavings("cache_hit", 1000, 300)
	c.RecordSavings("dedup", 500, 100)
	c.RecordSavings("cache_hit", 800, 200)

	saved := c.GetSavingsByStrategy("cache_hit")
	if saved != 500 { // 300 + 200
		t.Errorf("cache_hit savings should be 500, got %d", saved)
	}
}

func TestTokenSavingsCalculator_TotalSavings(t *testing.T) {
	c := NewTokenSavingsCalculator()
	c.RecordSavings("a", 1000, 300)
	c.RecordSavings("b", 500, 200)
	total := c.GetTotalSavings()
	if total != 500 {
		t.Errorf("total savings should be 500, got %d", total)
	}
}

func TestTokenSavingsCalculator_SavingsRate(t *testing.T) {
	c := NewTokenSavingsCalculator()
	c.RecordSavings("a", 1000, 250)
	rate := c.GetSavingsRate()
	if rate != 0.25 {
		t.Errorf("savings rate should be 0.25, got %f", rate)
	}
}

func TestTokenSavingsCalculator_Stats(t *testing.T) {
	c := NewTokenSavingsCalculator()
	c.RecordSavings("a", 1000, 300)
	c.RecordSavings("b", 500, 200)
	stats := c.GetStats()
	if stats["strategyCount"].(int) != 2 {
		t.Errorf("strategyCount should be 2, got %v", stats["strategyCount"])
	}
}

func TestTokenSavingsCalculator_Reset(t *testing.T) {
	c := NewTokenSavingsCalculator()
	c.RecordSavings("a", 1000, 300)
	c.Reset()
	if c.GetTotalSavings() != 0 {
		t.Errorf("total savings should be 0 after reset")
	}
}

// ── OPT-144: CacheAdmissionController ──

func TestCacheAdmissionController_Admit(t *testing.T) {
	c := NewCacheAdmissionController(50)
	if !c.RequestAdmission("key1", 100, 5) {
		t.Errorf("should admit with savings > min and freq >= 2")
	}
}

func TestCacheAdmissionController_RejectLowSavings(t *testing.T) {
	c := NewCacheAdmissionController(50)
	if c.RequestAdmission("key1", 30, 5) {
		t.Errorf("should reject with savings < min")
	}
}

func TestCacheAdmissionController_RejectLowFrequency(t *testing.T) {
	c := NewCacheAdmissionController(50)
	if c.RequestAdmission("key1", 100, 1) {
		t.Errorf("should reject with frequency < 2")
	}
}

func TestCacheAdmissionController_Rule(t *testing.T) {
	c := NewCacheAdmissionController(50)
	c.AddRule("special", true)
	if !c.RequestAdmission("special", 10, 1) {
		t.Errorf("should admit with explicit rule")
	}
	c.AddRule("blocked", false)
	if c.RequestAdmission("blocked", 1000, 100) {
		t.Errorf("should reject with explicit block rule")
	}
}

func TestCacheAdmissionController_Stats(t *testing.T) {
	c := NewCacheAdmissionController(50)
	c.RequestAdmission("a", 100, 5)  // admit
	c.RequestAdmission("b", 10, 1)   // reject
	stats := c.GetStats()
	if stats["totalRequests"].(int) != 2 {
		t.Errorf("totalRequests should be 2, got %v", stats["totalRequests"])
	}
	if stats["totalAdmitted"].(int) != 1 {
		t.Errorf("totalAdmitted should be 1, got %v", stats["totalAdmitted"])
	}
}

func TestCacheAdmissionController_Reset(t *testing.T) {
	c := NewCacheAdmissionController(50)
	c.RequestAdmission("a", 100, 5)
	c.Reset()
	stats := c.GetStats()
	if stats["totalRequests"].(int) != 0 {
		t.Errorf("totalRequests should be 0 after reset")
	}
}

// ── OPT-145: ContextWindowCalibrator ──

func TestContextWindowCalibrator_HighHitRate(t *testing.T) {
	c := NewContextWindowCalibrator(1000, 8000, 500)
	c.RecordPerformance(0.9, 0.8)
	newSize := c.Calibrate(0.9, 0.8)
	if newSize <= 1000 {
		t.Errorf("high hit rate should increase window, got %d", newSize)
	}
}

func TestContextWindowCalibrator_LowHitRate(t *testing.T) {
	c := NewContextWindowCalibrator(1000, 8000, 500)
	c.RecordPerformance(0.2, 0.3)
	newSize := c.Calibrate(0.2, 0.3)
	if newSize >= 8000 {
		t.Errorf("low hit rate should decrease or keep window, got %d", newSize)
	}
}

func TestContextWindowCalibrator_MediumHitRate(t *testing.T) {
	c := NewContextWindowCalibrator(1000, 8000, 500)
	c.RecordPerformance(0.6, 0.6)
	newSize := c.Calibrate(0.6, 0.6)
	// Medium hit rate should keep current size
	if newSize != 1000 {
		t.Errorf("medium hit rate should keep current size, got %d", newSize)
	}
}

func TestContextWindowCalibrator_Bounds(t *testing.T) {
	c := NewContextWindowCalibrator(1000, 2000, 500)
	// Push to max
	for i := 0; i < 10; i++ {
		c.Calibrate(0.9, 0.9)
	}
	if c.GetCurrentSize() > 2000 {
		t.Errorf("size should not exceed max, got %d", c.GetCurrentSize())
	}
	// Push to min
	for i := 0; i < 10; i++ {
		c.Calibrate(0.1, 0.1)
	}
	if c.GetCurrentSize() < 1000 {
		t.Errorf("size should not go below min, got %d", c.GetCurrentSize())
	}
}

func TestContextWindowCalibrator_Stats(t *testing.T) {
	c := NewContextWindowCalibrator(1000, 8000, 500)
	c.RecordPerformance(0.7, 0.6)
	stats := c.GetStats()
	if stats["currentSize"].(int) != 1000 {
		t.Errorf("currentSize should be 1000, got %v", stats["currentSize"])
	}
}

func TestContextWindowCalibrator_Reset(t *testing.T) {
	c := NewContextWindowCalibrator(1000, 8000, 500)
	c.RecordPerformance(0.9, 0.9)
	c.Calibrate(0.9, 0.9)
	c.Reset()
	stats := c.GetStats()
	if stats["totalCalibrations"].(int) != 0 {
		t.Errorf("totalCalibrations should be 0 after reset")
	}
}
