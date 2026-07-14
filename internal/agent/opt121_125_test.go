package agent

import (
	"testing"
)

// ── OPT-121: ContextWeightCalculator ──

func TestContextWeightCalculator_CalculateWeight(t *testing.T) {
	c := NewContextWeightCalculator()
	w := c.CalculateWeight("hello world this is a test message", 0, 5)
	if w < 0 || w > 1 {
		t.Errorf("weight should be 0..1, got %f", w)
	}
}

func TestContextWeightCalculator_FirstMessageHigher(t *testing.T) {
	c := NewContextWeightCalculator()
	w1 := c.CalculateWeight("test message here with some content for testing", 0, 5)
	w2 := c.CalculateWeight("test message here with some content for testing", 2, 5)
	// First message should have higher position factor
	if w1 < w2-0.1 {
		t.Errorf("first message weight (%f) should be >= middle message (%f)", w1, w2)
	}
}

func TestContextWeightCalculator_Category(t *testing.T) {
	c := NewContextWeightCalculator()
	if c.GetWeightCategory(0.2) != "low" {
		t.Error("0.2 should be low")
	}
	if c.GetWeightCategory(0.5) != "medium" {
		t.Error("0.5 should be medium")
	}
	if c.GetWeightCategory(0.7) != "high" {
		t.Error("0.7 should be high")
	}
	if c.GetWeightCategory(0.9) != "critical" {
		t.Error("0.9 should be critical")
	}
}

func TestContextWeightCalculator_Stats(t *testing.T) {
	c := NewContextWeightCalculator()
	c.CalculateWeight("test", 0, 3)
	c.CalculateWeight("test", 1, 3)
	stats := c.GetStats()
	if stats["totalCalculations"].(int) != 2 {
		t.Errorf("totalCalculations should be 2, got %v", stats["totalCalculations"])
	}
}

func TestContextWeightCalculator_Reset(t *testing.T) {
	c := NewContextWeightCalculator()
	c.CalculateWeight("test", 0, 1)
	c.Reset()
	stats := c.GetStats()
	if stats["totalCalculations"].(int) != 0 {
		t.Errorf("totalCalculations should be 0 after reset")
	}
}

// ── OPT-122: TokenAwareEvictor ──

func TestTokenAwareEvictor_LRU(t *testing.T) {
	e := NewTokenAwareEvictor("lru")
	e.AddCandidate("a", 100, 1, 5)
	e.AddCandidate("b", 200, 5, 3)
	e.AddCandidate("c", 150, 3, 10)

	selected := e.SelectForEviction(1)
	if len(selected) != 1 {
		t.Fatalf("should select 1, got %d", len(selected))
	}
	if selected[0].Key != "a" {
		t.Errorf("LRU should select 'a' (oldest), got %q", selected[0].Key)
	}
}

func TestTokenAwareEvictor_LFU(t *testing.T) {
	e := NewTokenAwareEvictor("lfu")
	e.AddCandidate("a", 100, 1, 5)
	e.AddCandidate("b", 200, 5, 3)
	e.AddCandidate("c", 150, 3, 10)

	selected := e.SelectForEviction(1)
	if selected[0].Key != "b" {
		t.Errorf("LFU should select 'b' (least used), got %q", selected[0].Key)
	}
}

func TestTokenAwareEvictor_SizePolicy(t *testing.T) {
	e := NewTokenAwareEvictor("size")
	e.AddCandidate("a", 100, 1, 5)
	e.AddCandidate("b", 500, 5, 3)
	e.AddCandidate("c", 150, 3, 10)

	selected := e.SelectForEviction(1)
	if selected[0].Key != "b" {
		t.Errorf("size policy should select 'b' (largest), got %q", selected[0].Key)
	}
}

func TestTokenAwareEvictor_Evict(t *testing.T) {
	e := NewTokenAwareEvictor("lru")
	e.AddCandidate("a", 100, 1, 5)
	e.AddCandidate("b", 200, 5, 3)
	freed := e.Evict(1)
	if freed != 100 {
		t.Errorf("should free 100 tokens, got %d", freed)
	}
}

func TestTokenAwareEvictor_Stats(t *testing.T) {
	e := NewTokenAwareEvictor("lru")
	e.AddCandidate("a", 100, 1, 5)
	e.Evict(1)
	stats := e.GetStats()
	if stats["totalEvicted"].(int) != 1 {
		t.Errorf("totalEvicted should be 1, got %v", stats["totalEvicted"])
	}
	if stats["totalTokensFreed"].(int) != 100 {
		t.Errorf("totalTokensFreed should be 100, got %v", stats["totalTokensFreed"])
	}
}

func TestTokenAwareEvictor_Reset(t *testing.T) {
	e := NewTokenAwareEvictor("lru")
	e.AddCandidate("a", 100, 1, 5)
	e.Reset()
	stats := e.GetStats()
	if stats["totalEvicted"].(int) != 0 {
		t.Errorf("totalEvicted should be 0 after reset")
	}
}

// ── OPT-123: PromptRedundancyChecker ──

func TestPromptRedundancyChecker_Clean(t *testing.T) {
	c := NewPromptRedundancyChecker()
	result := c.Check("hello world test message")
	// Clean prompt should have minimal redundancy
	total := 0
	for _, v := range result {
		total += v
	}
	if total > 5 {
		t.Errorf("clean prompt should have minimal redundancy, got %d", total)
	}
}

func TestPromptRedundancyChecker_Redundant(t *testing.T) {
	c := NewPromptRedundancyChecker()
	result := c.Check("basically actually very really basically the test is basically very important really")
	if result["filler_words"] == 0 {
		t.Errorf("should detect filler words")
	}
	if result["redundant_modifiers"] == 0 {
		t.Errorf("should detect redundant modifiers")
	}
}

func TestPromptRedundancyChecker_DuplicateSentences(t *testing.T) {
	c := NewPromptRedundancyChecker()
	result := c.Check("this is a test. this is a test. hello world.")
	if result["duplicate_sentences"] == 0 {
		t.Errorf("should detect duplicate sentences")
	}
}

func TestPromptRedundancyChecker_RedundancyScore(t *testing.T) {
	c := NewPromptRedundancyChecker()
	score := c.GetRedundancyScore("hello world test")
	if score < 0 || score > 1 {
		t.Errorf("score should be 0..1, got %f", score)
	}
}

func TestPromptRedundancyChecker_Stats(t *testing.T) {
	c := NewPromptRedundancyChecker()
	c.Check("hello world")
	stats := c.GetStats()
	if stats["totalChecks"].(int) != 1 {
		t.Errorf("totalChecks should be 1, got %v", stats["totalChecks"])
	}
}

func TestPromptRedundancyChecker_Reset(t *testing.T) {
	c := NewPromptRedundancyChecker()
	c.Check("hello world")
	c.Reset()
	stats := c.GetStats()
	if stats["totalChecks"].(int) != 0 {
		t.Errorf("totalChecks should be 0 after reset")
	}
}

// ── OPT-124: CacheHitAnalyzer ──

func TestCacheHitAnalyzer_HitRate(t *testing.T) {
	a := NewCacheHitAnalyzer()
	a.RecordHit("prompt")
	a.RecordHit("prompt")
	a.RecordMiss("tool")
	rate := a.GetHitRate()
	if rate < 0.6 || rate > 0.7 {
		t.Errorf("hit rate should be ~0.667, got %f", rate)
	}
}

func TestCacheHitAnalyzer_TopHitPattern(t *testing.T) {
	a := NewCacheHitAnalyzer()
	a.RecordHit("prompt")
	a.RecordHit("prompt")
	a.RecordHit("tool")
	top := a.GetTopHitPattern()
	if top != "prompt" {
		t.Errorf("top pattern should be 'prompt', got %q", top)
	}
}

func TestCacheHitAnalyzer_NoData(t *testing.T) {
	a := NewCacheHitAnalyzer()
	rate := a.GetHitRate()
	if rate != 0 {
		t.Errorf("no data should give 0 hit rate, got %f", rate)
	}
	top := a.GetTopHitPattern()
	if top != "" {
		t.Errorf("no data should give empty pattern, got %q", top)
	}
}

func TestCacheHitAnalyzer_Stats(t *testing.T) {
	a := NewCacheHitAnalyzer()
	a.RecordHit("prompt")
	a.RecordMiss("tool")
	stats := a.GetStats()
	if stats["totalRequests"].(int) != 2 {
		t.Errorf("totalRequests should be 2, got %v", stats["totalRequests"])
	}
	if stats["totalHits"].(int) != 1 {
		t.Errorf("totalHits should be 1, got %v", stats["totalHits"])
	}
}

func TestCacheHitAnalyzer_Reset(t *testing.T) {
	a := NewCacheHitAnalyzer()
	a.RecordHit("test")
	a.Reset()
	stats := a.GetStats()
	if stats["totalRequests"].(int) != 0 {
		t.Errorf("totalRequests should be 0 after reset")
	}
}

// ── OPT-125: ContextBoundaryDetector ──

func TestContextBoundaryDetector_DetectBoundaries(t *testing.T) {
	d := NewContextBoundaryDetector()
	msgs := []string{
		"hello world this is about databases",
		"hello world this is about databases",
		"completely different topic about cooking recipes",
		"another very different topic about space exploration and mars colonies",
	}
	boundaries := d.DetectBoundaries(msgs)
	// Should detect boundary between msg2 and msg3 (topic change)
	if len(boundaries) == 0 {
		t.Errorf("should detect at least 1 boundary")
	}
}

func TestContextBoundaryDetector_IsBoundary(t *testing.T) {
	d := NewContextBoundaryDetector()
	if d.IsBoundary("hello world", "hello world") {
		t.Errorf("identical messages should not have boundary")
	}
	if !d.IsBoundary("database query optimization", "cooking recipe pasta sauce") {
		t.Errorf("different topics should have boundary")
	}
}

func TestContextBoundaryDetector_LengthDifference(t *testing.T) {
	d := NewContextBoundaryDetector()
	short := "hi"
	long := "this is a very long message about complex topics that goes on and on with lots of details and explanations"
	if !d.IsBoundary(short, long) {
		t.Errorf("large length difference should have boundary")
	}
}

func TestContextBoundaryDetector_Stats(t *testing.T) {
	d := NewContextBoundaryDetector()
	d.DetectBoundaries([]string{"msg1", "completely different", "also different topic"})
	stats := d.GetStats()
	if stats["totalDetections"].(int) != 1 {
		t.Errorf("totalDetections should be 1, got %v", stats["totalDetections"])
	}
}

func TestContextBoundaryDetector_Reset(t *testing.T) {
	d := NewContextBoundaryDetector()
	d.DetectBoundaries([]string{"msg1", "msg2"})
	d.Reset()
	stats := d.GetStats()
	if stats["totalDetections"].(int) != 0 {
		t.Errorf("totalDetections should be 0 after reset")
	}
}
