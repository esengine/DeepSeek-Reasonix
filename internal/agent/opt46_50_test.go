package agent

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// ── OPT-46: ConversationPhaseDetector ──

func TestConversationPhaseDetector_Exploration(t *testing.T) {
	d := NewConversationPhaseDetector()
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "what does this codebase do?"},
	}
	phase := d.Analyze(msgs, 0)
	if phase != PhaseExploration {
		t.Fatalf("turn 1 should be exploration, got %s", phase)
	}
}

func TestConversationPhaseDetector_Execution(t *testing.T) {
	d := NewConversationPhaseDetector()
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "q1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "edit the file"},
		{Role: provider.RoleAssistant, Content: "a2"},
	}
	// Need to get past turn 2 for execution phase
	d.Analyze(msgs, 0) // turn 1 → exploration
	d.Analyze(msgs, 0) // turn 2 → exploration
	// Turn 3 with tool calls → execution
	phase := d.Analyze(msgs, 2)
	if phase != PhaseExecution {
		t.Fatalf("turn 3 with tools should be execution, got %s", phase)
	}
}

func TestConversationPhaseDetector_GetOptimizationHint(t *testing.T) {
	d := NewConversationPhaseDetector()
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hello"}}
	d.Analyze(msgs, 0) // Exploration
	hint := d.GetOptimizationHint()
	if !hint.EnableToolMemo {
		t.Fatal("exploration should enable tool memo")
	}
}

func TestConversationPhaseDetector_GetStats(t *testing.T) {
	d := NewConversationPhaseDetector()
	d.Analyze([]provider.Message{{Role: provider.RoleUser, Content: "q"}}, 0)
	stats := d.GetStats()
	if stats.TurnCount != 1 {
		t.Fatalf("expected turn count 1, got %d", stats.TurnCount)
	}
}

// ── OPT-47: TokenEfficientFormatter ──

func TestTokenEfficientFormatter_FormatToolArgs(t *testing.T) {
	f := NewTokenEfficientFormatter()
	args := `{"path": "/some/file", "empty": "", "zero": 0, "name": "test"}`
	result := f.FormatToolArgs(args)
	if len(result) >= len(args) {
		t.Fatal("formatted should be shorter or equal")
	}
	if strings.Contains(result, "empty") {
		t.Fatal("should remove empty string values")
	}
}

func TestTokenEfficientFormatter_FormatToolOutput(t *testing.T) {
	f := NewTokenEfficientFormatter()
	output := "line1\n\n\n\nline2\n   trailing spaces   \nline3"
	result := f.FormatToolOutput(output, 0)
	if strings.Contains(result, "\n\n\n") {
		t.Fatal("should collapse multiple blank lines")
	}
}

func TestTokenEfficientFormatter_CompactJSON(t *testing.T) {
	f := NewTokenEfficientFormatter()
	json := `{ "key" : "value" , "num" : 123 }`
	result := f.CompactJSON(json)
	if strings.Contains(result, " ") {
		t.Fatal("compact JSON should have no spaces")
	}
}

func TestTokenEfficientFormatter_GetStats(t *testing.T) {
	f := NewTokenEfficientFormatter()
	f.FormatToolArgs(`{"key": "value"}`)
	stats := f.GetStats()
	if stats.TotalFormatted == 0 {
		t.Fatal("should have formatting stats")
	}
}

// ── OPT-48: CacheWarmingScheduler ──

func TestCacheWarmingScheduler_RecordQuery(t *testing.T) {
	s := NewCacheWarmingScheduler()
	s.RecordQuery("how to read a file")
	s.RecordQuery("how to read a file")
	s.RecordQuery("how to read a file")
	stats := s.GetStats()
	if stats.PatternsTracked != 1 {
		t.Fatalf("expected 1 pattern, got %d", stats.PatternsTracked)
	}
}

func TestCacheWarmingScheduler_ShouldWarmup(t *testing.T) {
	s := NewCacheWarmingScheduler()
	s.RecordQuery("test query")
	s.RecordQuery("test query")
	s.RecordQuery("test query")
	if !s.ShouldWarmup("test query") {
		t.Fatal("should warmup for frequent query")
	}
	if s.ShouldWarmup("unknown query") {
		t.Fatal("should not warmup for unknown query")
	}
}

func TestCacheWarmingScheduler_GetStats(t *testing.T) {
	s := NewCacheWarmingScheduler()
	s.RecordQuery("q1")
	stats := s.GetStats()
	if stats.PatternsTracked != 1 {
		t.Fatalf("expected 1 pattern, got %d", stats.PatternsTracked)
	}
}

// ── OPT-49: ProviderRetryOptimizer ──

func TestProviderRetryOptimizer_ShouldRetry(t *testing.T) {
	o := NewProviderRetryOptimizer()
	if !o.ShouldRetry("rate_limit", 1) {
		t.Fatal("should retry on first attempt for rate_limit")
	}
	if o.ShouldRetry("rate_limit", 5) {
		t.Fatal("should not retry after max attempts")
	}
}

func TestProviderRetryOptimizer_GetStrategy(t *testing.T) {
	o := NewProviderRetryOptimizer()
	strategy := o.GetRetryStrategy("timeout")
	if strategy == nil {
		t.Fatal("should return strategy for timeout")
	}
	if !strategy.UseCachedContext {
		t.Fatal("timeout strategy should use cached context")
	}
}

func TestProviderRetryOptimizer_EstimateTokenSavings(t *testing.T) {
	o := NewProviderRetryOptimizer()
	saved := o.EstimateTokenSavings("rate_limit", 10000)
	if saved <= 0 {
		t.Fatal("should estimate token savings for rate_limit retry")
	}
}

func TestProviderRetryOptimizer_RecordRetry(t *testing.T) {
	o := NewProviderRetryOptimizer()
	o.RecordRetry("rate_limit", 8000)
	stats := o.GetStats()
	if stats.TotalRetries != 1 {
		t.Fatalf("expected 1 retry, got %d", stats.TotalRetries)
	}
	if stats.AvgTokensSaved != 8000 {
		t.Fatalf("expected avg 8000 saved, got %f", stats.AvgTokensSaved)
	}
}

// ── OPT-50: ContextualToolFilter ──

func TestContextualToolFilter_DetectContext(t *testing.T) {
	f := NewContextualToolFilter()
	ctx := f.DetectContext("please edit the file and fix the code")
	if ctx != "file_editing" {
		t.Fatalf("expected file_editing, got %s", ctx)
	}
	ctx = f.DetectContext("search the web for information")
	if ctx != "web_research" {
		t.Fatalf("expected web_research, got %s", ctx)
	}
}

func TestContextualToolFilter_FilterTools(t *testing.T) {
	f := NewContextualToolFilter()
	allTools := []string{"bash", "edit_file", "read_file", "web_search", "web_fetch", "grep", "glob", "mcp"}
	filtered := f.FilterTools(allTools, "file_editing")
	// Should include file editing tools + always-included
	if len(filtered) >= len(allTools) {
		t.Fatal("filtered should be fewer than all tools")
	}
	// Should always include read_file, grep, glob
	found := false
	for _, tool := range filtered {
		if tool == "read_file" {
			found = true
		}
	}
	if !found {
		t.Fatal("should always include read_file")
	}
}

func TestContextualToolFilter_NoContext(t *testing.T) {
	f := NewContextualToolFilter()
	allTools := []string{"bash", "edit_file", "read_file"}
	filtered := f.FilterTools(allTools, "")
	if len(filtered) != len(allTools) {
		t.Fatal("empty context should return all tools")
	}
}

func TestContextualToolFilter_GetStats(t *testing.T) {
	f := NewContextualToolFilter()
	f.DetectContext("edit the file")
	stats := f.GetStats()
	if stats.TotalFiltered != 0 {
		t.Fatal("DetectContext should not increment TotalFiltered")
	}
}
