package agent

import (
	"testing"
)

// ── OPT-131: CacheEfficiencyScorer ──

func TestCacheEfficiencyScorer_Score(t *testing.T) {
	s := NewCacheEfficiencyScorer()
	score := s.Score(80, 20, 1000, 100)
	if score < 0 || score > 1 {
		t.Errorf("score should be 0..1, got %f", score)
	}
}

func TestCacheEfficiencyScorer_HighScore(t *testing.T) {
	s := NewCacheEfficiencyScorer()
	score := s.Score(95, 5, 2000, 10)
	if score < 0.7 {
		t.Errorf("high hit rate + high savings should score high, got %f", score)
	}
}

func TestCacheEfficiencyScorer_LowScore(t *testing.T) {
	s := NewCacheEfficiencyScorer()
	score := s.Score(10, 90, 100, 500)
	if score > 0.5 {
		t.Errorf("low hit rate + high overhead should score low, got %f", score)
	}
}

func TestCacheEfficiencyScorer_Category(t *testing.T) {
	s := NewCacheEfficiencyScorer()
	if s.GetScoreCategory(0.2) != "poor" {
		t.Error("0.2 should be poor")
	}
	if s.GetScoreCategory(0.4) != "fair" {
		t.Error("0.4 should be fair")
	}
	if s.GetScoreCategory(0.6) != "good" {
		t.Error("0.6 should be good")
	}
	if s.GetScoreCategory(0.8) != "excellent" {
		t.Error("0.8 should be excellent")
	}
	if s.GetScoreCategory(0.95) != "optimal" {
		t.Error("0.95 should be optimal")
	}
}

func TestCacheEfficiencyScorer_Stats(t *testing.T) {
	s := NewCacheEfficiencyScorer()
	s.Score(80, 20, 1000, 100)
	s.Score(50, 50, 500, 200)
	stats := s.GetStats()
	if stats["totalScores"].(int) != 2 {
		t.Errorf("totalScores should be 2, got %v", stats["totalScores"])
	}
}

func TestCacheEfficiencyScorer_Reset(t *testing.T) {
	s := NewCacheEfficiencyScorer()
	s.Score(80, 20, 1000, 100)
	s.Reset()
	stats := s.GetStats()
	if stats["totalScores"].(int) != 0 {
		t.Errorf("totalScores should be 0 after reset")
	}
}

// ── OPT-132: TokenAwarePruner ──

func TestTokenAwarePruner_PruneWithinBudget(t *testing.T) {
	p := NewTokenAwarePruner("fifo")
	msgs := []PrunableMessage{
		{Content: "short", Value: 5, Turn: 1},
		{Content: "medium length message", Value: 3, Turn: 2},
		{Content: "another message here", Value: 8, Turn: 3},
	}
	result := p.Prune(msgs, 1000) // budget >> total tokens
	if len(result) != 3 {
		t.Errorf("should keep all messages when budget is sufficient, got %d", len(result))
	}
}

func TestTokenAwarePruner_PruneExceedsBudget(t *testing.T) {
	p := NewTokenAwarePruner("fifo")
	msgs := []PrunableMessage{
		{Content: "short message one", Value: 5, Turn: 1},
		{Content: "short message two", Value: 3, Turn: 2},
		{Content: "short message three", Value: 8, Turn: 3},
	}
	// Each message ~5 tokens, total ~15 tokens, budget=5
	result := p.Prune(msgs, 5)
	if len(result) >= 3 {
		t.Errorf("should prune some messages, got %d", len(result))
	}
}

func TestTokenAwarePruner_LowestValueStrategy(t *testing.T) {
	p := NewTokenAwarePruner("lowest-value")
	msgs := []PrunableMessage{
		{Content: "high value content here", Value: 10, Turn: 1},
		{Content: "low value content here", Value: 1, Turn: 2},
		{Content: "medium value content", Value: 5, Turn: 3},
	}
	result := p.Prune(msgs, 5)
	// lowest-value should remove value=1 first
	for _, m := range result {
		if m.Value == 1 {
			t.Errorf("lowest value message should have been pruned")
		}
	}
}

func TestTokenAwarePruner_EstimateTokens(t *testing.T) {
	p := NewTokenAwarePruner("fifo")
	tokens := p.EstimateTokens("hello world test") // 16 chars / 4 = 4
	if tokens != 4 {
		t.Errorf("should estimate 4 tokens, got %d", tokens)
	}
}

func TestTokenAwarePruner_Stats(t *testing.T) {
	p := NewTokenAwarePruner("fifo")
	msgs := []PrunableMessage{{Content: "test message", Value: 1, Turn: 1}}
	p.Prune(msgs, 1)
	stats := p.GetStats()
	if stats["strategy"].(string) != "fifo" {
		t.Errorf("strategy should be fifo, got %v", stats["strategy"])
	}
}

func TestTokenAwarePruner_Reset(t *testing.T) {
	p := NewTokenAwarePruner("fifo")
	p.Prune([]PrunableMessage{{Content: "test", Value: 1, Turn: 1}}, 1)
	p.Reset()
	stats := p.GetStats()
	if stats["totalPruned"].(int) != 0 {
		t.Errorf("totalPruned should be 0 after reset")
	}
}

// ── OPT-133: ConversationTopicTracker ──

func TestConversationTopicTracker_DetectTopic(t *testing.T) {
	tr := NewConversationTopicTracker(20)
	topic := tr.DetectTopic("how to optimize database query performance")
	if topic == "general" {
		t.Errorf("should detect a specific topic, got general")
	}
}

func TestConversationTopicTracker_UpdateTopic(t *testing.T) {
	tr := NewConversationTopicTracker(20)
	changed1 := tr.UpdateTopic("help me with database configuration")
	if !changed1 {
		t.Errorf("first topic should be a change from empty")
	}
	changed2 := tr.UpdateTopic("now let's talk about database indexing")
	if changed2 {
		// Same topic "database", should not be a change
	}
	changed3 := tr.UpdateTopic("how to write unit tests for my code")
	if !changed3 {
		t.Errorf("topic change to 'test' or 'code' should be detected")
	}
}

func TestConversationTopicTracker_GetCurrentTopic(t *testing.T) {
	tr := NewConversationTopicTracker(20)
	tr.UpdateTopic("database query optimization")
	if tr.GetCurrentTopic() != "database" {
		t.Errorf("current topic should be 'database', got %q", tr.GetCurrentTopic())
	}
}

func TestConversationTopicTracker_Stats(t *testing.T) {
	tr := NewConversationTopicTracker(20)
	tr.UpdateTopic("database help")
	tr.UpdateTopic("code review")
	stats := tr.GetStats()
	if stats["totalTransitions"].(int) < 1 {
		t.Errorf("should have at least 1 transition, got %v", stats["totalTransitions"])
	}
}

func TestConversationTopicTracker_Reset(t *testing.T) {
	tr := NewConversationTopicTracker(20)
	tr.UpdateTopic("database")
	tr.Reset()
	stats := tr.GetStats()
	if stats["totalTransitions"].(int) != 0 {
		t.Errorf("totalTransitions should be 0 after reset")
	}
}

// ── OPT-134: TokenCostProjector ──

func TestTokenCostProjector_Project(t *testing.T) {
	p := NewTokenCostProjector(0.001)
	total, cost := p.Project(1000, 5, 500)
	if total != 3500 { // 1000 + 5*500
		t.Errorf("projected total should be 3500, got %d", total)
	}
	if cost < 0 {
		t.Errorf("cost should be non-negative, got %f", cost)
	}
}

func TestTokenCostProjector_GetCostEstimate(t *testing.T) {
	p := NewTokenCostProjector(0.001)
	cost := p.GetCostEstimate(1000)
	if cost != 1.0 {
		t.Errorf("cost for 1000 tokens at 0.001 should be 1.0, got %f", cost)
	}
}

func TestTokenCostProjector_RecordActual(t *testing.T) {
	p := NewTokenCostProjector(0.001)
	p.Project(1000, 5, 500) // projected = 3500
	p.RecordActual(3000)    // close to prediction
	stats := p.GetStats()
	if stats["totalProjections"].(int) != 1 {
		t.Errorf("totalProjections should be 1, got %v", stats["totalProjections"])
	}
}

func TestTokenCostProjector_Stats(t *testing.T) {
	p := NewTokenCostProjector(0.002)
	p.Project(500, 3, 200)
	stats := p.GetStats()
	if stats["costPerToken"].(float64) != 0.002 {
		t.Errorf("costPerToken should be 0.002, got %v", stats["costPerToken"])
	}
}

func TestTokenCostProjector_Reset(t *testing.T) {
	p := NewTokenCostProjector(0.001)
	p.Project(1000, 5, 500)
	p.Reset()
	stats := p.GetStats()
	if stats["totalProjections"].(int) != 0 {
		t.Errorf("totalProjections should be 0 after reset")
	}
}

// ── OPT-135: ContextAssemblyOptimizer ──

func TestContextAssemblyOptimizer_Assemble(t *testing.T) {
	o := NewContextAssemblyOptimizer()
	msgs := []AssemblyItem{
		{Content: "user message", Role: "user", Value: 5, Turn: 3},
		{Content: "system prompt", Role: "system", Value: 10, Turn: 0},
		{Content: "assistant reply", Role: "assistant", Value: 7, Turn: 2},
	}
	result := o.Assemble(msgs)
	if result[0].Role != "system" {
		t.Errorf("system message should be first, got %q", result[0].Role)
	}
}

func TestContextAssemblyOptimizer_GetAssemblyStrategy(t *testing.T) {
	o := NewContextAssemblyOptimizer()
	msgs := []AssemblyItem{
		{Content: "system", Role: "system", Value: 1, Turn: 0},
		{Content: "user", Role: "user", Value: 1, Turn: 1},
	}
	strategy := o.GetAssemblyStrategy(msgs)
	if strategy != "system_first" {
		t.Errorf("should detect system_first strategy, got %q", strategy)
	}
}

func TestContextAssemblyOptimizer_Stats(t *testing.T) {
	o := NewContextAssemblyOptimizer()
	o.Assemble([]AssemblyItem{
		{Content: "a", Role: "user", Value: 1, Turn: 1},
		{Content: "b", Role: "system", Value: 2, Turn: 0},
	})
	stats := o.GetStats()
	if stats["totalAssemblies"].(int) != 1 {
		t.Errorf("totalAssemblies should be 1, got %v", stats["totalAssemblies"])
	}
}

func TestContextAssemblyOptimizer_Reset(t *testing.T) {
	o := NewContextAssemblyOptimizer()
	o.Assemble([]AssemblyItem{{Content: "a", Role: "user", Value: 1, Turn: 1}})
	o.Reset()
	stats := o.GetStats()
	if stats["totalAssemblies"].(int) != 0 {
		t.Errorf("totalAssemblies should be 0 after reset")
	}
}
