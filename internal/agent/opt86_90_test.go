package agent

import (
	"testing"

	"reasonix/internal/provider"
)

// ── OPT-86: CacheInvalidationTracker ──

func TestCacheInvalidationTracker_Record(t *testing.T) {
	tr := NewCacheInvalidationTracker()
	tr.RecordInvalidation(5, "prefix_changed", 5000, "hash1")
	tr.RecordInvalidation(10, "prefix_changed", 3000, "hash2")
	tr.RecordInvalidation(15, "system_prompt_edited", 2000, "hash3")
	stats := tr.GetStats()
	if stats.TotalInvalidations != 3 {
		t.Fatalf("expected 3 invalidations, got %d", stats.TotalInvalidations)
	}
}

func TestCacheInvalidationTracker_GetTopCauses(t *testing.T) {
	tr := NewCacheInvalidationTracker()
	tr.RecordInvalidation(1, "prefix_changed", 1000, "h1")
	tr.RecordInvalidation(2, "prefix_changed", 2000, "h2")
	tr.RecordInvalidation(3, "tools_modified", 3000, "h3")
	top := tr.GetTopCauses(2)
	if len(top) != 2 {
		t.Fatalf("expected 2 causes, got %d", len(top))
	}
	if top[0].Cause != "prefix_changed" {
		t.Fatalf("expected prefix_changed first, got %s", top[0].Cause)
	}
}

// ── OPT-87: TokenCostAnalyzer ──

func TestTokenCostAnalyzer_RecordUsage(t *testing.T) {
	a := NewTokenCostAnalyzer(1.0)
	a.RecordUsage(10000, 2000, 8000)
	stats := a.GetStats()
	if stats.TotalInputTokens != 10000 {
		t.Fatalf("expected 10000 input, got %d", stats.TotalInputTokens)
	}
}

func TestTokenCostAnalyzer_AnalyzeCosts(t *testing.T) {
	a := NewTokenCostAnalyzer(1.0)
	a.RecordUsage(10000, 2000, 8000)
	analysis := a.AnalyzeCosts()
	if analysis.TotalCost <= 0 {
		t.Fatal("should have positive cost")
	}
}

func TestTokenCostAnalyzer_GetStats(t *testing.T) {
	a := NewTokenCostAnalyzer(2.0)
	a.RecordUsage(1000, 500, 200)
	stats := a.GetStats()
	if stats.TotalInputTokens != 1000 {
		t.Fatalf("expected 1000, got %d", stats.TotalInputTokens)
	}
}

// ── OPT-88: MessageImportanceScorer ──

func TestMessageImportanceScorer_ScoreMessage(t *testing.T) {
	s := NewMessageImportanceScorer()
	msg := provider.Message{Role: provider.RoleSystem, Content: "system prompt"}
	score := s.ScoreMessage(msg, 0, 10, false)
	if score.Score < 0.9 {
		t.Fatalf("system message should have high score, got %f", score.Score)
	}
	if !score.Keep {
		t.Fatal("system message should be kept")
	}
}

func TestMessageImportanceScorer_ScoreMessages(t *testing.T) {
	s := NewMessageImportanceScorer()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "q1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "q2"},
	}
	scores := s.ScoreMessages(msgs)
	if len(scores) != len(msgs) {
		t.Fatalf("expected %d scores, got %d", len(msgs), len(scores))
	}
}

func TestMessageImportanceScorer_GetDropCandidates(t *testing.T) {
	s := NewMessageImportanceScorer()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleTool, Content: "very long old tool result that is not important"},
		{Role: provider.RoleUser, Content: "recent query"},
	}
	scores := s.ScoreMessages(msgs)
	dropIndices := s.GetDropCandidates(scores)
	// System and recent user should not be droppable
	for _, idx := range dropIndices {
		if msgs[idx].Role == provider.RoleSystem {
			t.Fatal("system message should not be a drop candidate")
		}
	}
}

func TestMessageImportanceScorer_GetStats(t *testing.T) {
	s := NewMessageImportanceScorer()
	s.ScoreMessages([]provider.Message{{Role: provider.RoleUser, Content: "q"}})
	stats := s.GetStats()
	if stats.TotalScored == 0 {
		t.Fatal("should have stats")
	}
}

// ── OPT-89: ContextCoherenceChecker ──

func TestContextCoherenceChecker_Coherent(t *testing.T) {
	c := NewContextCoherenceChecker()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "what is 2+2?"},
		{Role: provider.RoleAssistant, Content: "2+2 is 4."},
		{Role: provider.RoleUser, Content: "thank you"},
	}
	report := c.CheckCoherence(msgs)
	if !report.IsCoherent {
		t.Fatal("normal conversation should be coherent")
	}
}

func TestContextCoherenceChecker_OrphanedToolResult(t *testing.T) {
	c := NewContextCoherenceChecker()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleTool, Content: "tool result without preceding call"},
		{Role: provider.RoleUser, Content: "thanks"},
	}
	report := c.CheckCoherence(msgs)
	if report.IsCoherent {
		t.Fatal("orphaned tool result should be incoherent")
	}
}

func TestContextCoherenceChecker_GetStats(t *testing.T) {
	c := NewContextCoherenceChecker()
	c.CheckCoherence([]provider.Message{{Role: provider.RoleUser, Content: "q"}})
	stats := c.GetStats()
	if stats.TotalChecks != 1 {
		t.Fatalf("expected 1 check, got %d", stats.TotalChecks)
	}
}

// ── OPT-90: AdaptiveMessageSelector ──

func TestAdaptiveMessageSelector_SelectMessages(t *testing.T) {
	s := NewAdaptiveMessageSelector()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "q1"},
		{Role: provider.RoleAssistant, Content: "a1"},
		{Role: provider.RoleUser, Content: "q2"},
		{Role: provider.RoleAssistant, Content: "a2"},
	}
	result := s.SelectMessages(msgs, 100000) // Large budget
	if len(result) != len(msgs) {
		t.Fatalf("with large budget should keep all, got %d vs %d", len(result), len(msgs))
	}
}

func TestAdaptiveMessageSelector_TightBudget(t *testing.T) {
	s := NewAdaptiveMessageSelector()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "q1 with some content"},
		{Role: provider.RoleAssistant, Content: "a1 with some response"},
		{Role: provider.RoleUser, Content: "q2 with more content"},
		{Role: provider.RoleAssistant, Content: "a2 with more response"},
	}
	result := s.SelectMessages(msgs, 20) // Very tight budget (20 tokens)
	// Should keep at least system + last 2
	if len(result) < 3 {
		t.Fatalf("should keep at least 3 messages, got %d", len(result))
	}
}

func TestAdaptiveMessageSelector_SetStrategy(t *testing.T) {
	s := NewAdaptiveMessageSelector()
	s.SetStrategy("aggressive")
	stats := s.GetStats()
	if stats.Strategy != "aggressive" {
		t.Fatalf("expected aggressive, got %s", stats.Strategy)
	}
}

func TestAdaptiveMessageSelector_EstimateTokens(t *testing.T) {
	s := NewAdaptiveMessageSelector()
	tokens := s.EstimateMessageTokens(provider.Message{Content: "hello world test"})
	if tokens != 4 { // 16/4
		t.Fatalf("expected 4 tokens, got %d", tokens)
	}
}

func TestAdaptiveMessageSelector_GetStats(t *testing.T) {
	s := NewAdaptiveMessageSelector()
	s.SelectMessages([]provider.Message{{Role: provider.RoleUser, Content: "q"}}, 10000)
	stats := s.GetStats()
	if stats.TotalSelections == 0 {
		t.Fatal("should have stats")
	}
}
