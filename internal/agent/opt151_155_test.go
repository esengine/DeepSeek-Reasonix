package agent

import (
	"testing"
)

// ── OPT-151: TokenAwareSplitter ──

func TestTokenAwareSplitter_Split(t *testing.T) {
	s := NewTokenAwareSplitter(10)
	text := "This is a test sentence. This is another sentence. And yet another one here. More text to ensure splitting occurs."
	segments := s.Split(text, 500)
	if len(segments) <= 1 {
		t.Errorf("should split into multiple segments, got %d", len(segments))
	}
	for i, seg := range segments {
		if seg == "" {
			t.Errorf("segment %d should not be empty", i)
		}
	}
}

func TestTokenAwareSplitter_ShortText(t *testing.T) {
	s := NewTokenAwareSplitter(4096)
	segments := s.Split("Hello", 0)
	if len(segments) != 1 {
		t.Errorf("short text should return 1 segment, got %d", len(segments))
	}
	if segments[0] != "Hello" {
		t.Errorf("segment should be 'Hello', got %q", segments[0])
	}
}

func TestTokenAwareSplitter_FindSplitPoint(t *testing.T) {
	s := NewTokenAwareSplitter(100)

	// Test newline boundary
	text1 := "first line\nsecond line\nthird line"
	point1 := s.FindSplitPoint(text1, 3) // maxChars = 12
	if point1 <= 0 || point1 > len(text1) {
		t.Errorf("split point should be within (0, %d], got %d", len(text1), point1)
	} else if text1[point1-1] != '\n' {
		t.Errorf("split point should be at newline, got char %q at %d", text1[point1-1], point1)
	}

	// Test period boundary
	text2 := "First sentence. Second sentence."
	point2 := s.FindSplitPoint(text2, 4) // maxChars = 16
	if point2 <= 0 || point2 > len(text2) {
		t.Errorf("split point should be within (0, %d], got %d", len(text2), point2)
	} else if text2[point2-1] != '.' {
		t.Errorf("split point should be at period, got char %q at %d", text2[point2-1], point2)
	}

	// Test space boundary
	text3 := "hello world test value"
	point3 := s.FindSplitPoint(text3, 2) // maxChars = 8
	if point3 <= 0 || point3 > len(text3) {
		t.Errorf("split point should be within (0, %d], got %d", len(text3), point3)
	} else if text3[point3-1] != ' ' {
		t.Errorf("split point should be at space, got char %q at %d", text3[point3-1], point3)
	}
}

func TestTokenAwareSplitter_Stats(t *testing.T) {
	s := NewTokenAwareSplitter(100)
	s.Split("This is a long text that exceeds the budget.", 200)
	stats := s.GetStats()
	if stats["splitCount"].(int) != 1 {
		t.Errorf("splitCount should be 1, got %v", stats["splitCount"])
	}
	if stats["maxSegmentTokens"].(int) != 100 {
		t.Errorf("maxSegmentTokens should be 100, got %v", stats["maxSegmentTokens"])
	}
}

func TestTokenAwareSplitter_Reset(t *testing.T) {
	s := NewTokenAwareSplitter(100)
	s.Split("This is a long text that exceeds the budget.", 200)
	s.Split("Another long text for splitting.", 200)
	s.Reset()
	stats := s.GetStats()
	if stats["splitCount"].(int) != 0 {
		t.Errorf("splitCount should be 0 after reset, got %v", stats["splitCount"])
	}
	if stats["totalTokensSaved"].(int) != 0 {
		t.Errorf("totalTokensSaved should be 0 after reset, got %v", stats["totalTokensSaved"])
	}
}

// ── OPT-152: CacheCoherenceValidator ──

func TestCacheCoherenceValidator_Validate(t *testing.T) {
	v := NewCacheCoherenceValidator()
	v.Update("k", 1)
	result := v.Validate("k", 1)
	if !result {
		t.Errorf("Validate should return true for matching version, got false")
	}
}

func TestCacheCoherenceValidator_Violation(t *testing.T) {
	v := NewCacheCoherenceValidator()
	v.Update("k", 1)
	result := v.Validate("k", 2)
	if result {
		t.Errorf("Validate should return false for mismatched version, got true")
	}
}

func TestCacheCoherenceValidator_Remove(t *testing.T) {
	v := NewCacheCoherenceValidator()
	v.Update("k", 1)
	v.Remove("k")
	result := v.Validate("k", 1)
	if result {
		t.Errorf("Validate should return false after Remove, got true")
	}
}

func TestCacheCoherenceValidator_ViolationRate(t *testing.T) {
	v := NewCacheCoherenceValidator()
	v.Update("k", 1)
	v.Validate("k", 1) // true, checks=1, violations=0
	v.Validate("k", 2) // false, checks=2, violations=1
	rate := v.GetViolationRate()
	// 1 violation / 2 checks = 0.5
	if rate != 0.5 {
		t.Errorf("violation rate should be 0.5, got %f", rate)
	}
}

func TestCacheCoherenceValidator_Stats(t *testing.T) {
	v := NewCacheCoherenceValidator()
	v.Update("k1", 1)
	v.Update("k2", 2)
	v.Validate("k1", 1) // true
	v.Validate("k2", 5) // false (violation)
	stats := v.GetStats()
	if stats["trackedEntries"].(int) != 2 {
		t.Errorf("trackedEntries should be 2, got %v", stats["trackedEntries"])
	}
	if stats["checks"].(int) != 2 {
		t.Errorf("checks should be 2, got %v", stats["checks"])
	}
	if stats["violations"].(int) != 1 {
		t.Errorf("violations should be 1, got %v", stats["violations"])
	}
}

func TestCacheCoherenceValidator_Reset(t *testing.T) {
	v := NewCacheCoherenceValidator()
	v.Update("k", 1)
	v.Validate("k", 1)
	v.Validate("k", 2) // violation
	v.Reset()
	stats := v.GetStats()
	if stats["trackedEntries"].(int) != 0 {
		t.Errorf("trackedEntries should be 0 after reset, got %v", stats["trackedEntries"])
	}
	if stats["checks"].(int) != 0 {
		t.Errorf("checks should be 0 after reset, got %v", stats["checks"])
	}
	if stats["violations"].(int) != 0 {
		t.Errorf("violations should be 0 after reset, got %v", stats["violations"])
	}
}

// ── OPT-153: ContextRelevanceScorer ──

func TestContextRelevanceScorer_Score(t *testing.T) {
	s := NewContextRelevanceScorer()
	score := s.Score("hello world", "hello world")
	if score < 0.99 || score > 1.01 {
		t.Errorf("identical text score should be ~1.0, got %f", score)
	}
}

func TestContextRelevanceScorer_NoOverlap(t *testing.T) {
	s := NewContextRelevanceScorer()
	score := s.Score("apple banana", "cat dog")
	if score != 0.0 {
		t.Errorf("no overlap score should be 0, got %f", score)
	}
}

func TestContextRelevanceScorer_ScoreBatch(t *testing.T) {
	s := NewContextRelevanceScorer()
	segments := []string{"hello world", "cat dog", "hello there"}
	scores := s.ScoreBatch(segments, "hello world")
	if len(scores) != 3 {
		t.Errorf("should return 3 scores, got %d", len(scores))
	}
	if scores[0] < 0.99 {
		t.Errorf("first segment score should be ~1.0, got %f", scores[0])
	}
	if scores[1] != 0.0 {
		t.Errorf("second segment score should be 0, got %f", scores[1])
	}
}

func TestContextRelevanceScorer_GetTopSegments(t *testing.T) {
	s := NewContextRelevanceScorer()
	segments := []string{"hello world", "cat dog", "hello there"}
	top := s.GetTopSegments(segments, "hello world", 1)
	if len(top) != 1 {
		t.Errorf("should return 1 index, got %d", len(top))
	}
	if top[0] != 0 {
		t.Errorf("top segment should be index 0, got %d", top[0])
	}
	top2 := s.GetTopSegments(segments, "hello world", 2)
	if len(top2) != 2 {
		t.Errorf("should return 2 indices, got %d", len(top2))
	}
	if top2[0] != 0 {
		t.Errorf("first top segment should be index 0, got %d", top2[0])
	}
}

func TestContextRelevanceScorer_Stats(t *testing.T) {
	s := NewContextRelevanceScorer()
	s.Score("hello world", "hello world")
	stats := s.GetStats()
	if stats["scoredSegments"].(int) != 1 {
		t.Errorf("scoredSegments should be 1, got %v", stats["scoredSegments"])
	}
}

func TestContextRelevanceScorer_Reset(t *testing.T) {
	s := NewContextRelevanceScorer()
	s.Score("hello", "hello")
	s.Score("world", "world")
	s.Reset()
	stats := s.GetStats()
	if stats["scoredSegments"].(int) != 0 {
		t.Errorf("scoredSegments should be 0 after reset, got %v", stats["scoredSegments"])
	}
}

// ── OPT-154: WeightedBudgetAllocator ──

func TestWeightedBudgetAllocator_Allocate(t *testing.T) {
	a := NewWeightedBudgetAllocator(1000)
	a.RegisterConsumer("c1", 1.0)
	a.RegisterConsumer("c2", 1.0)
	result := a.Allocate()
	if result["c1"] != 500 {
		t.Errorf("c1 should get 500, got %d", result["c1"])
	}
	if result["c2"] != 500 {
		t.Errorf("c2 should get 500, got %d", result["c2"])
	}
}

func TestWeightedBudgetAllocator_WeightedAllocation(t *testing.T) {
	a := NewWeightedBudgetAllocator(1000)
	a.RegisterConsumer("c1", 1.0)
	a.RegisterConsumer("c2", 3.0)
	result := a.Allocate()
	if result["c2"] <= result["c1"] {
		t.Errorf("c2 (weight 3.0) should get more than c1 (weight 1.0), got c1=%d c2=%d", result["c1"], result["c2"])
	}
	if result["c1"] != 250 {
		t.Errorf("c1 should get 250, got %d", result["c1"])
	}
	if result["c2"] != 750 {
		t.Errorf("c2 should get 750, got %d", result["c2"])
	}
}

func TestWeightedBudgetAllocator_Adjust(t *testing.T) {
	a := NewWeightedBudgetAllocator(1000)
	a.RegisterConsumer("c1", 1.0)
	a.RegisterConsumer("c2", 1.0)
	a.Allocate() // c1=500, c2=500
	before := a.GetAllocation("c1")
	a.Adjust("c1", 100) // c1=600, c2=400
	after := a.GetAllocation("c1")
	if after <= before {
		t.Errorf("c1 allocation should increase after Adjust, before=%d after=%d", before, after)
	}
	if after != 600 {
		t.Errorf("c1 allocation should be 600, got %d", after)
	}
}

func TestWeightedBudgetAllocator_GetAllocation(t *testing.T) {
	a := NewWeightedBudgetAllocator(1000)
	alloc := a.GetAllocation("nonexistent")
	if alloc != 0 {
		t.Errorf("unregistered consumer allocation should be 0, got %d", alloc)
	}
}

func TestWeightedBudgetAllocator_Stats(t *testing.T) {
	a := NewWeightedBudgetAllocator(1000)
	a.RegisterConsumer("c1", 1.0)
	a.RegisterConsumer("c2", 2.0)
	a.Allocate()
	stats := a.GetStats()
	if stats["totalBudget"].(int) != 1000 {
		t.Errorf("totalBudget should be 1000, got %v", stats["totalBudget"])
	}
	if stats["consumerCount"].(int) != 2 {
		t.Errorf("consumerCount should be 2, got %v", stats["consumerCount"])
	}
}

func TestWeightedBudgetAllocator_Reset(t *testing.T) {
	a := NewWeightedBudgetAllocator(1000)
	a.RegisterConsumer("c1", 1.0)
	a.RegisterConsumer("c2", 2.0)
	a.Allocate()
	a.Reset()
	stats := a.GetStats()
	if stats["consumerCount"].(int) != 0 {
		t.Errorf("consumerCount should be 0 after reset, got %v", stats["consumerCount"])
	}
}

// ── OPT-155: PromptCompressionCache ──

func TestPromptCompressionCache_GetMiss(t *testing.T) {
	c := NewPromptCompressionCache(100)
	result, ok := c.Get("nonexistent")
	if ok {
		t.Errorf("Get should return false for missing key, got true")
	}
	if result != "" {
		t.Errorf("Get should return empty string for missing key, got %q", result)
	}
}

func TestPromptCompressionCache_PutAndGet(t *testing.T) {
	c := NewPromptCompressionCache(100)
	c.Put("hash1", "compressed1", 10)
	result, ok := c.Get("hash1")
	if !ok {
		t.Errorf("Get should return true after Put, got false")
	}
	if result != "compressed1" {
		t.Errorf("Get should return 'compressed1', got %q", result)
	}
}

func TestPromptCompressionCache_Invalidate(t *testing.T) {
	c := NewPromptCompressionCache(100)
	c.Put("hash1", "compressed1", 10)
	c.Invalidate("hash1")
	_, ok := c.Get("hash1")
	if ok {
		t.Errorf("Get should return false after Invalidate, got true")
	}
}

func TestPromptCompressionCache_HitRate(t *testing.T) {
	c := NewPromptCompressionCache(100)
	c.Get("missing") // miss, misses=1
	c.Put("hash1", "compressed1", 10)
	c.Get("hash1") // hit, hits=1
	c.Get("hash1") // hit, hits=2
	rate := c.GetHitRate()
	// 2 hits / (2 hits + 1 miss) = 2/3 ~ 0.667
	if rate < 0.6 || rate > 0.7 {
		t.Errorf("hit rate should be ~0.667, got %f", rate)
	}
}

func TestPromptCompressionCache_Eviction(t *testing.T) {
	c := NewPromptCompressionCache(2)
	c.Put("h1", "c1", 10)
	c.Put("h2", "c2", 20)
	c.Put("h3", "c3", 30) // should evict at least 1
	stats := c.GetStats()
	if stats["entries"].(int) > 2 {
		t.Errorf("entries should not exceed maxEntries (2), got %v", stats["entries"])
	}
	if stats["entries"].(int) != 2 {
		t.Errorf("entries should be 2 after eviction, got %v", stats["entries"])
	}
}

func TestPromptCompressionCache_Stats(t *testing.T) {
	c := NewPromptCompressionCache(100)
	c.Get("missing") // miss
	c.Put("h1", "compressed1", 10)
	c.Get("h1") // hit
	stats := c.GetStats()
	if stats["hits"].(int) != 1 {
		t.Errorf("hits should be 1, got %v", stats["hits"])
	}
	if stats["misses"].(int) != 1 {
		t.Errorf("misses should be 1, got %v", stats["misses"])
	}
}

func TestPromptCompressionCache_Reset(t *testing.T) {
	c := NewPromptCompressionCache(100)
	c.Put("h1", "compressed1", 10)
	c.Get("h1")      // hit
	c.Get("missing") // miss
	c.Reset()
	stats := c.GetStats()
	if stats["hits"].(int) != 0 {
		t.Errorf("hits should be 0 after reset, got %v", stats["hits"])
	}
	if stats["misses"].(int) != 0 {
		t.Errorf("misses should be 0 after reset, got %v", stats["misses"])
	}
	if stats["entries"].(int) != 0 {
		t.Errorf("entries should be 0 after reset, got %v", stats["entries"])
	}
}
