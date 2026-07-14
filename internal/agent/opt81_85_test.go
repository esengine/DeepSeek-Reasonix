package agent

import (
	"testing"

	"reasonix/internal/provider"
)

// ── OPT-81: HistoryWindowManager ──

func TestHistoryWindowManager_NoPrune(t *testing.T) {
	m := NewHistoryWindowManager(50000)
	if m.ShouldPrune(30000) {
		t.Fatal("should not prune at 60% usage")
	}
}

func TestHistoryWindowManager_ShouldPrune(t *testing.T) {
	m := NewHistoryWindowManager(50000)
	if !m.ShouldPrune(45000) {
		t.Fatal("should prune at 90% usage")
	}
}

func TestHistoryWindowManager_ManageWindow(t *testing.T) {
	m := NewHistoryWindowManager(100) // Very small to trigger pruning
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
	}
	for i := 0; i < 20; i++ {
		msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: "message"})
	}
	result := m.ManageWindow(msgs, 200)
	if len(result) >= len(msgs) {
		t.Fatal("should prune messages when over limit")
	}
}

func TestHistoryWindowManager_GetOptimalWindowSize(t *testing.T) {
	m := NewHistoryWindowManager(50000)
	optimal := m.GetOptimalWindowSize()
	if optimal != 35000 {
		t.Fatalf("expected 35000, got %d", optimal)
	}
}

func TestHistoryWindowManager_GetStats(t *testing.T) {
	m := NewHistoryWindowManager(50000)
	m.ManageWindow([]provider.Message{{Role: provider.RoleUser, Content: "q"}}, 100)
	stats := m.GetStats()
	_ = stats
}

// ── OPT-82: TokenAwareRetry ──

func TestTokenAwareRetry_ShouldRetry(t *testing.T) {
	r := NewTokenAwareRetry(3)
	if !r.ShouldRetry(1, "timeout") {
		t.Fatal("should retry on timeout")
	}
	if r.ShouldRetry(4, "timeout") {
		t.Fatal("should not retry after max retries")
	}
	if r.ShouldRetry(1, "syntax_error") {
		t.Fatal("should not retry non-retryable error")
	}
}

func TestTokenAwareRetry_CalculateTokens(t *testing.T) {
	r := NewTokenAwareRetry(3)
	if tokens := r.CalculateRetryTokens(10000, 1); tokens != 8000 {
		t.Fatalf("expected 8000, got %d", tokens)
	}
	if tokens := r.CalculateRetryTokens(10000, 2); tokens != 6000 {
		t.Fatalf("expected 6000, got %d", tokens)
	}
	if tokens := r.CalculateRetryTokens(10000, 3); tokens != 4000 {
		t.Fatalf("expected 4000, got %d", tokens)
	}
}

func TestTokenAwareRetry_RecordRetry(t *testing.T) {
	r := NewTokenAwareRetry(3)
	r.RecordRetry(1, "timeout", 8000, 2000, true)
	stats := r.GetStats()
	if stats.TotalRetries != 1 {
		t.Fatalf("expected 1 retry, got %d", stats.TotalRetries)
	}
}

// ── OPT-83: CompactionTriggerV2 ──

func TestCompactionTriggerV2_HighPriority(t *testing.T) {
	c := NewCompactionTriggerV2()
	decision := c.Evaluate(115000, 128000, 60, 40, 0.6)
	if !decision.ShouldCompact {
		t.Fatal("should compact when multiple signals triggered")
	}
}

func TestCompactionTriggerV2_NoCompaction(t *testing.T) {
	c := NewCompactionTriggerV2()
	decision := c.Evaluate(30000, 128000, 10, 2, 0.1)
	if decision.ShouldCompact {
		t.Fatal("should not compact when no signals triggered")
	}
}

func TestCompactionTriggerV2_GetStats(t *testing.T) {
	c := NewCompactionTriggerV2()
	c.Evaluate(115000, 128000, 60, 40, 0.6)
	stats := c.GetStats()
	_ = stats
}

// ── OPT-84: ModelAwareOptimizer ──

func TestModelAwareOptimizer_DeepSeek(t *testing.T) {
	o := NewModelAwareOptimizer("deepseek-chat")
	cfg := o.GetModelConfig()
	if cfg.ContextWindow != 128000 {
		t.Fatalf("expected 128000, got %d", cfg.ContextWindow)
	}
	if !cfg.SupportsCache {
		t.Fatal("deepseek should support cache")
	}
}

func TestModelAwareOptimizer_OptimizeForModel(t *testing.T) {
	o := NewModelAwareOptimizer("deepseek-chat")
	result := o.OptimizeForModel(10000, 15)
	if result.RecommendedMaxTokens <= 0 {
		t.Fatal("should return positive max tokens")
	}
}

func TestModelAwareOptimizer_SetModel(t *testing.T) {
	o := NewModelAwareOptimizer("deepseek-chat")
	o.SetModel("claude-sonnet")
	cfg := o.GetModelConfig()
	if cfg.ContextWindow != 200000 {
		t.Fatalf("expected 200000 for claude, got %d", cfg.ContextWindow)
	}
}

func TestModelAwareOptimizer_GetStats(t *testing.T) {
	o := NewModelAwareOptimizer("deepseek-chat")
	o.OptimizeForModel(10000, 15)
	stats := o.GetStats()
	if stats.ModelName != "deepseek-chat" {
		t.Fatalf("expected deepseek-chat, got %s", stats.ModelName)
	}
}

// ── OPT-85: TokenUsagePredictor ──

func TestTokenUsagePredictor_RecordUsage(t *testing.T) {
	p := NewTokenUsagePredictor()
	p.RecordUsage(1, 10000)
	p.RecordUsage(2, 12000)
	p.RecordUsage(3, 15000)
	stats := p.GetStats()
	if stats.HistorySize != 3 {
		t.Fatalf("expected 3 records, got %d", stats.HistorySize)
	}
}

func TestTokenUsagePredictor_PredictNextTurn(t *testing.T) {
	p := NewTokenUsagePredictor()
	p.RecordUsage(1, 10000)
	p.RecordUsage(2, 12000)
	predicted := p.PredictNextTurn(2, 12000)
	if predicted <= 0 {
		t.Fatal("predicted tokens should be positive")
	}
}

func TestTokenUsagePredictor_PredictInNTurns(t *testing.T) {
	p := NewTokenUsagePredictor()
	p.RecordUsage(1, 10000)
	p.RecordUsage(2, 12000)
	predicted := p.PredictInNTurns(2, 12000, 5)
	if predicted <= 0 {
		t.Fatal("predicted tokens should be positive")
	}
}

func TestTokenUsagePredictor_GetStats(t *testing.T) {
	p := NewTokenUsagePredictor()
	p.RecordUsage(1, 10000)
	stats := p.GetStats()
	if stats.HistorySize != 1 {
		t.Fatalf("expected 1 record, got %d", stats.HistorySize)
	}
}
