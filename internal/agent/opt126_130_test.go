package agent

import (
	"testing"
)

// ── OPT-126: TokenUsageForecaster ──

func TestTokenUsageForecaster_EmptyHistory(t *testing.T) {
	f := NewTokenUsageForecaster(50, 5)
	fc := f.Forecast(3)
	if len(fc) != 3 {
		t.Errorf("should return 3 forecasts, got %d", len(fc))
	}
	for _, v := range fc {
		if v != 0 {
			t.Errorf("empty history should forecast 0, got %d", v)
		}
	}
}

func TestTokenUsageForecaster_Predict(t *testing.T) {
	f := NewTokenUsageForecaster(50, 5)
	f.RecordUsage(1000)
	f.RecordUsage(1100)
	f.RecordUsage(1050)
	fc := f.Forecast(1)
	if fc[0] <= 0 {
		t.Errorf("should predict positive usage, got %d", fc[0])
	}
}

func TestTokenUsageForecaster_Evaluate(t *testing.T) {
	f := NewTokenUsageForecaster(50, 5)
	f.RecordUsage(1000)
	f.Forecast(1)
	f.EvaluateForecast(1050) // within 25% of prediction
	stats := f.GetStats()
	if stats["accurateForecasts"].(int) == 0 {
		t.Errorf("should have at least 1 accurate forecast")
	}
}

func TestTokenUsageForecaster_Stats(t *testing.T) {
	f := NewTokenUsageForecaster(50, 5)
	f.RecordUsage(1000)
	f.RecordUsage(1200)
	stats := f.GetStats()
	if stats["historySize"].(int) != 2 {
		t.Errorf("historySize should be 2, got %v", stats["historySize"])
	}
}

func TestTokenUsageForecaster_Reset(t *testing.T) {
	f := NewTokenUsageForecaster(50, 5)
	f.RecordUsage(1000)
	f.Reset()
	stats := f.GetStats()
	if stats["historySize"].(int) != 0 {
		t.Errorf("historySize should be 0 after reset")
	}
}

// ── OPT-127: ContextFreshnessTracker ──

func TestContextFreshnessTracker_TrackAndQuery(t *testing.T) {
	tr := NewContextFreshnessTracker(10)
	tr.TrackMessage(0, 1)
	tr.TrackMessage(1, 3)
	tr.TrackMessage(2, 5)
	tr.UpdateTurn(8)

	fresh := tr.GetFreshMessages()
	if len(fresh) != 3 {
		t.Errorf("all should be fresh (age < 10), got %d", len(fresh))
	}
}

func TestContextFreshnessTracker_Expired(t *testing.T) {
	tr := NewContextFreshnessTracker(3)
	tr.TrackMessage(0, 1)
	tr.TrackMessage(1, 5)
	tr.UpdateTurn(10)

	fresh := tr.GetFreshMessages()
	// msg 0: age = 10-1 = 9 > 3, expired
	// msg 1: age = 10-5 = 5 > 3, expired
	if len(fresh) != 0 {
		t.Errorf("all should be expired, got %d fresh", len(fresh))
	}
}

func TestContextFreshnessTracker_PartialExpiry(t *testing.T) {
	tr := NewContextFreshnessTracker(5)
	tr.TrackMessage(0, 1)
	tr.TrackMessage(1, 8)
	tr.UpdateTurn(10)

	fresh := tr.GetFreshMessages()
	// msg 0: age = 9 > 5, expired
	// msg 1: age = 2 < 5, fresh
	if len(fresh) != 1 {
		t.Errorf("should have 1 fresh message, got %d", len(fresh))
	}
	if fresh[0] != 1 {
		t.Errorf("fresh message should be index 1, got %d", fresh[0])
	}
}

func TestContextFreshnessTracker_Ratio(t *testing.T) {
	tr := NewContextFreshnessTracker(5)
	tr.TrackMessage(0, 1)
	tr.TrackMessage(1, 8)
	tr.UpdateTurn(10)
	ratio := tr.GetFreshnessRatio()
	if ratio < 0 || ratio > 1 {
		t.Errorf("ratio should be 0..1, got %f", ratio)
	}
}

func TestContextFreshnessTracker_Reset(t *testing.T) {
	tr := NewContextFreshnessTracker(10)
	tr.TrackMessage(0, 1)
	tr.Reset()
	stats := tr.GetStats()
	if stats["totalTracked"].(int) != 0 {
		t.Errorf("totalTracked should be 0 after reset")
	}
}

// ── OPT-128: CacheWarmingSchedulerV2 ──

func TestCacheWarmingSchedulerV2_LearnAndSchedule(t *testing.T) {
	s := NewCacheWarmingSchedulerV2(20)
	s.LearnPattern("key1")
	s.LearnPattern("key1")
	s.LearnPattern("key2")
	s.ScheduleWarmup("key1")

	queue := s.ProcessWarmupQueue()
	if len(queue) == 0 {
		t.Errorf("should have items in warmup queue")
	}
}

func TestCacheWarmingSchedulerV2_HitTracking(t *testing.T) {
	s := NewCacheWarmingSchedulerV2(20)
	s.RecordHit("key1")
	s.RecordHit("key1")
	stats := s.GetStats()
	if stats["totalHits"].(int) != 2 {
		t.Errorf("totalHits should be 2, got %v", stats["totalHits"])
	}
}

func TestCacheWarmingSchedulerV2_Stats(t *testing.T) {
	s := NewCacheWarmingSchedulerV2(20)
	s.LearnPattern("key1")
	s.ScheduleWarmup("key1")
	stats := s.GetStats()
	if stats["patternCount"].(int) != 1 {
		t.Errorf("patternCount should be 1, got %v", stats["patternCount"])
	}
}

func TestCacheWarmingSchedulerV2_Reset(t *testing.T) {
	s := NewCacheWarmingSchedulerV2(20)
	s.LearnPattern("key1")
	s.Reset()
	stats := s.GetStats()
	if stats["patternCount"].(int) != 0 {
		t.Errorf("patternCount should be 0 after reset")
	}
}

// ── OPT-129: PromptTemplateOptimizer ──

func TestPromptTemplateOptimizer_RegisterAndGet(t *testing.T) {
	o := NewPromptTemplateOptimizer()
	o.RegisterTemplate("greeting", "hello world")
	tmpl, ok := o.GetTemplate("greeting")
	if !ok {
		t.Errorf("should find template")
	}
	if tmpl != "hello world" {
		t.Errorf("template should be 'hello world', got %q", tmpl)
	}
}

func TestPromptTemplateOptimizer_Optimize(t *testing.T) {
	o := NewPromptTemplateOptimizer()
	template := "# comment line\nhello   world\n\n\n  test  "
	o.RegisterTemplate("test", template)
	optimized := o.OptimizeTemplate("test")
	// Should remove comment, compress spaces, remove blank lines
	if optimized == template {
		t.Errorf("template should be optimized")
	}
}

func TestPromptTemplateOptimizer_EstimateSavings(t *testing.T) {
	o := NewPromptTemplateOptimizer()
	template := "# comment\nhello   world\n\n\n  test  "
	o.RegisterTemplate("test", template)
	savings := o.EstimateSavings("test")
	if savings < 0 {
		t.Errorf("savings should be >= 0, got %d", savings)
	}
}

func TestPromptTemplateOptimizer_Stats(t *testing.T) {
	o := NewPromptTemplateOptimizer()
	o.RegisterTemplate("t1", "hello")
	o.OptimizeTemplate("t1")
	stats := o.GetStats()
	if stats["totalOptimized"].(int) != 1 {
		t.Errorf("totalOptimized should be 1, got %v", stats["totalOptimized"])
	}
}

func TestPromptTemplateOptimizer_Reset(t *testing.T) {
	o := NewPromptTemplateOptimizer()
	o.RegisterTemplate("t1", "hello")
	o.Reset()
	stats := o.GetStats()
	if stats["templateCount"].(int) != 0 {
		t.Errorf("templateCount should be 0 after reset")
	}
}

// ── OPT-130: TokenBudgetNegotiator ──

func TestTokenBudgetNegotiator_RegisterAndRequest(t *testing.T) {
	n := NewTokenBudgetNegotiator(10000)
	n.RegisterConsumer("agent", 5)
	n.RegisterConsumer("tool", 3)
	n.RequestBudget("agent", 5000)
	n.RequestBudget("tool", 3000)

	allocations := n.Negotiate()
	if len(allocations) != 2 {
		t.Errorf("should have 2 allocations, got %d", len(allocations))
	}
}

func TestTokenBudgetNegotiator_SufficientBudget(t *testing.T) {
	n := NewTokenBudgetNegotiator(10000)
	n.RegisterConsumer("a", 5)
	n.RegisterConsumer("b", 3)
	n.RequestBudget("a", 3000)
	n.RequestBudget("b", 2000)

	allocations := n.Negotiate()
	if allocations["a"] != 3000 {
		t.Errorf("a should get 3000, got %d", allocations["a"])
	}
	if allocations["b"] != 2000 {
		t.Errorf("b should get 2000, got %d", allocations["b"])
	}
}

func TestTokenBudgetNegotiator_InsufficientBudget(t *testing.T) {
	n := NewTokenBudgetNegotiator(1000)
	n.RegisterConsumer("a", 5)
	n.RegisterConsumer("b", 3)
	n.RequestBudget("a", 3000)
	n.RequestBudget("b", 2000)

	allocations := n.Negotiate()
	total := 0
	for _, v := range allocations {
		total += v
	}
	if total > 1000 {
		t.Errorf("total allocation should not exceed budget, got %d", total)
	}
}

func TestTokenBudgetNegotiator_GetAllocation(t *testing.T) {
	n := NewTokenBudgetNegotiator(10000)
	n.RegisterConsumer("a", 5)
	n.RequestBudget("a", 5000)
	n.Negotiate()
	alloc := n.GetAllocation("a")
	if alloc <= 0 {
		t.Errorf("should have positive allocation, got %d", alloc)
	}
}

func TestTokenBudgetNegotiator_Stats(t *testing.T) {
	n := NewTokenBudgetNegotiator(10000)
	n.RegisterConsumer("a", 5)
	n.RegisterConsumer("b", 3)
	n.Negotiate()
	stats := n.GetStats()
	if stats["consumerCount"].(int) != 2 {
		t.Errorf("consumerCount should be 2, got %v", stats["consumerCount"])
	}
	if stats["totalBudget"].(int) != 10000 {
		t.Errorf("totalBudget should be 10000, got %v", stats["totalBudget"])
	}
}

func TestTokenBudgetNegotiator_Reset(t *testing.T) {
	n := NewTokenBudgetNegotiator(10000)
	n.RegisterConsumer("a", 5)
	n.Reset()
	stats := n.GetStats()
	if stats["consumerCount"].(int) != 0 {
		t.Errorf("consumerCount should be 0 after reset")
	}
}
