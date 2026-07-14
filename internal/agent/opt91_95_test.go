package agent

import (
	"testing"
)

// ── OPT-91: CacheHitPredictor ──

func TestCacheHitPredictor_PredictHit(t *testing.T) {
	p := NewCacheHitPredictor()
	// Same hash, no tools changed → predict hit
	if !p.PredictHit("hash1", "hash1", false) {
		t.Fatal("should predict hit when prefix unchanged")
	}
	// Different hash → predict miss
	if p.PredictHit("hash1", "hash2", false) {
		t.Fatal("should predict miss when prefix changed")
	}
	// Tools changed → predict miss
	if p.PredictHit("hash1", "hash1", true) {
		t.Fatal("should predict miss when tools changed")
	}
}

func TestCacheHitPredictor_RecordActualHit(t *testing.T) {
	p := NewCacheHitPredictor()
	p.PredictHit("h1", "h1", false) // predict hit
	p.RecordActualHit(true)          // actual hit → correct
	stats := p.GetStats()
	if stats.CorrectPredictions != 1 {
		t.Fatalf("expected 1 correct, got %d", stats.CorrectPredictions)
	}
}

func TestCacheHitPredictor_GetStats(t *testing.T) {
	p := NewCacheHitPredictor()
	p.PredictHit("h1", "h1", false)
	p.RecordActualHit(true)
	stats := p.GetStats()
	if stats.TotalPredictions != 1 {
		t.Fatalf("expected 1 prediction, got %d", stats.TotalPredictions)
	}
}

// ── OPT-92: ContextBudgetNegotiator ──

func TestContextBudgetNegotiator_Negotiate(t *testing.T) {
	n := NewContextBudgetNegotiator()
	alloc := n.Negotiate(100000, 5000, 15000, 60000, 10000)
	if alloc.Total == 0 {
		t.Fatal("should allocate tokens")
	}
	if alloc.System != 5000 {
		t.Fatalf("system should get full request, got %d", alloc.System)
	}
}

func TestContextBudgetNegotiator_OverBudget(t *testing.T) {
	n := NewContextBudgetNegotiator()
	alloc := n.Negotiate(10000, 5000, 5000, 5000, 5000) // requests exceed budget
	if alloc.System + alloc.Tools + alloc.History + alloc.Response + alloc.Reserved > 10000 {
		t.Fatal("total allocation should not exceed budget")
	}
}

func TestContextBudgetNegotiator_GetStats(t *testing.T) {
	n := NewContextBudgetNegotiator()
	n.Negotiate(100000, 5000, 15000, 60000, 10000)
	stats := n.GetStats()
	if stats.TotalNegotiations != 1 {
		t.Fatalf("expected 1 negotiation, got %d", stats.TotalNegotiations)
	}
}

// ── OPT-93: ToolResultSummarizer ──

func TestToolResultSummarizer_ShouldSummarize(t *testing.T) {
	s := NewToolResultSummarizer()
	if s.ShouldSummarize("bash", "short output", 100) {
		t.Fatal("short output should not be summarized")
	}
	long := make([]byte, 500)
	if !s.ShouldSummarize("bash", string(long), 100) {
		t.Fatal("long output should be summarized")
	}
}

func TestToolResultSummarizer_SummarizeBash(t *testing.T) {
	s := NewToolResultSummarizer()
	// Generate long bash output
	var lines string
	for i := 0; i < 30; i++ {
		lines += "line of output\n"
	}
	result := s.SummarizeResult("bash", lines, 100)
	if len(result) >= len(lines) {
		t.Fatal("summarized should be shorter")
	}
}

func TestToolResultSummarizer_PreserveGrep(t *testing.T) {
	s := NewToolResultSummarizer()
	result := "match1\nmatch2\nmatch3"
	summarized := s.SummarizeResult("grep", result, 0)
	if summarized != result {
		t.Fatal("grep results should not be modified")
	}
}

func TestToolResultSummarizer_GetStats(t *testing.T) {
	s := NewToolResultSummarizer()
	var lines string
	for i := 0; i < 30; i++ {
		lines += "line\n"
	}
	s.SummarizeResult("bash", lines, 100)
	stats := s.GetStats()
	if stats.TotalSummarized == 0 {
		t.Fatal("should have stats")
	}
}

// ── OPT-94: PromptSegmentManager ──

func TestPromptSegmentManager_Register(t *testing.T) {
	m := NewPromptSegmentManager()
	m.RegisterSegment("system", "system prompt", true, 1)
	m.RegisterSegment("tools", "tool schemas", true, 2)
	m.RegisterSegment("query", "user query", false, 3)
	seg := m.GetSegment("system")
	if seg == nil {
		t.Fatal("segment should exist")
	}
	if !seg.Cacheable {
		t.Fatal("system segment should be cacheable")
	}
}

func TestPromptSegmentManager_ReorderForCache(t *testing.T) {
	m := NewPromptSegmentManager()
	m.RegisterSegment("query", "user query", false, 3)
	m.RegisterSegment("system", "system prompt", true, 1)
	m.RegisterSegment("tools", "tool schemas", true, 2)
	order := m.ReorderForCache()
	if len(order) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(order))
	}
	// Cacheable segments should come first
	seg := m.GetSegment(order[0])
	if !seg.Cacheable {
		t.Fatal("first segment should be cacheable")
	}
}

func TestPromptSegmentManager_UpdateSegment(t *testing.T) {
	m := NewPromptSegmentManager()
	m.RegisterSegment("system", "original", true, 1)
	if !m.UpdateSegment("system", "updated") {
		t.Fatal("should update existing segment")
	}
	seg := m.GetSegment("system")
	if seg.Content != "updated" {
		t.Fatal("content should be updated")
	}
}

func TestPromptSegmentManager_GetStats(t *testing.T) {
	m := NewPromptSegmentManager()
	m.RegisterSegment("s1", "content", true, 1)
	m.ReorderForCache()
	stats := m.GetStats()
	if stats.SegmentsTracked != 1 {
		t.Fatalf("expected 1 segment, got %d", stats.SegmentsTracked)
	}
}

// ── OPT-95: ZeroTokenStatsCollector ──

func TestZeroTokenStatsCollector_ShouldCollect(t *testing.T) {
	c := NewZeroTokenStatsCollector()
	if !c.ShouldCollect(1000) {
		t.Fatal("should collect on first call")
	}
}

func TestZeroTokenStatsCollector_GetStats(t *testing.T) {
	c := NewZeroTokenStatsCollector()
	stats := c.GetStats()
	_ = stats // should not crash
}

func TestZeroTokenStatsCollector_Reset(t *testing.T) {
	c := NewZeroTokenStatsCollector()
	c.Reset()
	stats := c.GetStats()
	if stats.ModuleCount != 0 {
		t.Fatal("should reset to 0")
	}
}
