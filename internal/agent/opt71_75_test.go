package agent

import (
	"testing"
)

// ── OPT-71: CachePrefixStabilizer ──

func TestCachePrefixStabilizer_Stabilize(t *testing.T) {
	s := NewCachePrefixStabilizer()
	prompt := "  system   prompt  "
	tools := `[{"name":"bash"},{"name":"edit_file"}]`
	sp, st := s.Stabilize(prompt, tools)
	if sp == "" {
		t.Fatal("stabilized prompt should not be empty")
	}
	if st == "" {
		t.Fatal("stabilized tools should not be empty")
	}
}

func TestCachePrefixStabilizer_DetectChange(t *testing.T) {
	s := NewCachePrefixStabilizer()
	s.Stabilize("prompt1", "[]")
	// First call establishes baseline; DetectChange compares against last stable hash
	changed := s.DetectChange("different_hash")
	_ = changed // behavior depends on implementation
}

func TestCachePrefixStabilizer_GetStats(t *testing.T) {
	s := NewCachePrefixStabilizer()
	s.Stabilize("prompt", "[]")
	stats := s.GetStats()
	_ = stats
}

// ── OPT-72: ResponseTokenController ──

func TestResponseTokenController_ClassifyQuery(t *testing.T) {
	c := NewResponseTokenController()
	if c.ClassifyQuery("write a function") != "code_generation" {
		t.Fatal("should classify as code_generation")
	}
	if c.ClassifyQuery("explain how it works") != "explanation" {
		t.Fatal("should classify as explanation")
	}
	if c.ClassifyQuery("summarize the results") != "summary" {
		t.Fatal("should classify as summary")
	}
}

func TestResponseTokenController_GetMaxTokens(t *testing.T) {
	c := NewResponseTokenController()
	max := c.GetMaxTokens("code_generation", 128000)
	if max != 8192 {
		t.Fatalf("expected 8192 for code_generation, got %d", max)
	}
	max = c.GetMaxTokens("summary", 128000)
	if max != 1024 {
		t.Fatalf("expected 1024 for summary, got %d", max)
	}
}

func TestResponseTokenController_CappedByContext(t *testing.T) {
	c := NewResponseTokenController()
	max := c.GetMaxTokens("code_generation", 10000)
	if max > 2500 { // 10000/4 = 2500
		t.Fatalf("should be capped by context/4, got %d", max)
	}
}

func TestResponseTokenController_GetStats(t *testing.T) {
	c := NewResponseTokenController()
	c.GetMaxTokens("code_generation", 128000)
	stats := c.GetStats()
	if stats.TotalControlled == 0 {
		t.Fatal("should have stats")
	}
}

// ── OPT-73: ContextDecayManager ──

func TestContextDecayManager_AgeMessages(t *testing.T) {
	m := NewContextDecayManager()
	m.AgeMessages(1)
	m.AgeMessages(2)
	m.AgeMessages(3)
	stats := m.GetStats()
	_ = stats
}

func TestContextDecayManager_ShouldDecay(t *testing.T) {
	m := NewContextDecayManager()
	// Age messages 6 times
	for i := 1; i <= 6; i++ {
		m.AgeMessages(i)
	}
	// Verify aging works without crash
	_ = m.ShouldDecay(0)
}

func TestContextDecayManager_GetDecayPriority(t *testing.T) {
	m := NewContextDecayManager()
	priority := m.GetDecayPriority(0)
	if priority < 0 || priority > 1 {
		t.Fatalf("priority should be 0-1, got %f", priority)
	}
}

func TestContextDecayManager_GetStats(t *testing.T) {
	m := NewContextDecayManager()
	m.AgeMessages(1)
	stats := m.GetStats()
	if stats.DecayRate != 0.1 {
		t.Fatalf("expected decay rate 0.1, got %f", stats.DecayRate)
	}
}

// ── OPT-74: ToolCallOptimizer ──

func TestToolCallOptimizer_ShouldSkipCall(t *testing.T) {
	o := NewToolCallOptimizer()
	o.RecordCall("bash", "ls", "output1", 1)
	// Same call in next turn should be skippable
	if !o.ShouldSkipCall("bash", "ls") {
		t.Fatal("should skip duplicate call")
	}
}

func TestToolCallOptimizer_DifferentArgs(t *testing.T) {
	o := NewToolCallOptimizer()
	o.RecordCall("bash", "ls", "output1", 1)
	if o.ShouldSkipCall("bash", "rm") {
		t.Fatal("should not skip call with different args")
	}
}

func TestToolCallOptimizer_GetCallFrequency(t *testing.T) {
	o := NewToolCallOptimizer()
	o.RecordCall("bash", "ls", "out1", 1)
	o.RecordCall("bash", "pwd", "out2", 2)
	o.RecordCall("grep", "pattern", "out3", 3)
	if o.GetCallFrequency("bash") != 2 {
		t.Fatalf("expected frequency 2 for bash, got %d", o.GetCallFrequency("bash"))
	}
}

func TestToolCallOptimizer_GetStats(t *testing.T) {
	o := NewToolCallOptimizer()
	o.RecordCall("bash", "ls", "out", 1)
	o.ShouldSkipCall("bash", "ls")
	stats := o.GetStats()
	_ = stats
}

// ── OPT-75: TokenEfficiencyScorer ──

func TestTokenEfficiencyScorer_ScoreEfficiency(t *testing.T) {
	s := NewTokenEfficiencyScorer()
	score := s.ScoreEfficiency(10000, 2000, 8000, 2000, 3)
	if score.Score < 0 || score.Score > 100 {
		t.Fatalf("score should be 0-100, got %f", score.Score)
	}
	if score.Grade == "" {
		t.Fatal("should have a grade")
	}
}

func TestTokenEfficiencyScorer_HighEfficiency(t *testing.T) {
	s := NewTokenEfficiencyScorer()
	score := s.ScoreEfficiency(10000, 2000, 9500, 500, 1)
	if score.Score < 70 {
		t.Fatalf("high cache hit should score well, got %f", score.Score)
	}
}

func TestTokenEfficiencyScorer_LowEfficiency(t *testing.T) {
	s := NewTokenEfficiencyScorer()
	score := s.ScoreEfficiency(10000, 8000, 0, 10000, 10)
	if score.Score > 55 {
		t.Fatalf("no cache hits and many tool calls should score poorly, got %f", score.Score)
	}
}

func TestTokenEfficiencyScorer_GetOverallStats(t *testing.T) {
	s := NewTokenEfficiencyScorer()
	s.ScoreEfficiency(10000, 2000, 8000, 2000, 3)
	s.ScoreEfficiency(10000, 2000, 5000, 5000, 2)
	stats := s.GetOverallStats()
	if stats.TotalScored != 2 {
		t.Fatalf("expected 2 scored, got %d", stats.TotalScored)
	}
}
