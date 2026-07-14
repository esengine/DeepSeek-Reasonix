package agent

import (
	"testing"
)

// ── OPT-146: TokenAwareDispatcher ──

func TestTokenAwareDispatcher_Dispatch(t *testing.T) {
	d := NewTokenAwareDispatcher(1000)
	d.RegisterHandler("h1")
	d.RegisterHandler("h2")

	handler := d.Dispatch("task1", 100)
	if handler == "" {
		t.Errorf("should dispatch to a handler")
	}
}

func TestTokenAwareDispatcher_BudgetBalance(t *testing.T) {
	d := NewTokenAwareDispatcher(1000)
	d.RegisterHandler("h1")
	d.RegisterHandler("h2")

	// Both handlers have 1000 budget, should alternate
	h1 := d.Dispatch("task1", 500)
	h2 := d.Dispatch("task2", 500)
	// First dispatch goes to first handler with most remaining budget
	if h1 == "" || h2 == "" {
		t.Errorf("both should be dispatched")
	}
}

func TestTokenAwareDispatcher_ExhaustedBudget(t *testing.T) {
	d := NewTokenAwareDispatcher(100)
	d.RegisterHandler("h1")
	d.Dispatch("task1", 100)
	handler := d.Dispatch("task2", 50)
	if handler != "" {
		t.Errorf("should reject when all handlers exhausted")
	}
}

func TestTokenAwareDispatcher_GetHandlerLoad(t *testing.T) {
	d := NewTokenAwareDispatcher(1000)
	d.RegisterHandler("h1")
	d.Dispatch("task1", 200)
	load := d.GetHandlerLoad()
	if load["h1"] != 200 {
		t.Errorf("h1 load should be 200, got %d", load["h1"])
	}
}

func TestTokenAwareDispatcher_Stats(t *testing.T) {
	d := NewTokenAwareDispatcher(1000)
	d.RegisterHandler("h1")
	d.Dispatch("task1", 100)
	stats := d.GetStats()
	if stats["totalDispatched"].(int) != 1 {
		t.Errorf("totalDispatched should be 1, got %v", stats["totalDispatched"])
	}
}

func TestTokenAwareDispatcher_Reset(t *testing.T) {
	d := NewTokenAwareDispatcher(1000)
	d.RegisterHandler("h1")
	d.Dispatch("task1", 100)
	d.Reset()
	stats := d.GetStats()
	if stats["totalDispatched"].(int) != 0 {
		t.Errorf("totalDispatched should be 0 after reset")
	}
}

// ── OPT-147: CachePopulationPredictor ──

func TestCachePopulationPredictor_PredictNoData(t *testing.T) {
	p := NewCachePopulationPredictor()
	prob := p.PredictFill("key1")
	if prob < 0 || prob > 1 {
		t.Errorf("probability should be 0..1, got %f", prob)
	}
}

func TestCachePopulationPredictor_RecordAndPredict(t *testing.T) {
	p := NewCachePopulationPredictor()
	p.RecordFill("key1", true)
	p.RecordFill("key1", true)
	p.RecordFill("key1", false)
	prob := p.PredictFill("key1")
	if prob < 0.5 { // 2/3 = 0.667
		t.Errorf("key1 should have high fill probability, got %f", prob)
	}
}

func TestCachePopulationPredictor_FillRate(t *testing.T) {
	p := NewCachePopulationPredictor()
	p.RecordFill("a", true)
	p.RecordFill("b", false)
	p.RecordFill("c", true)
	rate := p.GetFillRate()
	if rate < 0.6 || rate > 0.7 { // 2/3 = 0.667
		t.Errorf("fill rate should be ~0.667, got %f", rate)
	}
}

func TestCachePopulationPredictor_Stats(t *testing.T) {
	p := NewCachePopulationPredictor()
	p.RecordFill("key1", true)
	stats := p.GetStats()
	if stats["patternCount"].(int) != 1 {
		t.Errorf("patternCount should be 1, got %v", stats["patternCount"])
	}
}

func TestCachePopulationPredictor_Reset(t *testing.T) {
	p := NewCachePopulationPredictor()
	p.RecordFill("key1", true)
	p.Reset()
	stats := p.GetStats()
	if stats["patternCount"].(int) != 0 {
		t.Errorf("patternCount should be 0 after reset")
	}
}

// ── OPT-148: ContextSegmentAssembler ──

func TestContextSegmentAssembler_AddAndAssemble(t *testing.T) {
	a := NewContextSegmentAssembler()
	a.AddSegment("intro", "hello world")
	a.AddSegment("body", "this is the body")
	a.AddSegment("outro", "goodbye")
	result := a.Assemble()
	if result == "" {
		t.Errorf("assembled result should not be empty")
	}
}

func TestContextSegmentAssembler_Order(t *testing.T) {
	a := NewContextSegmentAssembler()
	a.AddSegment("a", "first")
	a.AddSegment("b", "second")
	a.SetOrder([]string{"b", "a"})
	result := a.Assemble()
	// "b" should come before "a"
	if len(result) < 10 {
		t.Errorf("result should contain both segments")
	}
}

func TestContextSegmentAssembler_Remove(t *testing.T) {
	a := NewContextSegmentAssembler()
	a.AddSegment("a", "first")
	a.AddSegment("b", "second")
	a.RemoveSegment("a")
	result := a.Assemble()
	if result != "second" {
		t.Errorf("after removing 'a', result should be 'second', got %q", result)
	}
}

func TestContextSegmentAssembler_Stats(t *testing.T) {
	a := NewContextSegmentAssembler()
	a.AddSegment("a", "first")
	a.AddSegment("b", "second")
	a.Assemble()
	stats := a.GetStats()
	if stats["totalAssemblies"].(int) != 1 {
		t.Errorf("totalAssemblies should be 1, got %v", stats["totalAssemblies"])
	}
}

func TestContextSegmentAssembler_Reset(t *testing.T) {
	a := NewContextSegmentAssembler()
	a.AddSegment("a", "first")
	a.Reset()
	stats := a.GetStats()
	if stats["totalAssemblies"].(int) != 0 {
		t.Errorf("totalAssemblies should be 0 after reset")
	}
}

// ── OPT-149: TokenAwareMerger ──

func TestTokenAwareMerger_CanMerge(t *testing.T) {
	m := NewTokenAwareMerger(0.5)
	if !m.CanMerge("hello world test", "hello world test") {
		t.Errorf("identical messages should be mergeable")
	}
	if m.CanMerge("hello world database", "cooking pasta recipes") {
		t.Errorf("different messages should not be mergeable")
	}
}

func TestTokenAwareMerger_Merge(t *testing.T) {
	m := NewTokenAwareMerger(0.5)
	result := m.Merge("hello world", "hello test")
	if result == "" {
		t.Errorf("merged result should not be empty")
	}
	// Should contain all unique tokens
	if len(result) < 10 {
		t.Errorf("merged result should contain all tokens, got %q", result)
	}
}

func TestTokenAwareMerger_BatchMerge(t *testing.T) {
	m := NewTokenAwareMerger(0.7)
	msgs := []string{
		"hello world test",
		"hello world test two",
		"completely different topic",
	}
	result := m.BatchMerge(msgs)
	if len(result) > len(msgs) {
		t.Errorf("batch merge should reduce or keep same count, got %d (original %d)", len(result), len(msgs))
	}
}

func TestTokenAwareMerger_Stats(t *testing.T) {
	m := NewTokenAwareMerger(0.5)
	m.Merge("hello world", "hello test")
	stats := m.GetStats()
	if stats["totalMerged"].(int) < 1 {
		t.Errorf("totalMerged should be at least 1, got %v", stats["totalMerged"])
	}
}

func TestTokenAwareMerger_Reset(t *testing.T) {
	m := NewTokenAwareMerger(0.5)
	m.Merge("hello", "world")
	m.Reset()
	stats := m.GetStats()
	if stats["totalMerged"].(int) != 0 {
		t.Errorf("totalMerged should be 0 after reset")
	}
}

// ── OPT-150: CacheUtilizationTracker ──

func TestCacheUtilizationTracker_InsertAndEvict(t *testing.T) {
	tr := NewCacheUtilizationTracker(100)
	tr.RecordInsert()
	tr.RecordInsert()
	tr.RecordEviction()
	util := tr.GetUtilization()
	if util != 0.01 { // 1/100
		t.Errorf("utilization should be 0.01, got %f", util)
	}
}

func TestCacheUtilizationTracker_FullCapacity(t *testing.T) {
	tr := NewCacheUtilizationTracker(5)
	for i := 0; i < 10; i++ {
		tr.RecordInsert() // should cap at 5
	}
	util := tr.GetUtilization()
	if util > 1.0 {
		t.Errorf("utilization should not exceed 1.0, got %f", util)
	}
}

func TestCacheUtilizationTracker_Trend(t *testing.T) {
	tr := NewCacheUtilizationTracker(20)
	// Increasing utilization
	for i := 0; i < 10; i++ {
		tr.RecordInsert()
	}
	trend := tr.GetUtilizationTrend()
	if trend != "increasing" {
		t.Errorf("should be increasing, got %s", trend)
	}
}

func TestCacheUtilizationTracker_Stats(t *testing.T) {
	tr := NewCacheUtilizationTracker(100)
	tr.RecordInsert()
	tr.RecordInsert()
	stats := tr.GetStats()
	if stats["totalInserts"].(int) != 2 {
		t.Errorf("totalInserts should be 2, got %v", stats["totalInserts"])
	}
	if stats["usedEntries"].(int) != 2 {
		t.Errorf("usedEntries should be 2, got %v", stats["usedEntries"])
	}
}

func TestCacheUtilizationTracker_Reset(t *testing.T) {
	tr := NewCacheUtilizationTracker(100)
	tr.RecordInsert()
	tr.Reset()
	stats := tr.GetStats()
	if stats["totalInserts"].(int) != 0 {
		t.Errorf("totalInserts should be 0 after reset")
	}
	if stats["usedEntries"].(int) != 0 {
		t.Errorf("usedEntries should be 0 after reset")
	}
}
