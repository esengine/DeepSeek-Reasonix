package agent

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// ── OPT-56: ReasoningTokenOptimizer ──

func TestReasoningTokenOptimizer_OptimizeReasoning(t *testing.T) {
	o := NewReasoningTokenOptimizer()
	short := "short reasoning content"
	result := o.OptimizeReasoning(short, 1)
	if result != short {
		t.Fatal("short reasoning should not be truncated")
	}
}

func TestReasoningTokenOptimizer_LongReasoning(t *testing.T) {
	o := NewReasoningTokenOptimizer()
	long := strings.Repeat("a", 20000) // 20000 chars > 4096*4
	result := o.OptimizeReasoning(long, 1)
	if len(result) >= len(long) {
		t.Fatal("long reasoning should be truncated")
	}
	if !strings.Contains(result, "truncated") {
		t.Fatal("should contain truncation marker")
	}
}

func TestReasoningTokenOptimizer_ShouldInclude(t *testing.T) {
	o := NewReasoningTokenOptimizer()
	if !o.ShouldIncludeReasoning(1, false) {
		t.Fatal("turn 1 should include reasoning")
	}
	if o.ShouldIncludeReasoning(10, false) {
		t.Fatal("turn 10 without tools should not include reasoning")
	}
	if !o.ShouldIncludeReasoning(10, true) {
		t.Fatal("turn 10 with tools should include reasoning")
	}
}

func TestReasoningTokenOptimizer_GetStats(t *testing.T) {
	o := NewReasoningTokenOptimizer()
	o.OptimizeReasoning(strings.Repeat("a", 20000), 1)
	stats := o.GetStats()
	if stats.TotalOptimized == 0 {
		t.Fatal("should have optimization stats")
	}
}

// ── OPT-57: ContextPrioritizer ──

func TestContextPrioritizer_PrioritizeMessages(t *testing.T) {
	p := NewContextPrioritizer()
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "q1"},
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleAssistant, Content: "a1"},
	}
	result := p.PrioritizeMessages(msgs)
	if len(result) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result))
	}
	// System should be first
	if result[0].Role != provider.RoleSystem {
		t.Fatal("system message should be first after prioritization")
	}
}

func TestContextPrioritizer_ScoreMessage(t *testing.T) {
	p := NewContextPrioritizer()
	sysScore := p.ScoreMessage(provider.Message{Role: provider.RoleSystem, Content: "sys"}, 0, 10)
	userScore := p.ScoreMessage(provider.Message{Role: provider.RoleUser, Content: "q"}, 9, 10)
	if sysScore <= userScore {
		t.Fatal("system message should have higher priority score")
	}
}

func TestContextPrioritizer_GetStats(t *testing.T) {
	p := NewContextPrioritizer()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "q"},
	}
	p.PrioritizeMessages(msgs)
	stats := p.GetStats()
	if stats.TotalPrioritized == 0 {
		t.Fatal("should have prioritization stats")
	}
}

// ── OPT-58: TokenAwarenessMonitor ──

func TestTokenAwarenessMonitor_CheckOK(t *testing.T) {
	m := NewTokenAwarenessMonitor()
	report := m.CheckAwareness(10000, 2000, 128000)
	if report.Status != "ok" {
		t.Fatalf("expected ok, got %s", report.Status)
	}
}

func TestTokenAwarenessMonitor_CheckWarning(t *testing.T) {
	m := NewTokenAwarenessMonitor()
	report := m.CheckAwareness(100000, 5000, 128000)
	if report.Status != "warning" {
		t.Fatalf("expected warning, got %s", report.Status)
	}
}

func TestTokenAwarenessMonitor_CheckCritical(t *testing.T) {
	m := NewTokenAwarenessMonitor()
	report := m.CheckAwareness(120000, 5000, 128000)
	if report.Status != "critical" {
		t.Fatalf("expected critical, got %s", report.Status)
	}
}

func TestTokenAwarenessMonitor_TrackTurn(t *testing.T) {
	m := NewTokenAwarenessMonitor()
	m.TrackTurn(10000, 2000)
	m.TrackTurn(20000, 3000)
	stats := m.GetStats()
	if stats.AvgTokensPerTurn < 10000 {
		t.Fatalf("expected avg > 10000, got %f", stats.AvgTokensPerTurn)
	}
}

func TestTokenAwarenessMonitor_GetStats(t *testing.T) {
	m := NewTokenAwarenessMonitor()
	m.CheckAwareness(10000, 2000, 128000)
	stats := m.GetStats()
	if stats.TotalChecks != 1 {
		t.Fatalf("expected 1 check, got %d", stats.TotalChecks)
	}
}

// ── OPT-59: ErrorContextOptimizer ──

func TestErrorContextOptimizer_IsRetryable(t *testing.T) {
	o := NewErrorContextOptimizer()
	if !o.IsRetryableError("connection timeout") {
		t.Fatal("timeout should be retryable")
	}
	if !o.IsRetryableError("rate limit exceeded (429)") {
		t.Fatal("rate limit should be retryable")
	}
	if o.IsRetryableError("syntax error in code") {
		t.Fatal("syntax error should not be retryable")
	}
}

func TestErrorContextOptimizer_OptimizeErrorContext(t *testing.T) {
	o := NewErrorContextOptimizer()
	errorMsg := "Error: file not found"
	fullContext := "line1\nline2\nError: file not found\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	result := o.OptimizeErrorContext(errorMsg, fullContext)
	if len(result) >= len(fullContext) {
		t.Fatal("optimized context should be shorter")
	}
}

func TestErrorContextOptimizer_ExtractRelevant(t *testing.T) {
	o := NewErrorContextOptimizer()
	context := "line1\nline2\nline3\nERROR: something failed\nline5\nline6\nline7\nline8\nline9\nline10"
	result := o.ExtractErrorRelevantContext("ERROR", context)
	if !strings.Contains(result, "ERROR") {
		t.Fatal("should contain error line")
	}
}

func TestErrorContextOptimizer_GetStats(t *testing.T) {
	o := NewErrorContextOptimizer()
	o.OptimizeErrorContext("error", "context with error")
	stats := o.GetStats()
	if stats.TotalOptimized == 0 {
		t.Fatal("should have optimization stats")
	}
}

// ── OPT-60: AdaptiveCacheManager ──

func TestAdaptiveCacheManager_Default(t *testing.T) {
	m := NewAdaptiveCacheManager()
	if m.GetStrategy() != "balanced" {
		t.Fatalf("expected balanced, got %s", m.GetStrategy())
	}
}

func TestAdaptiveCacheManager_AdaptStrategy(t *testing.T) {
	m := NewAdaptiveCacheManager()
	if s := m.AdaptStrategy(0.2, 500); s != "aggressive" {
		t.Fatalf("low hit rate should be aggressive, got %s", s)
	}
	if s := m.AdaptStrategy(0.9, 100); s != "minimal" {
		t.Fatalf("high hit rate should be minimal, got %s", s)
	}
	if s := m.AdaptStrategy(0.5, 100); s != "balanced" {
		t.Fatalf("medium hit rate should be balanced, got %s", s)
	}
}

func TestAdaptiveCacheManager_ShouldAddBreakpoint(t *testing.T) {
	m := NewAdaptiveCacheManager()
	if !m.ShouldAddBreakpoint(2000, 0.3) {
		t.Fatal("should add breakpoint with high misses and low hit rate")
	}
	if m.ShouldAddBreakpoint(100, 0.8) {
		t.Fatal("should not add breakpoint with low misses and high hit rate")
	}
}

func TestAdaptiveCacheManager_RecordPerformance(t *testing.T) {
	m := NewAdaptiveCacheManager()
	m.RecordCachePerformance(8000, 2000)
	stats := m.GetStats()
	if stats.CurrentHitRate < 0.7 || stats.CurrentHitRate > 0.85 {
		t.Fatalf("expected ~0.8 hit rate, got %f", stats.CurrentHitRate)
	}
}

func TestAdaptiveCacheManager_GetStats(t *testing.T) {
	m := NewAdaptiveCacheManager()
	m.RecordCachePerformance(8000, 2000)
	stats := m.GetStats()
	if stats.Strategy == "" {
		t.Fatal("should have strategy")
	}
}
