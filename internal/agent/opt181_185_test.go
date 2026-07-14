package agent

import "testing"

// ── OPT-181: TokenAwareSchedulerV2 Tests ──

func TestSchedulerV2_HighPriorityFirst(t *testing.T) {
	s := NewTokenAwareSchedulerV2()
	s.AddTask("low", 1, 100)
	s.AddTask("high", 5, 200)
	s.AddTask("mid", 3, 50)

	task, ok := s.ScheduleNext()
	if !ok {
		t.Fatalf("expected task to be scheduled, got false")
	}
	if task.ID != "high" {
		t.Errorf("expected high priority task 'high', got %s", task.ID)
	}
	if task.Status != "scheduled" {
		t.Errorf("expected status 'scheduled', got %s", task.Status)
	}
}

func TestSchedulerV2_SamePrioritySmallerTokenFirst(t *testing.T) {
	s := NewTokenAwareSchedulerV2()
	s.AddTask("a", 5, 200)
	s.AddTask("b", 5, 100)
	s.AddTask("c", 5, 300)

	task, ok := s.ScheduleNext()
	if !ok {
		t.Fatalf("expected task to be scheduled, got false")
	}
	if task.ID != "b" {
		t.Errorf("expected smallest token budget task 'b', got %s", task.ID)
	}
}

func TestSchedulerV2_EmptyQueueReturnsFalse(t *testing.T) {
	s := NewTokenAwareSchedulerV2()
	task, ok := s.ScheduleNext()
	if ok {
		t.Errorf("expected false for empty queue, got true")
	}
	if task.ID != "" {
		t.Errorf("expected empty task ID, got %s", task.ID)
	}
}

func TestSchedulerV2_StatsScheduledCount(t *testing.T) {
	s := NewTokenAwareSchedulerV2()
	s.AddTask("a", 1, 100)
	s.AddTask("b", 2, 200)
	s.AddTask("c", 3, 300)

	s.ScheduleNext() // c (priority 3)
	s.ScheduleNext() // b (priority 2)

	stats := s.GetStats()
	scheduledCount := stats["scheduledCount"].(int)
	pendingCount := stats["pendingCount"].(int)
	totalTokens := stats["totalTokensScheduled"].(int)

	if scheduledCount != 2 {
		t.Errorf("expected scheduledCount 2, got %d", scheduledCount)
	}
	if pendingCount != 1 {
		t.Errorf("expected pendingCount 1, got %d", pendingCount)
	}
	// scheduled: c (300 tokens) + b (200 tokens) = 500
	if totalTokens != 500 {
		t.Errorf("expected totalTokensScheduled 500, got %d", totalTokens)
	}
}

func TestSchedulerV2_GetPendingCount(t *testing.T) {
	s := NewTokenAwareSchedulerV2()
	s.AddTask("a", 1, 100)
	s.AddTask("b", 2, 200)

	if s.GetPendingCount() != 2 {
		t.Errorf("expected pending count 2, got %d", s.GetPendingCount())
	}
	s.ScheduleNext()
	if s.GetPendingCount() != 1 {
		t.Errorf("expected pending count 1 after schedule, got %d", s.GetPendingCount())
	}
}

func TestSchedulerV2_Reset(t *testing.T) {
	s := NewTokenAwareSchedulerV2()
	s.AddTask("a", 1, 100)
	s.AddTask("b", 2, 200)
	s.ScheduleNext()

	s.Reset()

	stats := s.GetStats()
	if stats["pendingCount"].(int) != 0 {
		t.Errorf("expected pendingCount 0 after reset, got %d", stats["pendingCount"].(int))
	}
	if stats["scheduledCount"].(int) != 0 {
		t.Errorf("expected scheduledCount 0 after reset, got %d", stats["scheduledCount"].(int))
	}
	if stats["totalTokensScheduled"].(int) != 0 {
		t.Errorf("expected totalTokensScheduled 0 after reset, got %d", stats["totalTokensScheduled"].(int))
	}
}

// ── OPT-182: CacheHitPredictorV2 Tests ──

func TestPredictorV2_PredictHitRate(t *testing.T) {
	p := NewCacheHitPredictorV2()
	p.RecordHit("k1", true)
	p.RecordHit("k1", false)
	p.RecordHit("k1", true)

	prob := p.Predict("k1")
	// 2 hits out of 3 = 0.6667
	if prob < 0.66 || prob > 0.67 {
		t.Errorf("expected hit rate ~0.667, got %v", prob)
	}
}

func TestPredictorV2_PredictNoHistoryReturnsZero(t *testing.T) {
	p := NewCacheHitPredictorV2()
	prob := p.Predict("nonexistent")
	// 源码 chpComputeHitRate 对空历史返回 0
	if prob != 0 {
		t.Errorf("expected 0 for no history, got %v", prob)
	}
}

func TestPredictorV2_GetAccuracy(t *testing.T) {
	p := NewCacheHitPredictorV2()
	// All hits -> prediction should be correct
	p.RecordHit("k1", true)
	p.RecordHit("k1", true)
	p.RecordHit("k1", true)

	p.Predict("k1") // prob=1.0, predictedHit=true, actualHit=true -> correct

	acc := p.GetAccuracy()
	if acc != 1.0 {
		t.Errorf("expected accuracy 1.0, got %v", acc)
	}
}

func TestPredictorV2_StatsTrackedKeys(t *testing.T) {
	p := NewCacheHitPredictorV2()
	p.RecordHit("k1", true)
	p.RecordHit("k2", false)
	p.RecordHit("k1", true)

	stats := p.GetStats()
	trackedKeys := stats["trackedKeys"].(int)
	if trackedKeys != 2 {
		t.Errorf("expected trackedKeys 2, got %d", trackedKeys)
	}
}

func TestPredictorV2_Reset(t *testing.T) {
	p := NewCacheHitPredictorV2()
	p.RecordHit("k1", true)
	p.RecordHit("k2", false)
	p.Predict("k1")

	p.Reset()

	stats := p.GetStats()
	if stats["trackedKeys"].(int) != 0 {
		t.Errorf("expected trackedKeys 0 after reset, got %d", stats["trackedKeys"].(int))
	}
	if stats["predictions"].(int) != 0 {
		t.Errorf("expected predictions 0 after reset, got %d", stats["predictions"].(int))
	}
	if stats["correctPredictions"].(int) != 0 {
		t.Errorf("expected correctPredictions 0 after reset, got %d", stats["correctPredictions"].(int))
	}
}

// ── OPT-183: ContextDecayManagerV2 Tests ──

func TestDecayV2_AddGet(t *testing.T) {
	m := NewContextDecayManagerV2(0.5, 5)
	m.Add("k1", "v1", 1.0)

	item, ok := m.Get("k1")
	if !ok {
		t.Fatalf("expected item to exist")
	}
	if item.Key != "k1" {
		t.Errorf("expected key 'k1', got %s", item.Key)
	}
	if item.Value != "v1" {
		t.Errorf("expected value 'v1', got %s", item.Value)
	}
	if item.Age != 0 {
		t.Errorf("expected age 0, got %d", item.Age)
	}
	if item.Importance != 1.0 {
		t.Errorf("expected importance 1.0, got %v", item.Importance)
	}
}

func TestDecayV2_TickIncrementsAge(t *testing.T) {
	m := NewContextDecayManagerV2(0.1, 10)
	m.Add("k1", "v1", 1.0)

	m.Tick()

	item, ok := m.Get("k1")
	if !ok {
		t.Fatalf("expected item to still exist after tick")
	}
	if item.Age != 1 {
		t.Errorf("expected age 1 after tick, got %d", item.Age)
	}
	if item.Importance < 0.89 || item.Importance > 0.91 {
		t.Errorf("expected importance ~0.9 after tick, got %v", item.Importance)
	}
}

func TestDecayV2_TickRemovesExpiredItemByAge(t *testing.T) {
	m := NewContextDecayManagerV2(0.01, 1)
	m.Add("k1", "v1", 1.0)

	// First tick: age=1, importance=0.99 -> age(1) not > maxAge(1), stays
	m.Tick()
	item, ok := m.Get("k1")
	if !ok {
		t.Fatalf("expected item to exist after first tick")
	}
	if item.Age != 1 {
		t.Errorf("expected age 1 after first tick, got %d", item.Age)
	}

	// Second tick: age=2 > maxAge(1) -> removed
	m.Tick()
	_, ok = m.Get("k1")
	if ok {
		t.Errorf("expected item to be removed after exceeding maxAge")
	}
}

func TestDecayV2_StatsItemCount(t *testing.T) {
	m := NewContextDecayManagerV2(0.5, 5)
	m.Add("k1", "v1", 1.0)
	m.Add("k2", "v2", 0.8)

	stats := m.GetStats()
	itemCount := stats["itemCount"].(int)
	if itemCount != 2 {
		t.Errorf("expected itemCount 2, got %d", itemCount)
	}
}

func TestDecayV2_Reset(t *testing.T) {
	m := NewContextDecayManagerV2(0.5, 5)
	m.Add("k1", "v1", 1.0)
	m.Add("k2", "v2", 0.8)
	m.Tick()

	m.Reset()

	stats := m.GetStats()
	if stats["itemCount"].(int) != 0 {
		t.Errorf("expected itemCount 0 after reset, got %d", stats["itemCount"].(int))
	}
	if stats["decayedCount"].(int) != 0 {
		t.Errorf("expected decayedCount 0 after reset, got %d", stats["decayedCount"].(int))
	}
}

// ── OPT-184: TokenAwareGatekeeper Tests ──

func TestGatekeeper_AllowWithinBudget(t *testing.T) {
	g := NewTokenAwareGatekeeper(1000, 0.8)
	if !g.Allow(500) {
		t.Errorf("expected Allow(500) to return true within budget of 1000")
	}
	if !g.Allow(400) {
		t.Errorf("expected Allow(400) to return true within remaining budget of 500")
	}
}

func TestGatekeeper_AllowExceedsBudget(t *testing.T) {
	g := NewTokenAwareGatekeeper(1000, 0.8)
	g.Allow(600)
	if g.Allow(500) {
		t.Errorf("expected Allow(500) to return false when exceeding budget (600+500 > 1000)")
	}
}

func TestGatekeeper_PeekDoesNotConsume(t *testing.T) {
	g := NewTokenAwareGatekeeper(1000, 0.8)
	if !g.Peek(500) {
		t.Errorf("expected Peek(500) to return true")
	}
	remaining := g.GetRemainingBudget()
	if remaining != 1000 {
		t.Errorf("expected remaining budget 1000 after peek (no consumption), got %d", remaining)
	}
}

func TestGatekeeper_GetRemainingBudget(t *testing.T) {
	g := NewTokenAwareGatekeeper(1000, 0.8)
	g.Allow(300)
	remaining := g.GetRemainingBudget()
	if remaining != 700 {
		t.Errorf("expected remaining budget 700 after consuming 300, got %d", remaining)
	}
}

func TestGatekeeper_GetUtilization(t *testing.T) {
	g := NewTokenAwareGatekeeper(1000, 0.8)
	g.Allow(400)
	util := g.GetUtilization()
	if util < 0.39 || util > 0.41 {
		t.Errorf("expected utilization ~0.4, got %v", util)
	}
}

func TestGatekeeper_StatsAndReset(t *testing.T) {
	g := NewTokenAwareGatekeeper(1000, 0.8)
	g.Allow(300)
	g.Allow(200)
	g.Allow(600) // blocked: 300+200+600 = 1100 > 1000

	stats := g.GetStats()
	if stats["allowedCount"].(int) != 2 {
		t.Errorf("expected allowedCount 2, got %d", stats["allowedCount"].(int))
	}
	if stats["blockedCount"].(int) != 1 {
		t.Errorf("expected blockedCount 1, got %d", stats["blockedCount"].(int))
	}
	if stats["consumedTokens"].(int) != 500 {
		t.Errorf("expected consumedTokens 500, got %d", stats["consumedTokens"].(int))
	}

	g.Reset()
	stats = g.GetStats()
	if stats["consumedTokens"].(int) != 0 {
		t.Errorf("expected consumedTokens 0 after reset, got %d", stats["consumedTokens"].(int))
	}
	if stats["allowedCount"].(int) != 0 {
		t.Errorf("expected allowedCount 0 after reset, got %d", stats["allowedCount"].(int))
	}
	if stats["blockedCount"].(int) != 0 {
		t.Errorf("expected blockedCount 0 after reset, got %d", stats["blockedCount"].(int))
	}
	// Budget config should be preserved after reset
	if stats["totalBudget"].(int) != 1000 {
		t.Errorf("expected totalBudget 1000 preserved after reset, got %d", stats["totalBudget"].(int))
	}
}

// ── OPT-185: PromptContextBridge Tests ──

func TestBridge_LinkGetContexts(t *testing.T) {
	b := NewPromptContextBridge()
	b.Link("p1", "c1")
	b.Link("p1", "c2")

	contexts := b.GetContexts("p1")
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(contexts))
	}
	found := map[string]bool{}
	for _, c := range contexts {
		found[c] = true
	}
	if !found["c1"] || !found["c2"] {
		t.Errorf("expected contexts c1 and c2, got %v", contexts)
	}
}

func TestBridge_UnlinkRemovesMapping(t *testing.T) {
	b := NewPromptContextBridge()
	b.Link("p1", "c1")
	b.Link("p1", "c2")

	b.Unlink("p1", "c1")

	contexts := b.GetContexts("p1")
	if len(contexts) != 1 {
		t.Fatalf("expected 1 context after unlink, got %d", len(contexts))
	}
	if contexts[0] != "c2" {
		t.Errorf("expected remaining context 'c2', got %s", contexts[0])
	}
}

func TestBridge_GetPromptsReverseQuery(t *testing.T) {
	b := NewPromptContextBridge()
	b.Link("p1", "c1")
	b.Link("p2", "c1")

	prompts := b.GetPrompts("c1")
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(prompts))
	}
	found := map[string]bool{}
	for _, p := range prompts {
		found[p] = true
	}
	if !found["p1"] || !found["p2"] {
		t.Errorf("expected prompts p1 and p2, got %v", prompts)
	}
}

func TestBridge_StatsBridgeCount(t *testing.T) {
	b := NewPromptContextBridge()
	b.Link("p1", "c1")
	b.Link("p1", "c2")
	b.Link("p2", "c1")

	stats := b.GetStats()
	bridgeCount := stats["bridgeCount"].(int)
	if bridgeCount != 3 {
		t.Errorf("expected bridgeCount 3, got %d", bridgeCount)
	}
}

func TestBridge_DuplicateLinkNotCounted(t *testing.T) {
	b := NewPromptContextBridge()
	b.Link("p1", "c1")
	b.Link("p1", "c1") // duplicate, should not increment bridgeCount

	stats := b.GetStats()
	bridgeCount := stats["bridgeCount"].(int)
	if bridgeCount != 1 {
		t.Errorf("expected bridgeCount 1 for duplicate link, got %d", bridgeCount)
	}
	contexts := b.GetContexts("p1")
	if len(contexts) != 1 {
		t.Errorf("expected 1 context for duplicate link, got %d", len(contexts))
	}
}

func TestBridge_Reset(t *testing.T) {
	b := NewPromptContextBridge()
	b.Link("p1", "c1")
	b.Link("p2", "c2")

	b.Reset()

	stats := b.GetStats()
	if stats["bridgeCount"].(int) != 0 {
		t.Errorf("expected bridgeCount 0 after reset, got %d", stats["bridgeCount"].(int))
	}
	if stats["promptCount"].(int) != 0 {
		t.Errorf("expected promptCount 0 after reset, got %d", stats["promptCount"].(int))
	}
	if stats["contextCount"].(int) != 0 {
		t.Errorf("expected contextCount 0 after reset, got %d", stats["contextCount"].(int))
	}
}
