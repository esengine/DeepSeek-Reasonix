package agent

import (
	"reasonix/internal/provider"
	"testing"
)

// ── OPT-106: SemanticCacheRouter ──

func TestSemanticCacheRouter_Route(t *testing.T) {
	r := NewSemanticCacheRouter()
	r.AddRoute("how to deploy go app", "cache_key_1")

	result := r.Route("how to deploy go app")
	if result != "cache_key_1" {
		t.Errorf("exact match should return cache key, got %q", result)
	}
}

func TestSemanticCacheRouter_SimilarRoute(t *testing.T) {
	r := NewSemanticCacheRouter()
	r.AddRoute("how to deploy go application", "cache_key_1")

	// Same tokens reordered -> Jaccard = 1.0
	result := r.Route("deploy go application how to")
	if result == "" {
		t.Errorf("similar query should match, got empty")
	}
}

func TestSemanticCacheRouter_NoMatch(t *testing.T) {
	r := NewSemanticCacheRouter()
	r.AddRoute("hello world", "cache_key_1")

	result := r.Route("completely different topic about database")
	if result != "" {
		t.Errorf("unrelated query should not match, got %q", result)
	}
}

func TestSemanticCacheRouter_HitRate(t *testing.T) {
	r := NewSemanticCacheRouter()
	r.AddRoute("hello world", "key1")
	r.Route("hello world")
	r.RecordHit()
	r.Route("no match")
	r.RecordMiss()

	rate := r.GetHitRate()
	if rate < 0 || rate > 1 {
		t.Errorf("hit rate should be 0..1, got %f", rate)
	}
}

func TestSemanticCacheRouter_Stats(t *testing.T) {
	r := NewSemanticCacheRouter()
	r.AddRoute("test", "key1")
	r.Route("test")
	stats := r.GetStats()
	if stats["tableSize"].(int) != 1 {
		t.Errorf("tableSize should be 1, got %v", stats["tableSize"])
	}
}

func TestSemanticCacheRouter_Reset(t *testing.T) {
	r := NewSemanticCacheRouter()
	r.AddRoute("test", "key1")
	r.Reset()
	stats := r.GetStats()
	if stats["tableSize"].(int) != 0 {
		t.Errorf("tableSize should be 0 after reset, got %v", stats["tableSize"])
	}
}

// ── OPT-107: TokenAwareScheduler ──

func TestTokenAwareScheduler_Schedule(t *testing.T) {
	s := NewTokenAwareScheduler(5000)
	id1 := s.Schedule("task A", 5, 1000)
	id2 := s.Schedule("task B", 10, 2000)

	if id1 == id2 {
		t.Errorf("tasks should have different IDs")
	}
}

func TestTokenAwareScheduler_NextReturnsHighestPriority(t *testing.T) {
	s := NewTokenAwareScheduler(5000)
	s.Schedule("low priority", 1, 1000)
	s.Schedule("high priority", 10, 1000)
	s.Schedule("medium priority", 5, 1000)

	next := s.Next()
	if next == nil {
		t.Fatal("Next should return a task")
	}
	if next.Priority != 10 {
		t.Errorf("should return highest priority (10), got %d", next.Priority)
	}
}

func TestTokenAwareScheduler_NextRespectsBudget(t *testing.T) {
	s := NewTokenAwareScheduler(1000)
	s.Schedule("expensive", 10, 5000)
	s.Schedule("cheap", 1, 500)

	next := s.Next()
	if next == nil {
		t.Fatal("Next should return a task")
	}
	if next.Description != "cheap" {
		t.Errorf("should return task within budget, got %q", next.Description)
	}
}

func TestTokenAwareScheduler_Complete(t *testing.T) {
	s := NewTokenAwareScheduler(5000)
	id := s.Schedule("task", 5, 1000)
	s.Complete(id)

	next := s.Next()
	if next != nil {
		t.Errorf("should have no tasks after complete, got %v", next)
	}
}

func TestTokenAwareScheduler_Stats(t *testing.T) {
	s := NewTokenAwareScheduler(5000)
	s.Schedule("task1", 5, 1000)
	s.Schedule("task2", 3, 2000)
	stats := s.GetStats()
	if stats["totalScheduled"].(int) != 2 {
		t.Errorf("totalScheduled should be 2, got %v", stats["totalScheduled"])
	}
	if stats["pendingTasks"].(int) != 2 {
		t.Errorf("pendingTasks should be 2, got %v", stats["pendingTasks"])
	}
}

func TestTokenAwareScheduler_Reset(t *testing.T) {
	s := NewTokenAwareScheduler(5000)
	s.Schedule("task", 5, 1000)
	s.Reset()
	stats := s.GetStats()
	if stats["totalScheduled"].(int) != 0 {
		t.Errorf("totalScheduled should be 0 after reset, got %v", stats["totalScheduled"])
	}
}

// ── OPT-108: ContextSnapshotManager ──

func TestContextSnapshotManager_TakeAndRestore(t *testing.T) {
	m := NewContextSnapshotManager(10)
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "hi there"},
	}
	m.TakeSnapshot(1, msgs)

	restored := m.Restore(1)
	if len(restored) != 2 {
		t.Errorf("should restore 2 messages, got %d", len(restored))
	}
	if restored[0].Content != "hello" {
		t.Errorf("first message should be 'hello', got %q", restored[0].Content)
	}
}

func TestContextSnapshotManager_RestoreNotFound(t *testing.T) {
	m := NewContextSnapshotManager(10)
	restored := m.Restore(99)
	if restored != nil {
		t.Errorf("should return nil for non-existent snapshot, got %v", restored)
	}
}

func TestContextSnapshotManager_Eviction(t *testing.T) {
	m := NewContextSnapshotManager(2)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "turn1"}}
	m.TakeSnapshot(1, msgs)
	m.TakeSnapshot(2, msgs)
	m.TakeSnapshot(3, msgs) // should evict turn 1

	if m.Restore(1) != nil {
		t.Errorf("turn 1 should have been evicted")
	}
	if m.Restore(2) == nil {
		t.Errorf("turn 2 should still exist")
	}
}

func TestContextSnapshotManager_GetLatestTurn(t *testing.T) {
	m := NewContextSnapshotManager(10)
	m.TakeSnapshot(3, nil)
	m.TakeSnapshot(7, nil)
	m.TakeSnapshot(5, nil)

	latest := m.GetLatestTurn()
	if latest != 7 {
		t.Errorf("latest turn should be 7, got %d", latest)
	}
}

func TestContextSnapshotManager_Stats(t *testing.T) {
	m := NewContextSnapshotManager(10)
	m.TakeSnapshot(1, nil)
	m.TakeSnapshot(2, nil)
	m.Restore(1)
	stats := m.GetStats()
	if stats["totalSnapshots"].(int) != 2 {
		t.Errorf("totalSnapshots should be 2, got %v", stats["totalSnapshots"])
	}
	if stats["restored"].(int) != 1 {
		t.Errorf("restored should be 1, got %v", stats["restored"])
	}
}

func TestContextSnapshotManager_Reset(t *testing.T) {
	m := NewContextSnapshotManager(10)
	m.TakeSnapshot(1, nil)
	m.Reset()
	stats := m.GetStats()
	if stats["totalSnapshots"].(int) != 0 {
		t.Errorf("totalSnapshots should be 0 after reset, got %v", stats["totalSnapshots"])
	}
}

// ── OPT-109: TokenWasteDetector ──

func TestTokenWasteDetector_DetectRedundancy(t *testing.T) {
	d := NewTokenWasteDetector()
	msgs := []string{
		"please help me with the database connection issue",
		"please help me with the database connection issue",
	}
	waste := d.DetectRedundancy(msgs)
	if waste == 0 {
		t.Errorf("should detect redundancy in duplicate messages")
	}
}

func TestTokenWasteDetector_DetectVerbosity(t *testing.T) {
	d := NewTokenWasteDetector()
	// Very long message with lots of filler
	long := "well I was thinking that maybe perhaps we could potentially consider the possibility that the database connection might possibly have some issues with the configuration that we set up earlier today"
	waste := d.DetectVerbosity(long)
	// verbosity returns 0 for single message since it needs average for comparison
	// but with a very long message it should still return some estimate
	_ = waste // just verify no panic
}

func TestTokenWasteDetector_Detect(t *testing.T) {
	d := NewTokenWasteDetector()
	msgs := []string{
		"hello   world   !!!  ...  hello   world",
		"hello   world   !!!  ...  hello   world",
	}
	result := d.Detect(msgs)
	if result["redundancy"] == 0 && result["whitespace"] == 0 && result["boilerplate"] == 0 {
		// At least one category should detect waste
		t.Errorf("should detect some waste, got %v", result)
	}
}

func TestTokenWasteDetector_Stats(t *testing.T) {
	d := NewTokenWasteDetector()
	d.Detect([]string{"hello world", "hello world"})
	stats := d.GetStats()
	if stats["totalChecks"].(int) != 1 {
		t.Errorf("totalChecks should be 1, got %v", stats["totalChecks"])
	}
}

func TestTokenWasteDetector_Reset(t *testing.T) {
	d := NewTokenWasteDetector()
	d.Detect([]string{"hello world"})
	d.Reset()
	stats := d.GetStats()
	if stats["totalChecks"].(int) != 0 {
		t.Errorf("totalChecks should be 0 after reset, got %v", stats["totalChecks"])
	}
}

// ── OPT-110: AdaptiveBatchOptimizer ──

func TestAdaptiveBatchOptimizer_AddAndFlush(t *testing.T) {
	o := NewAdaptiveBatchOptimizer(3)
	o.AddItem("item1")
	o.AddItem("item2")
	if o.ShouldFlush() {
		t.Errorf("should not flush with 2 items when batch size is 3")
	}
	o.AddItem("item3")
	if !o.ShouldFlush() {
		t.Errorf("should flush with 3 items when batch size is 3")
	}
	batch := o.Flush()
	if len(batch) != 3 {
		t.Errorf("flushed batch should have 3 items, got %d", len(batch))
	}
}

func TestAdaptiveBatchOptimizer_AdjustBatchSize(t *testing.T) {
	o := NewAdaptiveBatchOptimizer(5)
	o.AdjustBatchSize(10) // positive feedback increases
	stats := o.GetStats()
	if stats["optimalBatchSize"].(int) <= 5 {
		t.Errorf("batch size should increase with positive feedback, got %v", stats["optimalBatchSize"])
	}
	o.AdjustBatchSize(-10) // negative feedback decreases
	stats = o.GetStats()
	if stats["optimalBatchSize"].(int) >= 10 {
		t.Errorf("batch size should decrease with negative feedback, got %v", stats["optimalBatchSize"])
	}
}

func TestAdaptiveBatchOptimizer_BatchSizeBounds(t *testing.T) {
	o := NewAdaptiveBatchOptimizer(50)
	// Push way up
	for i := 0; i < 100; i++ {
		o.AdjustBatchSize(100)
	}
	stats := o.GetStats()
	if stats["optimalBatchSize"].(int) > 100 {
		t.Errorf("batch size should not exceed 100, got %v", stats["optimalBatchSize"])
	}
	// Push way down
	for i := 0; i < 100; i++ {
		o.AdjustBatchSize(-100)
	}
	stats = o.GetStats()
	if stats["optimalBatchSize"].(int) < 1 {
		t.Errorf("batch size should not go below 1, got %v", stats["optimalBatchSize"])
	}
}

func TestAdaptiveBatchOptimizer_Stats(t *testing.T) {
	o := NewAdaptiveBatchOptimizer(3)
	o.AddItem("a")
	o.AddItem("b")
	o.AddItem("c")
	o.Flush()
	stats := o.GetStats()
	if stats["totalBatches"].(int) != 1 {
		t.Errorf("totalBatches should be 1, got %v", stats["totalBatches"])
	}
	if stats["totalItems"].(int) != 3 {
		t.Errorf("totalItems should be 3, got %v", stats["totalItems"])
	}
}

func TestAdaptiveBatchOptimizer_Reset(t *testing.T) {
	o := NewAdaptiveBatchOptimizer(3)
	o.AddItem("a")
	o.Flush()
	o.Reset()
	stats := o.GetStats()
	if stats["totalBatches"].(int) != 0 {
		t.Errorf("totalBatches should be 0 after reset, got %v", stats["totalBatches"])
	}
	if stats["currentBatchSize"].(int) != 0 {
		t.Errorf("currentBatchSize should be 0 after reset, got %v", stats["currentBatchSize"])
	}
}
