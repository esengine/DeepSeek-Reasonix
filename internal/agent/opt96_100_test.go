package agent

import (
	"testing"

	"reasonix/internal/provider"
)

// ── OPT-96: CacheWarmingV2 ──

func TestCacheWarmingV2_LearnPattern(t *testing.T) {
	c := NewCacheWarmingV2()
	c.LearnPattern("how to read file", "how to edit file")
	c.LearnPattern("how to read file", "how to edit file")
	c.LearnPattern("how to read file", "how to edit file")
	followUp := c.PredictFollowUp("how to read file")
	if followUp == "" {
		t.Fatal("should predict follow-up after learning pattern")
	}
}

func TestCacheWarmingV2_WarmCache(t *testing.T) {
	c := NewCacheWarmingV2()
	c.LearnPattern("query1", "query2")
	c.LearnPattern("query1", "query2")
	c.LearnPattern("query1", "query2")
	called := false
	warmed := c.WarmCache("query1", func(predicted string) {
		called = true
	})
	if !warmed {
		t.Fatal("should warm cache for frequent pattern")
	}
	if !called {
		t.Fatal("should call prepare function")
	}
}

func TestCacheWarmingV2_GetStats(t *testing.T) {
	c := NewCacheWarmingV2()
	c.LearnPattern("q1", "q2")
	stats := c.GetStats()
	if stats.PatternsLearned == 0 {
		t.Fatal("should have learned patterns")
	}
}

// ── OPT-97: TokenEfficiencyDashboard ──

func TestTokenEfficiencyDashboard_Refresh(t *testing.T) {
	d := NewTokenEfficiencyDashboard()
	allStats := map[string]interface{}{
		"opt01_sliding":     map[string]interface{}{"TokensSaved": 1000},
		"opt17_conversation": map[string]interface{}{"TotalDeduped": 5},
	}
	view := d.Refresh(allStats)
	if view.TotalModules != 2 {
		t.Fatalf("expected 2 modules, got %d", view.TotalModules)
	}
}

func TestTokenEfficiencyDashboard_GetStats(t *testing.T) {
	d := NewTokenEfficiencyDashboard()
	d.Refresh(map[string]interface{}{"opt01": map[string]interface{}{}})
	stats := d.GetStats()
	if stats.TotalModules == 0 {
		t.Fatal("should have modules after refresh")
	}
}

// ── OPT-98: ConversationTokenBudget ──

func TestConversationTokenBudget_Allocate(t *testing.T) {
	b := NewConversationTokenBudget(100000)
	if !b.Allocate(1, 30000) {
		t.Fatal("should allocate within budget")
	}
	if b.GetRemaining() != 70000 {
		t.Fatalf("expected 70000 remaining, got %d", b.GetRemaining())
	}
}

func TestConversationTokenBudget_OverBudget(t *testing.T) {
	b := NewConversationTokenBudget(10000)
	b.Allocate(1, 8000)
	b.Allocate(2, 8000)
	b.Allocate(3, 8000) // 24000 > 10000
	stats := b.GetStats()
	if stats.OverBudgetCount == 0 {
		t.Fatal("should track over-budget allocations")
	}
}

func TestConversationTokenBudget_ReserveRelease(t *testing.T) {
	b := NewConversationTokenBudget(100000)
	if !b.Reserve(20000) {
		t.Fatal("should reserve within budget")
	}
	b.Release(10000)
	remaining := b.GetRemaining()
	if remaining < 80000 || remaining > 90000 {
		t.Fatalf("expected ~90000 remaining after release, got %d", remaining)
	}
}

func TestConversationTokenBudget_ShouldEnd(t *testing.T) {
	b := NewConversationTokenBudget(10000)
	b.Allocate(1, 9500) // 95% used
	if !b.ShouldEndConversation() {
		t.Fatal("should end conversation when < 10% remaining")
	}
}

func TestConversationTokenBudget_GetStats(t *testing.T) {
	b := NewConversationTokenBudget(100000)
	b.Allocate(1, 50000)
	stats := b.GetStats()
	if stats.TotalBudget != 100000 {
		t.Fatalf("expected 100000, got %d", stats.TotalBudget)
	}
}

// ── OPT-99: SmartContextPruner ──

func TestSmartContextPruner_PruneContext(t *testing.T) {
	p := NewSmartContextPruner()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
	}
	for i := 0; i < 20; i++ {
		msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: "message content that is moderately long"})
	}
	pruned, decisions := p.PruneContext(msgs, 50) // Very tight budget
	if len(pruned) >= len(msgs) {
		t.Fatal("should prune messages")
	}
	_ = decisions
}

func TestSmartContextPruner_ScorePruneCandidate(t *testing.T) {
	p := NewSmartContextPruner()
	// Old tool message should have high prune score
	score := p.ScorePruneCandidate(provider.Message{Role: provider.RoleTool, Content: "old result"}, 5, 20, 10)
	if score < 0 || score > 1 {
		t.Fatalf("score should be 0-1, got %f", score)
	}
	// System message should have low prune score
	sysScore := p.ScorePruneCandidate(provider.Message{Role: provider.RoleSystem, Content: "system"}, 0, 20, 0)
	if sysScore > 0.5 {
		t.Fatalf("system message should have low prune score, got %f", sysScore)
	}
}

func TestSmartContextPruner_GetStats(t *testing.T) {
	p := NewSmartContextPruner()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "q"},
	}
	p.PruneContext(msgs, 10000)
	stats := p.GetStats()
	_ = stats
}

// ── OPT-100: UnifiedTokenOrchestrator ──

func TestUnifiedTokenOrchestrator_RegisterModule(t *testing.T) {
	o := NewUnifiedTokenOrchestrator()
	o.RegisterModule("opt01_sliding")
	o.RegisterModule("opt17_conversation")
	modules := o.GetActiveModules()
	if len(modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(modules))
	}
}

func TestUnifiedTokenOrchestrator_Orchestrate(t *testing.T) {
	o := NewUnifiedTokenOrchestrator()
	o.RegisterModule("opt01_sliding")
	o.RegisterModule("opt83_compactionTriggerV2")
	ctx := OrchestrationContext{
		PromptTokens:     100000,
		CompletionTokens: 5000,
		CacheHitTokens:   30000,
		CacheMissTokens:  70000,
		MessageCount:     50,
		Turn:             10,
		ContextWindow:    128000,
	}
	result := o.Orchestrate(ctx)
	if result.ModulesConsulted == 0 {
		t.Fatal("should consult modules")
	}
	if len(result.RecommendedActions) == 0 {
		t.Fatal("should recommend actions")
	}
}

func TestUnifiedTokenOrchestrator_SetStrategy(t *testing.T) {
	o := NewUnifiedTokenOrchestrator()
	o.SetStrategy("aggressive")
	stats := o.GetStats()
	if stats.Strategy != "aggressive" {
		t.Fatalf("expected aggressive, got %s", stats.Strategy)
	}
}

func TestUnifiedTokenOrchestrator_GetStats(t *testing.T) {
	o := NewUnifiedTokenOrchestrator()
	o.RegisterModule("opt01")
	o.Orchestrate(OrchestrationContext{PromptTokens: 1000, ContextWindow: 128000})
	stats := o.GetStats()
	if stats.Orchestrations != 1 {
		t.Fatalf("expected 1 orchestration, got %d", stats.Orchestrations)
	}
}
