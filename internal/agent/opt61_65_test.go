package agent

import (
	"testing"
)

// ── OPT-61: WarmupPredictor ──

func TestWarmupPredictor_PredictTools(t *testing.T) {
	p := NewWarmupPredictor()
	tools := p.PredictTools("please read the file and edit it")
	if len(tools) == 0 {
		t.Fatal("should predict tools for file query")
	}
}

func TestWarmupPredictor_NoMatch(t *testing.T) {
	p := NewWarmupPredictor()
	tools := p.PredictTools("xyz")
	if len(tools) != 0 {
		t.Fatal("should not predict tools for unknown query")
	}
}

func TestWarmupPredictor_ShouldWarmup(t *testing.T) {
	p := NewWarmupPredictor()
	p.PredictTools("read the file")
	if !p.ShouldWarmup("read_file") {
		t.Fatal("should warmup read_file after file prediction")
	}
}

func TestWarmupPredictor_GetStats(t *testing.T) {
	p := NewWarmupPredictor()
	p.PredictTools("read file")
	p.RecordPrediction("read file", []string{"read_file", "grep"})
	stats := p.GetStats()
	_ = stats // stats accessible after recording
}

// ── OPT-62: TokenBudgetEnforcer ──

func TestTokenBudgetEnforcer_Allow(t *testing.T) {
	e := NewTokenBudgetEnforcer(100000)
	if e.Enforce(50000) != EnforcementAllow {
		t.Fatal("50% usage should be allowed")
	}
}

func TestTokenBudgetEnforcer_Warn(t *testing.T) {
	e := NewTokenBudgetEnforcer(100000)
	if e.Enforce(85000) != EnforcementWarn {
		t.Fatal("85% usage should warn")
	}
}

func TestTokenBudgetEnforcer_Degrade(t *testing.T) {
	e := NewTokenBudgetEnforcer(100000)
	if e.Enforce(100000) != EnforcementDegrade {
		t.Fatal("100% usage should degrade")
	}
}

func TestTokenBudgetEnforcer_GetStats(t *testing.T) {
	e := NewTokenBudgetEnforcer(100000)
	e.Enforce(50000)
	e.Enforce(100000)
	stats := e.GetStats()
	if stats.EnforcementCount != 2 {
		t.Fatalf("expected 2 enforcements, got %d", stats.EnforcementCount)
	}
}

// ── OPT-63: ContextWindowStrategy ──

func TestContextWindowStrategy_Grow(t *testing.T) {
	s := NewContextWindowStrategy()
	decision := s.Evaluate(50000, 128000, 1)
	if decision.Action != "grow" {
		t.Fatalf("expected grow, got %s", decision.Action)
	}
}

func TestContextWindowStrategy_Compact(t *testing.T) {
	s := NewContextWindowStrategy()
	decision := s.Evaluate(110000, 128000, 5)
	if decision.Action != "compact" {
		t.Fatalf("expected compact, got %s", decision.Action)
	}
}

func TestContextWindowStrategy_Shrink(t *testing.T) {
	s := NewContextWindowStrategy()
	decision := s.Evaluate(20000, 128000, 5) // 15% utilization, turn > 3
	if decision.Action != "shrink" {
		t.Fatalf("expected shrink, got %s", decision.Action)
	}
}

func TestContextWindowStrategy_GetStats(t *testing.T) {
	s := NewContextWindowStrategy()
	s.Evaluate(50000, 128000, 1)
	stats := s.GetStats()
	if stats.TotalEvaluations != 1 {
		t.Fatalf("expected 1 evaluation, got %d", stats.TotalEvaluations)
	}
}

// ── OPT-64: ToolOutputCache ──

func TestToolOutputCache_SetGet(t *testing.T) {
	c := NewToolOutputCache(0)
	c.Set("bash", "ls", "file1.txt\nfile2.txt")
	result, hit := c.Get("bash", "ls")
	if !hit {
		t.Fatal("should hit cache")
	}
	if result != "file1.txt\nfile2.txt" {
		t.Fatal("cached result should match")
	}
}

func TestToolOutputCache_Miss(t *testing.T) {
	c := NewToolOutputCache(0)
	_, hit := c.Get("bash", "ls")
	if hit {
		t.Fatal("should miss cache for non-existent entry")
	}
}

func TestToolOutputCache_Invalidate(t *testing.T) {
	c := NewToolOutputCache(0)
	c.Set("bash", "ls", "output")
	c.Invalidate("bash")
	_, hit := c.Get("bash", "ls")
	if hit {
		t.Fatal("should miss after invalidation")
	}
}

func TestToolOutputCache_GetStats(t *testing.T) {
	c := NewToolOutputCache(0)
	c.Set("bash", "ls", "output")
	c.Get("bash", "ls")  // hit
	c.Get("bash", "rm")  // miss
	stats := c.GetStats()
	if stats.TotalHits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.TotalHits)
	}
	if stats.TotalMisses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.TotalMisses)
	}
}

// ── OPT-65: PromptFragmentCache ──

func TestPromptFragmentCache_SetGet(t *testing.T) {
	c := NewPromptFragmentCache()
	c.SetFragment("system_prompt", "You are a helpful assistant.")
	result, hit := c.GetFragment("system_prompt")
	if !hit {
		t.Fatal("should hit cache")
	}
	if result != "You are a helpful assistant." {
		t.Fatal("cached fragment should match")
	}
}

func TestPromptFragmentCache_GetOrCompute(t *testing.T) {
	c := NewPromptFragmentCache()
	called := false
	result1 := c.GetOrCompute("key1", func() string {
		called = true
		return "computed value"
	})
	if !called {
		t.Fatal("should call compute function on first call")
	}
	if result1 != "computed value" {
		t.Fatal("should return computed value")
	}

	called = false
	result2 := c.GetOrCompute("key1", func() string {
		called = true
		return "should not be called"
	})
	if called {
		t.Fatal("should not call compute on cache hit")
	}
	if result2 != "computed value" {
		t.Fatal("should return cached value")
	}
}

func TestPromptFragmentCache_GetStats(t *testing.T) {
	c := NewPromptFragmentCache()
	c.SetFragment("key1", "content1")
	c.GetFragment("key1") // hit
	c.GetFragment("key2") // miss
	stats := c.GetStats()
	if stats.TotalHits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.TotalHits)
	}
}
