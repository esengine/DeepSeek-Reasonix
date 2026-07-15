package agent

import "testing"

// approxEqual is defined in opt176_180_test.go and shared across the package.
// func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// ============================================================================
// OPT-241: TokenAwareOverflowSpillway — Token感知溢出溢洪道
// ============================================================================

func TestTAOS_AddChannelAndSpill(t *testing.T) {
	s := NewTokenAwareOverflowSpillway(1000)
	s.AddChannel("backup-1")
	s.AddChannel("backup-2")

	ch, ok := s.Spill(100)
	if !ok {
		t.Errorf("Spill should succeed when channels are available, got ok=false")
	}
	if ch != "backup-1" {
		t.Errorf("Spill should return first channel, got %q, want %q", ch, "backup-1")
	}
	if s.GetCurrentLoad() != 100 {
		t.Errorf("GetCurrentLoad after Spill(100) = %d, want 100", s.GetCurrentLoad())
	}
}

func TestTAOS_SpillNoChannel(t *testing.T) {
	s := NewTokenAwareOverflowSpillway(1000)
	ch, ok := s.Spill(50)
	if ok {
		t.Errorf("Spill should fail when no channels available, got ok=true, ch=%q", ch)
	}
	if ch != "" {
		t.Errorf("Spill should return empty channel name when no channels, got %q", ch)
	}
	if s.GetCurrentLoad() != 0 {
		t.Errorf("GetCurrentLoad should be 0 after failed Spill, got %d", s.GetCurrentLoad())
	}
}

func TestTAOS_GetCurrentLoad(t *testing.T) {
	s := NewTokenAwareOverflowSpillway(1000)
	s.AddChannel("ch1")
	if s.GetCurrentLoad() != 0 {
		t.Errorf("GetCurrentLoad initially = %d, want 0", s.GetCurrentLoad())
	}
	s.Spill(100)
	s.Spill(200)
	if s.GetCurrentLoad() != 300 {
		t.Errorf("GetCurrentLoad after Spill(100)+Spill(200) = %d, want 300", s.GetCurrentLoad())
	}
}

func TestTAOS_GetUtilization(t *testing.T) {
	s := NewTokenAwareOverflowSpillway(1000)
	s.AddChannel("ch1")
	s.Spill(250)
	util := s.GetUtilization()
	if !approxEqual(util, 0.25) {
		t.Errorf("GetUtilization after Spill(250) with capacity 1000 = %v, want 0.25", util)
	}
	// capacity <= 0 returns 0
	s2 := NewTokenAwareOverflowSpillway(0)
	s2.AddChannel("ch1")
	if s2.GetUtilization() != 0 {
		t.Errorf("GetUtilization with capacity 0 should be 0, got %v", s2.GetUtilization())
	}
}

func TestTAOS_Stats(t *testing.T) {
	s := NewTokenAwareOverflowSpillway(1000)
	s.AddChannel("ch1")
	s.AddChannel("ch2")
	s.Spill(100)
	s.Spill(200)

	stats := s.GetStats()
	if stats["spilledCount"].(int) != 2 {
		t.Errorf("stats spilledCount = %v, want 2", stats["spilledCount"])
	}
	if stats["totalSpilledTokens"].(int) != 300 {
		t.Errorf("stats totalSpilledTokens = %v, want 300", stats["totalSpilledTokens"])
	}
	if stats["channelCount"].(int) != 2 {
		t.Errorf("stats channelCount = %v, want 2", stats["channelCount"])
	}
	if stats["currentLoad"].(int) != 300 {
		t.Errorf("stats currentLoad = %v, want 300", stats["currentLoad"])
	}
	if !approxEqual(stats["utilization"].(float64), 0.3) {
		t.Errorf("stats utilization = %v, want 0.3", stats["utilization"])
	}
}

func TestTAOS_Reset(t *testing.T) {
	s := NewTokenAwareOverflowSpillway(1000)
	s.AddChannel("ch1")
	s.Spill(100)
	s.Spill(200)
	s.Reset()

	if s.GetCurrentLoad() != 0 {
		t.Errorf("GetCurrentLoad after Reset = %d, want 0", s.GetCurrentLoad())
	}
	stats := s.GetStats()
	if stats["spilledCount"].(int) != 0 {
		t.Errorf("stats spilledCount after Reset = %v, want 0", stats["spilledCount"])
	}
	if stats["totalSpilledTokens"].(int) != 0 {
		t.Errorf("stats totalSpilledTokens after Reset = %v, want 0", stats["totalSpilledTokens"])
	}
	// channels preserved after Reset
	if stats["channelCount"].(int) != 1 {
		t.Errorf("stats channelCount after Reset = %v, want 1 (channels preserved)", stats["channelCount"])
	}
	// capacity preserved after Reset
	if stats["capacity"].(int) != 1000 {
		t.Errorf("stats capacity after Reset = %v, want 1000 (capacity preserved)", stats["capacity"])
	}
	// spill should still work after Reset since channels are preserved
	ch, ok := s.Spill(50)
	if !ok || ch != "ch1" {
		t.Errorf("Spill after Reset should succeed, got ch=%q, ok=%v", ch, ok)
	}
}

// ============================================================================
// OPT-242: CacheInvalidationPriorityQueue — 缓存失效优先级队列
// ============================================================================

func TestCIPQ_EnqueueDequeue(t *testing.T) {
	q := NewCacheInvalidationPriorityQueue()
	q.Enqueue("low", 1)
	q.Enqueue("high", 10)
	q.Enqueue("mid", 5)

	item, ok := q.Dequeue()
	if !ok {
		t.Errorf("Dequeue should succeed, got ok=false")
	}
	if item.Key != "high" {
		t.Errorf("Dequeue should return highest priority, got key=%q, want %q", item.Key, "high")
	}
	if item.Priority != 10 {
		t.Errorf("Dequeue priority = %d, want 10", item.Priority)
	}

	item2, ok2 := q.Dequeue()
	if !ok2 {
		t.Errorf("second Dequeue should succeed, got ok=false")
	}
	if item2.Key != "mid" {
		t.Errorf("second Dequeue should return mid priority, got key=%q, want %q", item2.Key, "mid")
	}

	item3, ok3 := q.Dequeue()
	if !ok3 {
		t.Errorf("third Dequeue should succeed, got ok=false")
	}
	if item3.Key != "low" {
		t.Errorf("third Dequeue should return lowest priority, got key=%q, want %q", item3.Key, "low")
	}
}

func TestCIPQ_DequeueEmpty(t *testing.T) {
	q := NewCacheInvalidationPriorityQueue()
	item, ok := q.Dequeue()
	if ok {
		t.Errorf("Dequeue on empty queue should return false, got ok=true, key=%q", item.Key)
	}
	if item.Key != "" || item.Priority != 0 {
		t.Errorf("Dequeue on empty queue should return zero value, got key=%q, priority=%d", item.Key, item.Priority)
	}
}

func TestCIPQ_Peek(t *testing.T) {
	q := NewCacheInvalidationPriorityQueue()
	q.Enqueue("a", 1)
	q.Enqueue("b", 10)

	item, ok := q.Peek()
	if !ok {
		t.Errorf("Peek should succeed, got ok=false")
	}
	if item.Key != "b" {
		t.Errorf("Peek should return highest priority, got key=%q, want %q", item.Key, "b")
	}
	// Peek must not remove the item
	if q.Count() != 2 {
		t.Errorf("Count after Peek = %d, want 2 (Peek should not remove)", q.Count())
	}
	// Dequeue should still return the same item
	item2, ok2 := q.Dequeue()
	if !ok2 || item2.Key != "b" {
		t.Errorf("Dequeue after Peek should return same item, got key=%q, ok=%v", item2.Key, ok2)
	}
}

func TestCIPQ_Count(t *testing.T) {
	q := NewCacheInvalidationPriorityQueue()
	if q.Count() != 0 {
		t.Errorf("Count initially = %d, want 0", q.Count())
	}
	q.Enqueue("a", 1)
	q.Enqueue("b", 2)
	if q.Count() != 2 {
		t.Errorf("Count after 2 Enqueue = %d, want 2", q.Count())
	}
	q.Dequeue()
	if q.Count() != 1 {
		t.Errorf("Count after 1 Dequeue = %d, want 1", q.Count())
	}
}

func TestCIPQ_Stats(t *testing.T) {
	q := NewCacheInvalidationPriorityQueue()
	q.Enqueue("a", 1)
	q.Enqueue("b", 2)
	q.Enqueue("c", 3)
	q.Dequeue()

	stats := q.GetStats()
	if stats["totalEnqueued"].(int) != 3 {
		t.Errorf("stats totalEnqueued = %v, want 3", stats["totalEnqueued"])
	}
	if stats["totalDequeued"].(int) != 1 {
		t.Errorf("stats totalDequeued = %v, want 1", stats["totalDequeued"])
	}
	if stats["currentCount"].(int) != 2 {
		t.Errorf("stats currentCount = %v, want 2", stats["currentCount"])
	}
}

func TestCIPQ_Reset(t *testing.T) {
	q := NewCacheInvalidationPriorityQueue()
	q.Enqueue("a", 1)
	q.Enqueue("b", 2)
	q.Dequeue()
	q.Reset()

	if q.Count() != 0 {
		t.Errorf("Count after Reset = %d, want 0", q.Count())
	}
	stats := q.GetStats()
	if stats["totalEnqueued"].(int) != 0 {
		t.Errorf("stats totalEnqueued after Reset = %v, want 0", stats["totalEnqueued"])
	}
	if stats["totalDequeued"].(int) != 0 {
		t.Errorf("stats totalDequeued after Reset = %v, want 0", stats["totalDequeued"])
	}
}

// ============================================================================
// OPT-243: ContextWindowEvictionManager — 上下文窗口驱逐管理器
// ============================================================================

func TestCWEM_AddAndAccess(t *testing.T) {
	m := NewContextWindowEvictionManager(5)
	m.Add("key1", 100)

	stats := m.GetStats()
	if stats["entryCount"].(int) != 1 {
		t.Errorf("entryCount after Add = %v, want 1", stats["entryCount"])
	}

	// Access updates LastAccess, which should change LRU eviction order
	m.Add("key2", 200)
	m.Access("key1", 1000) // key1 accessed more recently than key2 (LastAccess=0)

	evicted, ok := m.EvictOne()
	if !ok {
		t.Errorf("EvictOne should succeed, got ok=false")
	}
	if evicted != "key2" {
		t.Errorf("EvictOne should evict least recently accessed (key2), got %q", evicted)
	}
}

func TestCWEM_LRUEviction(t *testing.T) {
	m := NewContextWindowEvictionManager(3)
	m.Add("a", 10)
	m.Add("b", 20)
	m.Add("c", 30)
	// Access a and c so b becomes the LRU (LastAccess=0)
	m.Access("a", 100)
	m.Access("c", 200)

	// Adding d exceeds maxEntries; b (LastAccess=0, smallest) should be evicted
	m.Add("d", 40)

	stats := m.GetStats()
	if stats["entryCount"].(int) != 3 {
		t.Errorf("entryCount after LRU eviction = %v, want 3", stats["entryCount"])
	}
	if stats["evictedCount"].(int) != 1 {
		t.Errorf("evictedCount after LRU eviction = %v, want 1", stats["evictedCount"])
	}
	if stats["totalTokensFreed"].(int) != 20 {
		t.Errorf("totalTokensFreed after LRU eviction = %v, want 20 (size of evicted b)", stats["totalTokensFreed"])
	}
}

func TestCWEM_EvictOne(t *testing.T) {
	m := NewContextWindowEvictionManager(5)
	m.Add("x", 50)
	m.Add("y", 60)
	m.Access("x", 1000) // x more recently accessed; y is LRU

	evicted, ok := m.EvictOne()
	if !ok {
		t.Errorf("EvictOne should succeed, got ok=false")
	}
	if evicted != "y" {
		t.Errorf("EvictOne should evict LRU entry (y), got %q", evicted)
	}
	stats := m.GetStats()
	if stats["entryCount"].(int) != 1 {
		t.Errorf("entryCount after EvictOne = %v, want 1", stats["entryCount"])
	}
	if stats["evictedCount"].(int) != 1 {
		t.Errorf("evictedCount after EvictOne = %v, want 1", stats["evictedCount"])
	}
	if stats["totalTokensFreed"].(int) != 60 {
		t.Errorf("totalTokensFreed after EvictOne = %v, want 60 (size of y)", stats["totalTokensFreed"])
	}
}

func TestCWEM_EvictOneEmpty(t *testing.T) {
	m := NewContextWindowEvictionManager(5)
	evicted, ok := m.EvictOne()
	if ok {
		t.Errorf("EvictOne on empty window should return false, got ok=true, key=%q", evicted)
	}
	if evicted != "" {
		t.Errorf("EvictOne on empty window should return empty key, got %q", evicted)
	}
}

func TestCWEM_Stats(t *testing.T) {
	m := NewContextWindowEvictionManager(2)
	m.Add("a", 10)
	m.Add("b", 20)
	m.Access("b", 100) // a is LRU (LastAccess=0)
	m.Add("c", 30)     // evicts a (size 10)

	stats := m.GetStats()
	if stats["maxEntries"].(int) != 2 {
		t.Errorf("stats maxEntries = %v, want 2", stats["maxEntries"])
	}
	if stats["entryCount"].(int) != 2 {
		t.Errorf("stats entryCount = %v, want 2", stats["entryCount"])
	}
	if stats["evictedCount"].(int) != 1 {
		t.Errorf("stats evictedCount = %v, want 1", stats["evictedCount"])
	}
	if stats["totalTokensFreed"].(int) != 10 {
		t.Errorf("stats totalTokensFreed = %v, want 10 (size of evicted a)", stats["totalTokensFreed"])
	}
}

func TestCWEM_Reset(t *testing.T) {
	m := NewContextWindowEvictionManager(5)
	m.Add("a", 10)
	m.Add("b", 20)
	m.Access("b", 100)
	m.EvictOne()
	m.Reset()

	stats := m.GetStats()
	if stats["entryCount"].(int) != 0 {
		t.Errorf("entryCount after Reset = %v, want 0", stats["entryCount"])
	}
	if stats["evictedCount"].(int) != 0 {
		t.Errorf("evictedCount after Reset = %v, want 0", stats["evictedCount"])
	}
	if stats["totalTokensFreed"].(int) != 0 {
		t.Errorf("totalTokensFreed after Reset = %v, want 0", stats["totalTokensFreed"])
	}
	// maxEntries preserved after Reset
	if stats["maxEntries"].(int) != 5 {
		t.Errorf("maxEntries after Reset = %v, want 5 (preserved)", stats["maxEntries"])
	}
}

// ============================================================================
// OPT-244: TokenAwareSaturationMonitor — Token感知饱和度监控器
// ============================================================================

func TestTASM3_Record(t *testing.T) {
	m := NewTokenAwareSaturationMonitor(1000, 10)
	sat := m.Record(500)
	if !approxEqual(sat, 0.5) {
		t.Errorf("Record(500) with capacity 1000 = %v, want 0.5", sat)
	}
	// saturation > 1.0 should be clamped to 1.0
	sat2 := m.Record(1500)
	if !approxEqual(sat2, 1.0) {
		t.Errorf("Record(1500) with capacity 1000 = %v, want 1.0 (clamped)", sat2)
	}
}

func TestTASM3_GetSaturation(t *testing.T) {
	m := NewTokenAwareSaturationMonitor(1000, 10)
	m.Record(750)
	sat := m.GetSaturation()
	if !approxEqual(sat, 0.75) {
		t.Errorf("GetSaturation after Record(750) = %v, want 0.75", sat)
	}
	// capacity <= 0 returns 0
	m2 := NewTokenAwareSaturationMonitor(0, 10)
	m2.Record(100)
	if m2.GetSaturation() != 0 {
		t.Errorf("GetSaturation with capacity 0 should be 0, got %v", m2.GetSaturation())
	}
}

func TestTASM3_GetAvgSaturation(t *testing.T) {
	m := NewTokenAwareSaturationMonitor(1000, 3)
	m.Record(500) // 0.5
	m.Record(600) // 0.6
	m.Record(700) // 0.7
	avg := m.GetAvgSaturation()
	if !approxEqual(avg, 0.6) {
		t.Errorf("GetAvgSaturation after [0.5, 0.6, 0.7] = %v, want 0.6", avg)
	}
	// sliding window: oldest (0.5) dropped, history becomes [0.6, 0.7, 0.8]
	m.Record(800) // 0.8
	avg2 := m.GetAvgSaturation()
	if !approxEqual(avg2, 0.7) {
		t.Errorf("GetAvgSaturation after sliding window [0.6, 0.7, 0.8] = %v, want 0.7", avg2)
	}
}

func TestTASM3_IsSaturated(t *testing.T) {
	m := NewTokenAwareSaturationMonitor(1000, 10)
	m.Record(500) // 0.5
	if m.IsSaturated() {
		t.Errorf("IsSaturated at 0.5 should be false")
	}
	m.Record(800) // 0.8, not strictly > 0.8
	if m.IsSaturated() {
		t.Errorf("IsSaturated at exactly 0.8 should be false (not > 0.8)")
	}
	m.Record(900) // 0.9, > 0.8
	if !m.IsSaturated() {
		t.Errorf("IsSaturated at 0.9 should be true")
	}
}

func TestTASM3_StatsAndReset(t *testing.T) {
	m := NewTokenAwareSaturationMonitor(1000, 10)
	m.Record(500) // 0.5, no alert
	m.Record(900) // 0.9, alert
	m.Record(950) // 0.95, alert

	stats := m.GetStats()
	if stats["alertCount"].(int) != 2 {
		t.Errorf("stats alertCount = %v, want 2", stats["alertCount"])
	}
	if stats["currentUsage"].(int) != 950 {
		t.Errorf("stats currentUsage = %v, want 950", stats["currentUsage"])
	}
	if !approxEqual(stats["currentSaturation"].(float64), 0.95) {
		t.Errorf("stats currentSaturation = %v, want 0.95", stats["currentSaturation"])
	}

	m.Reset()
	stats2 := m.GetStats()
	if stats2["alertCount"].(int) != 0 {
		t.Errorf("alertCount after Reset = %v, want 0", stats2["alertCount"])
	}
	if stats2["currentUsage"].(int) != 0 {
		t.Errorf("currentUsage after Reset = %v, want 0", stats2["currentUsage"])
	}
	if !approxEqual(stats2["avgSaturation"].(float64), 0.0) {
		t.Errorf("avgSaturation after Reset = %v, want 0", stats2["avgSaturation"])
	}
	// capacity preserved after Reset
	if stats2["capacity"].(int) != 1000 {
		t.Errorf("capacity after Reset = %v, want 1000 (preserved)", stats2["capacity"])
	}
}

// ============================================================================
// OPT-245: PromptCachePressureIndex — 提示缓存压力指数
// ============================================================================

func TestPCPI_UpdateAndGetPressureIndex(t *testing.T) {
	p := NewPromptCachePressureIndex()
	// index = 0.9*0.5 + 0.5*0.5 - 0.1*0.2 = 0.45 + 0.25 - 0.02 = 0.68
	p.Update(0.1, 0.9, 0.5)
	idx := p.GetPressureIndex()
	if !approxEqual(idx, 0.68) {
		t.Errorf("GetPressureIndex after Update(0.1, 0.9, 0.5) = %v, want 0.68", idx)
	}
	// index = 0.1*0.5 + 0.0*0.5 - 0.9*0.2 = 0.05 - 0.18 = -0.13 -> clamped to 0
	p.Update(0.9, 0.1, 0.0)
	idx2 := p.GetPressureIndex()
	if !approxEqual(idx2, 0.0) {
		t.Errorf("GetPressureIndex after Update(0.9, 0.1, 0.0) = %v, want 0.0 (clamped)", idx2)
	}
}

func TestPCPI_GetPressureLevel(t *testing.T) {
	p := NewPromptCachePressureIndex()
	// index = 0.68 -> high
	p.Update(0.1, 0.9, 0.5)
	if p.GetPressureLevel() != "high" {
		t.Errorf("GetPressureLevel for index 0.68 = %q, want %q", p.GetPressureLevel(), "high")
	}
	// index = 1.0 -> critical
	p.Update(0.0, 1.0, 1.0)
	if p.GetPressureLevel() != "critical" {
		t.Errorf("GetPressureLevel for index 1.0 = %q, want %q", p.GetPressureLevel(), "critical")
	}
	// index = 0.25 -> low
	p.Update(0.5, 0.5, 0.2)
	if p.GetPressureLevel() != "low" {
		t.Errorf("GetPressureLevel for index 0.25 = %q, want %q", p.GetPressureLevel(), "low")
	}
	// index = 0.44 -> medium
	p.Update(0.3, 0.7, 0.3)
	if p.GetPressureLevel() != "medium" {
		t.Errorf("GetPressureLevel for index 0.44 = %q, want %q", p.GetPressureLevel(), "medium")
	}
}

func TestPCPI_DifferentHitRates(t *testing.T) {
	// High hit rate -> low pressure
	pHigh := NewPromptCachePressureIndex()
	pHigh.Update(0.9, 0.1, 0.1)
	// index = 0.1*0.5 + 0.1*0.5 - 0.9*0.2 = 0.05 + 0.05 - 0.18 = -0.08 -> 0.0
	highIdx := pHigh.GetPressureIndex()

	// Low hit rate -> high pressure
	pLow := NewPromptCachePressureIndex()
	pLow.Update(0.1, 0.9, 0.9)
	// index = 0.9*0.5 + 0.9*0.5 - 0.1*0.2 = 0.45 + 0.45 - 0.02 = 0.88
	lowIdx := pLow.GetPressureIndex()

	if !(lowIdx > highIdx) {
		t.Errorf("low hit rate should produce higher pressure: lowIdx=%v, highIdx=%v", lowIdx, highIdx)
	}
	if !approxEqual(highIdx, 0.0) {
		t.Errorf("high hit rate pressure index = %v, want 0.0", highIdx)
	}
	if !approxEqual(lowIdx, 0.88) {
		t.Errorf("low hit rate pressure index = %v, want 0.88", lowIdx)
	}
}

func TestPCPI_Stats(t *testing.T) {
	p := NewPromptCachePressureIndex()
	p.Update(0.5, 0.5, 0.2)
	p.Update(0.1, 0.9, 0.5)

	stats := p.GetStats()
	if stats["calculations"].(int) != 2 {
		t.Errorf("stats calculations = %v, want 2", stats["calculations"])
	}
	if !approxEqual(stats["hitRate"].(float64), 0.1) {
		t.Errorf("stats hitRate = %v, want 0.1", stats["hitRate"])
	}
	if !approxEqual(stats["missRate"].(float64), 0.9) {
		t.Errorf("stats missRate = %v, want 0.9", stats["missRate"])
	}
	if !approxEqual(stats["evictionRate"].(float64), 0.5) {
		t.Errorf("stats evictionRate = %v, want 0.5", stats["evictionRate"])
	}
	// index = 0.68 -> high
	if stats["pressureLevel"].(string) != "high" {
		t.Errorf("stats pressureLevel = %q, want %q", stats["pressureLevel"], "high")
	}
}

func TestPCPI_Reset(t *testing.T) {
	p := NewPromptCachePressureIndex()
	p.Update(0.1, 0.9, 0.5)
	p.Reset()

	if p.GetPressureIndex() != 0 {
		t.Errorf("GetPressureIndex after Reset = %v, want 0", p.GetPressureIndex())
	}
	if p.GetPressureLevel() != "low" {
		t.Errorf("GetPressureLevel after Reset = %q, want %q", p.GetPressureLevel(), "low")
	}
	stats := p.GetStats()
	if stats["calculations"].(int) != 0 {
		t.Errorf("stats calculations after Reset = %v, want 0", stats["calculations"])
	}
	if !approxEqual(stats["hitRate"].(float64), 0.0) {
		t.Errorf("stats hitRate after Reset = %v, want 0", stats["hitRate"])
	}
}
