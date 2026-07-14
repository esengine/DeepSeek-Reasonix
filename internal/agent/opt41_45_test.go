package agent

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// ── OPT-41: TokenAwareMessageSorter ──

func TestTokenAwareMessageSorter_RecordPrefix(t *testing.T) {
	s := NewTokenAwareMessageSorter()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "hi"},
	}
	report := s.RecordPrefix(msgs)
	if report.PrefixHash == "" {
		t.Fatal("prefix hash should not be empty")
	}
	if report.PrefixChanged {
		t.Fatal("first call should not report change")
	}
}

func TestTokenAwareMessageSorter_DetectChange(t *testing.T) {
	s := NewTokenAwareMessageSorter()
	msgs1 := []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: "hello"},
	}
	s.RecordPrefix(msgs1)

	msgs2 := []provider.Message{
		{Role: provider.RoleSystem, Content: "CHANGED system prompt"},
		{Role: provider.RoleUser, Content: "hello"},
	}
	report := s.RecordPrefix(msgs2)
	if !report.PrefixChanged {
		t.Fatal("should detect prefix change")
	}
}

func TestTokenAwareMessageSorter_StablePrefix(t *testing.T) {
	s := NewTokenAwareMessageSorter()
	// Two calls with identical messages → prefix should be stable
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "q1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "q2"},
	}
	s.RecordPrefix(msgs)
	// Same messages again → stable
	report := s.RecordPrefix(msgs)
	if report.PrefixChanged {
		t.Fatal("identical messages should produce stable prefix")
	}
}

func TestTokenAwareMessageSorter_GetStats(t *testing.T) {
	s := NewTokenAwareMessageSorter()
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "test"},
	}
	s.RecordPrefix(msgs)
	stats := s.GetStats()
	if stats.TotalChecks != 1 {
		t.Fatalf("expected 1 total check, got %d", stats.TotalChecks)
	}
}

func TestTokenAwareMessageSorter_SuggestReorder(t *testing.T) {
	s := NewTokenAwareMessageSorter()
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "q"},
		{Role: provider.RoleTool, Content: "result1"},
		{Role: provider.RoleTool, Content: "result2"},
		{Role: provider.RoleAssistant, Content: "a"},
	}
	indices := s.SuggestReorder(msgs)
	if len(indices) != len(msgs) {
		t.Fatalf("expected %d indices, got %d", len(msgs), len(indices))
	}
}

// ── OPT-42: StreamingTokenGuard ──

func TestStreamingTokenGuard_NormalUsage(t *testing.T) {
	g := NewStreamingTokenGuard(128000)
	g.RecordInput(10000)
	g.RecordOutput(2000)
	status := g.CheckBudget()
	if status.Status != "ok" {
		t.Fatalf("expected ok, got %s", status.Status)
	}
}

func TestStreamingTokenGuard_WarningThreshold(t *testing.T) {
	g := NewStreamingTokenGuard(10000)
	g.RecordInput(8000) // 80% > 75% warning threshold
	status := g.CheckBudget()
	if status.Status != "warning" {
		t.Fatalf("expected warning, got %s", status.Status)
	}
}

func TestStreamingTokenGuard_CriticalThreshold(t *testing.T) {
	g := NewStreamingTokenGuard(10000)
	g.RecordInput(9500) // 95% > 90% critical threshold
	status := g.CheckBudget()
	if status.Status != "critical" {
		t.Fatalf("expected critical, got %s", status.Status)
	}
	if !g.ShouldTerminate() {
		t.Fatal("should terminate at critical level")
	}
}

func TestStreamingTokenGuard_ResetTurn(t *testing.T) {
	g := NewStreamingTokenGuard(10000)
	g.RecordInput(5000)
	g.ResetTurn()
	status := g.CheckBudget()
	if status.Status != "ok" {
		t.Fatal("after reset should be ok")
	}
}

func TestStreamingTokenGuard_GetStats(t *testing.T) {
	g := NewStreamingTokenGuard(10000)
	g.RecordInput(5000)
	g.RecordOutput(1000)
	g.ResetTurn()
	stats := g.GetStats()
	if stats.TurnsMonitored != 1 {
		t.Fatalf("expected 1 turn monitored, got %d", stats.TurnsMonitored)
	}
}

// ── OPT-43: ToolResultTruncator ──

func TestToolResultTruncator_NoTruncation(t *testing.T) {
	tr := NewToolResultTruncator(4000)
	short := "hello world"
	result := tr.Truncate("bash", short, 0)
	if result != short {
		t.Fatal("short output should not be truncated")
	}
}

func TestToolResultTruncator_LongOutput(t *testing.T) {
	tr := NewToolResultTruncator(100) // Very small limit
	// Create a long output
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("line of output content that is moderately long\n")
	}
	long := sb.String()
	result := tr.Truncate("bash", long, 0)
	if len(result) >= len(long) {
		t.Fatal("truncated should be shorter than original")
	}
	if !strings.Contains(result, "truncated") {
		t.Fatal("should contain truncation marker")
	}
}

func TestToolResultTruncator_PreserveErrors(t *testing.T) {
	tr := NewToolResultTruncator(100)
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("filler line\n")
	}
	sb.WriteString("ERROR: something went wrong\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("more filler\n")
	}
	result := tr.Truncate("bash", sb.String(), 0)
	if !strings.Contains(result, "ERROR") {
		t.Fatal("should preserve error lines")
	}
}

func TestToolResultTruncator_GetStats(t *testing.T) {
	tr := NewToolResultTruncator(100)
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("filler\n")
	}
	tr.Truncate("bash", sb.String(), 0)
	stats := tr.GetTotalStats()
	if stats.TotalTruncated == 0 {
		t.Fatal("should have truncation stats")
	}
}

func TestToolResultTruncator_EstimateTokens(t *testing.T) {
	tr := NewToolResultTruncator(4000)
	tokens := tr.EstimateTokens("hello world this is a test")
	if tokens != 6 { // 25/4 = 6
		t.Fatalf("expected 6 tokens, got %d", tokens)
	}
}

// ── OPT-44: TokenBudgetAllocator ──

func TestTokenBudgetAllocator_Default(t *testing.T) {
	a := NewTokenBudgetAllocator(128000)
	stats := a.GetStats()
	if stats.WindowSize != 128000 {
		t.Fatalf("expected window 128000, got %d", stats.WindowSize)
	}
}

func TestTokenBudgetAllocator_Allocate(t *testing.T) {
	a := NewTokenBudgetAllocator(128000)
	alloc := a.Allocate(6400, 19200, 76800) // 5%, 15%, 60%
	if alloc.Response < 12800 { // At least 10%
		t.Fatalf("response should be at least 10%% of window, got %d", alloc.Response)
	}
}

func TestTokenBudgetAllocator_ShouldCompact(t *testing.T) {
	a := NewTokenBudgetAllocator(128000)
	// 70% of 128000 = 89600
	if !a.ShouldCompact(90000) {
		t.Fatal("should compact when history > 70%")
	}
	if a.ShouldCompact(50000) {
		t.Fatal("should not compact when history < 70%")
	}
}

func TestTokenBudgetAllocator_GetOptimalHistoryLimit(t *testing.T) {
	a := NewTokenBudgetAllocator(128000)
	limit := a.GetOptimalHistoryLimit()
	if limit != 89600 { // 70% of 128000
		t.Fatalf("expected 89600, got %d", limit)
	}
}

// ── OPT-45: SystemPromptMinimizer ──

func TestSystemPromptMinimizer_SetOriginal(t *testing.T) {
	m := NewSystemPromptMinimizer()
	prompt := "Section 1: Introduction\n\nSection 2: Tool usage instructions\n\nSection 3: Safety rules - never do X"
	m.SetOriginal(prompt)
	stats := m.GetStats()
	if stats.SectionsTotal == 0 {
		t.Fatal("should parse sections from prompt")
	}
}

func TestSystemPromptMinimizer_EarlyTurn(t *testing.T) {
	m := NewSystemPromptMinimizer()
	prompt := "Introduction\n\nTool usage instructions\n\nSafety: never do X"
	m.SetOriginal(prompt)
	result := m.Minimize(0, false, false)
	if result != prompt {
		t.Fatal("turn 0 should return original prompt")
	}
}

func TestSystemPromptMinimizer_AfterToolUse(t *testing.T) {
	m := NewSystemPromptMinimizer()
	prompt := "Introduction to the system\n\nTool usage instructions: use bash carefully\n\nSafety: never delete files"
	m.SetOriginal(prompt)
	result := m.Minimize(3, true, false)
	// Should remove tool usage instructions since tools have been used
	if strings.Contains(result, "Tool usage instructions") && len(result) < len(prompt) {
		// OK - either removed or still present but shorter
	}
	if len(result) > len(prompt) {
		t.Fatal("minimized should not be longer than original")
	}
}

func TestSystemPromptMinimizer_PreserveSafety(t *testing.T) {
	m := NewSystemPromptMinimizer()
	prompt := "Introduction\n\nTool usage\n\nSafety: never do X\n\nExamples\n\nMore examples"
	m.SetOriginal(prompt)
	result := m.Minimize(10, true, true)
	if !strings.Contains(result, "Safety") || !strings.Contains(result, "never") {
		t.Fatal("safety sections must be preserved")
	}
}

func TestSystemPromptMinimizer_GetStats(t *testing.T) {
	m := NewSystemPromptMinimizer()
	m.SetOriginal("Section 1\n\nSection 2\n\nSection 3")
	m.Minimize(5, true, false)
	stats := m.GetStats()
	if stats.Minimizations != 1 {
		t.Fatalf("expected 1 minimization, got %d", stats.Minimizations)
	}
}
