package agent

import (
	"testing"
)

// ── OPT-66: DedupStatsReporter ──

func TestDedupStatsReporter_Record(t *testing.T) {
	r := NewDedupStatsReporter()
	r.RecordDedup("opt17_conversationDedup", 500)
	r.RecordDedup("opt17_conversationDedup", 300)
	r.RecordDedup("opt16_toolMemo", 200)
	report := r.GetReport()
	if report.TotalDedups != 3 {
		t.Fatalf("expected 3 dedups, got %d", report.TotalDedups)
	}
	if report.TotalTokensSaved != 1000 {
		t.Fatalf("expected 1000 saved, got %d", report.TotalTokensSaved)
	}
}

func TestDedupStatsReporter_ShouldReport(t *testing.T) {
	r := NewDedupStatsReporter()
	// ShouldReport returns true if 60s since last report; first call depends on implementation
	_ = r.ShouldReport()
}

// ── OPT-67: IncrementalCacheTracker ──

func TestIncrementalCacheTracker_Register(t *testing.T) {
	tr := NewIncrementalCacheTracker()
	tr.RegisterSegment("system_prompt", "hash1", 5000)
	seg := tr.GetSegment("system_prompt")
	if seg == nil {
		t.Fatal("segment should exist")
	}
	if seg.Hash != "hash1" {
		t.Fatalf("expected hash1, got %s", seg.Hash)
	}
}

func TestIncrementalCacheTracker_Update(t *testing.T) {
	tr := NewIncrementalCacheTracker()
	tr.RegisterSegment("tools", "hash1", 3000)
	// Update with same hash → not incremental
	if tr.UpdateSegment("tools", "hash1", 3000) {
		t.Fatal("same hash should not be incremental")
	}
	// Update with different hash → incremental
	if !tr.UpdateSegment("tools", "hash2", 3500) {
		t.Fatal("different hash should be incremental")
	}
}

func TestIncrementalCacheTracker_GetStats(t *testing.T) {
	tr := NewIncrementalCacheTracker()
	tr.RegisterSegment("seg1", "h1", 1000)
	tr.UpdateSegment("seg1", "h2", 1200)
	stats := tr.GetStats()
	if stats.SegmentsTracked != 1 {
		t.Fatalf("expected 1 segment, got %d", stats.SegmentsTracked)
	}
}

// ── OPT-68: TurnAwareDeduplicator ──

func TestTurnAwareDeduplicator_New(t *testing.T) {
	d := NewTurnAwareDeduplicator()
	content := "some unique content"
	_, deduped := d.CheckAndDedup(content, 1)
	if deduped {
		t.Fatal("first occurrence should not be deduped")
	}
}

func TestTurnAwareDeduplicator_Duplicate(t *testing.T) {
	d := NewTurnAwareDeduplicator()
	content := "some repeated content"
	d.CheckAndDedup(content, 1)
	_, deduped := d.CheckAndDedup(content, 5)
	if !deduped {
		t.Fatal("second occurrence should be deduped")
	}
}

func TestTurnAwareDeduplicator_HasSeen(t *testing.T) {
	d := NewTurnAwareDeduplicator()
	d.CheckAndDedup("content", 1)
	if !d.HasSeen("content") {
		t.Fatal("should have seen content")
	}
	if d.HasSeen("unseen") {
		t.Fatal("should not have seen unseen content")
	}
}

func TestTurnAwareDeduplicator_GetStats(t *testing.T) {
	d := NewTurnAwareDeduplicator()
	d.CheckAndDedup("content1", 1)
	d.CheckAndDedup("content1", 2) // deduped
	d.CheckAndDedup("content2", 3)
	stats := d.GetStats()
	if stats.TotalDeduped != 1 {
		t.Fatalf("expected 1 dedup, got %d", stats.TotalDeduped)
	}
	if stats.UniqueContentCount != 2 {
		t.Fatalf("expected 2 unique, got %d", stats.UniqueContentCount)
	}
}

// ── OPT-69: SmartToolSelector ──

func TestSmartToolSelector_RecordUsage(t *testing.T) {
	s := NewSmartToolSelector()
	s.RecordToolUsage("bash", true)
	s.RecordToolUsage("bash", true)
	s.RecordToolUsage("bash", false)
	// Priority should be between 0 and 1
	priority := s.GetToolPriority("bash")
	if priority < 0 || priority > 1 {
		t.Fatalf("priority should be 0-1, got %f", priority)
	}
}

func TestSmartToolSelector_SelectTools(t *testing.T) {
	s := NewSmartToolSelector()
	s.RecordToolUsage("bash", true)
	s.RecordToolUsage("edit_file", true)
	all := []string{"bash", "edit_file", "read_file", "grep", "glob", "web_search", "web_fetch", "mcp"}
	selected := s.SelectTools(all, "file editing", 5)
	if len(selected) > 5 {
		t.Fatalf("should select at most 5 tools, got %d", len(selected))
	}
	// Should always include read_file
	found := false
	for _, tool := range selected {
		if tool == "read_file" {
			found = true
		}
	}
	if !found {
		t.Fatal("should always include read_file")
	}
}

func TestSmartToolSelector_GetStats(t *testing.T) {
	s := NewSmartToolSelector()
	s.RecordToolUsage("bash", true)
	s.SelectTools([]string{"bash", "read_file"}, "test", 5)
	stats := s.GetStats()
	if stats.ToolsTracked == 0 {
		t.Fatal("should track tools")
	}
}

// ── OPT-70: TokenFlowAnalyzer ──

func TestTokenFlowAnalyzer_RecordFlow(t *testing.T) {
	a := NewTokenFlowAnalyzer()
	a.RecordFlow(1, 10000, 2000, 8000, 2000)
	stats := a.GetStats()
	if stats.TotalInputTokens != 10000 {
		t.Fatalf("expected 10000 input, got %d", stats.TotalInputTokens)
	}
	if stats.PeakUsage != 12000 {
		t.Fatalf("expected peak 12000, got %d", stats.PeakUsage)
	}
}

func TestTokenFlowAnalyzer_PeakTracking(t *testing.T) {
	a := NewTokenFlowAnalyzer()
	a.RecordFlow(1, 5000, 1000, 3000, 2000)  // net = 5000+1000 = 6000
	a.RecordFlow(2, 15000, 3000, 10000, 5000) // net = 15000+3000 = 18000
	peak := a.GetPeakUsage()
	if peak != 18000 {
		t.Fatalf("expected peak 18000, got %d", peak)
	}
}

func TestTokenFlowAnalyzer_Distribution(t *testing.T) {
	a := NewTokenFlowAnalyzer()
	a.RecordFlow(1, 10000, 2000, 8000, 2000)
	dist := a.GetTokenDistribution()
	total := dist.InputPercent + dist.OutputPercent + dist.CacheHitPercent + dist.CacheMissPercent
	if total < 99 || total > 101 {
		t.Fatalf("distribution should sum to ~100, got %f", total)
	}
}

func TestTokenFlowAnalyzer_GetFlowHistory(t *testing.T) {
	a := NewTokenFlowAnalyzer()
	a.RecordFlow(1, 1000, 200, 800, 200)
	a.RecordFlow(2, 2000, 400, 1600, 400)
	history := a.GetFlowHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 records, got %d", len(history))
	}
}

func TestTokenFlowAnalyzer_GetStats(t *testing.T) {
	a := NewTokenFlowAnalyzer()
	a.RecordFlow(1, 10000, 2000, 8000, 2000)
	stats := a.GetStats()
	if stats.RecordsCount != 1 {
		t.Fatalf("expected 1 record, got %d", stats.RecordsCount)
	}
}
