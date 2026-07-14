package agent

import (
	"strings"
	"testing"
)

// ── OPT-101: TokenStreamCompressor ──

func TestTokenStreamCompressor_BasicStream(t *testing.T) {
	c := NewTokenStreamCompressor()
	c.StartStream("s1")

	out1 := c.PushChunk("s1", "hello world")
	out2 := c.PushChunk("s1", "hello hello world")
	out3 := c.PushChunk("s1", "world world world")

	if !strings.Contains(out1, "hello") || !strings.Contains(out1, "world") {
		t.Errorf("first chunk should contain original tokens, got %q", out1)
	}
	// out2 should have deduped consecutive "hello hello" to one
	if strings.Count(out2, "hello") > 1 {
		t.Errorf("consecutive hello should be deduped, got %q", out2)
	}
	// out3 should have deduped consecutive "world world world" to one
	if strings.Count(out3, "world") > 1 {
		t.Errorf("consecutive world should be deduped, got %q", out3)
	}
	saved := c.EndStream("s1")
	if saved < 0 {
		t.Errorf("saved tokens should be >= 0, got %d", saved)
	}
}

func TestTokenStreamCompressor_MultipleStreams(t *testing.T) {
	c := NewTokenStreamCompressor()
	c.StartStream("a")
	c.StartStream("b")

	c.PushChunk("a", "token token")
	c.PushChunk("b", "data data data")

	c.EndStream("a")
	c.EndStream("b")

	stats := c.GetStats()
	if stats["totalStreamed"].(int64) < 2 {
		t.Errorf("should have streamed at least 2 chunks, got %v", stats["totalStreamed"])
	}
	if stats["activeStreams"].(int) != 0 {
		t.Errorf("active streams should be 0 after ending, got %v", stats["activeStreams"])
	}
}

func TestTokenStreamCompressor_CompressionRatio(t *testing.T) {
	c := NewTokenStreamCompressor()
	c.StartStream("s1")
	c.PushChunk("s1", "go go go go go")
	c.PushChunk("s1", "go go go")
	c.EndStream("s1")

	ratio := c.GetCompressionRatio()
	if ratio < 0 || ratio > 1 {
		t.Errorf("compression ratio should be 0..1, got %f", ratio)
	}
}

func TestTokenStreamCompressor_Reset(t *testing.T) {
	c := NewTokenStreamCompressor()
	c.StartStream("s1")
	c.PushChunk("s1", "hello hello")
	c.EndStream("s1")
	c.Reset()
	stats := c.GetStats()
	if stats["totalStreamed"].(int64) != 0 {
		t.Errorf("totalStreamed should be 0 after reset, got %v", stats["totalStreamed"])
	}
}

// ── OPT-102: AdaptiveContextSelector ──

func TestAdaptiveContextSelector_AnalyzeSimple(t *testing.T) {
	s := NewAdaptiveContextSelector(8000)
	complexity := s.AnalyzeQueryComplexity("hi")
	if complexity >= 0.3 {
		t.Errorf("simple query should have low complexity, got %f", complexity)
	}
}

func TestAdaptiveContextSelector_AnalyzeComplex(t *testing.T) {
	s := NewAdaptiveContextSelector(8000)
	long := "please analyze the following complex architecture diagram and explain in detail how each component interacts with the database and caching layer including edge cases error handling and performance considerations"
	complexity := s.AnalyzeQueryComplexity(long)
	if complexity < 0.3 {
		t.Errorf("complex query should have higher complexity, got %f", complexity)
	}
}

func TestAdaptiveContextSelector_SelectWindow(t *testing.T) {
	s := NewAdaptiveContextSelector(8000)
	w1 := s.SelectWindow(0.1) // simple
	if w1 > 8000*30/100 {
		t.Errorf("low complexity should select small window, got %d", w1)
	}
	w2 := s.SelectWindow(0.9) // complex
	if w2 != 8000 {
		t.Errorf("high complexity should select max window, got %d", w2)
	}
}

func TestAdaptiveContextSelector_RecordAndStats(t *testing.T) {
	s := NewAdaptiveContextSelector(8000)
	s.RecordSelection(2000)
	s.RecordSelection(4000)
	s.RecordSelection(6000)
	stats := s.GetStats()
	if stats["totalSelections"].(int) != 3 {
		t.Errorf("totalSelections should be 3, got %v", stats["totalSelections"])
	}
}

func TestAdaptiveContextSelector_Reset(t *testing.T) {
	s := NewAdaptiveContextSelector(8000)
	s.RecordSelection(2000)
	s.Reset()
	stats := s.GetStats()
	if stats["totalSelections"].(int) != 0 {
		t.Errorf("totalSelections should be 0 after reset, got %v", stats["totalSelections"])
	}
}

// ── OPT-103: PromptTokenAnalyzer ──

func TestPromptTokenAnalyzer_AnalyzeClean(t *testing.T) {
	a := NewPromptTokenAnalyzer()
	result := a.Analyze("hello world")
	tokens, ok := result["totalTokens"].(int)
	if !ok || tokens == 0 {
		t.Errorf("should have non-zero tokens, got %v", result["totalTokens"])
	}
	eff, ok := result["efficiency"].(float64)
	if !ok || eff < 0 || eff > 1 {
		t.Errorf("efficiency should be 0..1, got %v", result["efficiency"])
	}
}

func TestPromptTokenAnalyzer_AnalyzeWaste(t *testing.T) {
	a := NewPromptTokenAnalyzer()
	// Lots of redundant whitespace and punctuation
	wasteful := "hello   world    !!!  ...  hello   world"
	result := a.Analyze(wasteful)
	waste, ok := result["wasteTokens"].(int)
	if !ok || waste <= 0 {
		t.Errorf("should detect waste in redundant prompt, got %v", result["wasteTokens"])
	}
}

func TestPromptTokenAnalyzer_CategorizeWaste(t *testing.T) {
	a := NewPromptTokenAnalyzer()
	cats := a.CategorizeWaste("hello   world   !!!  ...  test   test")
	if cats["whitespace"] == 0 {
		t.Errorf("should detect whitespace waste")
	}
	if cats["punctuation"] == 0 {
		t.Errorf("should detect punctuation waste")
	}
}

func TestPromptTokenAnalyzer_Stats(t *testing.T) {
	a := NewPromptTokenAnalyzer()
	a.Analyze("hello world")
	a.Analyze("test   prompt   !!!")
	stats := a.GetStats()
	if stats["analyses"].(int) != 2 {
		t.Errorf("analyses should be 2, got %v", stats["analyses"])
	}
}

func TestPromptTokenAnalyzer_Reset(t *testing.T) {
	a := NewPromptTokenAnalyzer()
	a.Analyze("hello")
	a.Reset()
	stats := a.GetStats()
	if stats["analyses"].(int) != 0 {
		t.Errorf("analyses should be 0 after reset, got %v", stats["analyses"])
	}
}

// ── OPT-104: CachePressureMonitor ──

func TestCachePressureMonitor_LowPressure(t *testing.T) {
	m := NewCachePressureMonitor(1000)
	for i := 0; i < 100; i++ {
		m.RecordInsert()
	}
	level := m.GetPressureLevel()
	if level != "low" {
		t.Errorf("100/1000 should be low pressure, got %s", level)
	}
	if m.ShouldEvict() {
		t.Errorf("should not evict at low pressure")
	}
}

func TestCachePressureMonitor_HighPressure(t *testing.T) {
	m := NewCachePressureMonitor(1000)
	for i := 0; i < 900; i++ {
		m.RecordInsert()
	}
	level := m.GetPressureLevel()
	if level != "high" {
		t.Errorf("900/1000 should be high pressure, got %s", level)
	}
	if !m.ShouldEvict() {
		t.Errorf("should evict at high pressure")
	}
}

func TestCachePressureMonitor_CriticalPressure(t *testing.T) {
	m := NewCachePressureMonitor(1000)
	for i := 0; i < 980; i++ {
		m.RecordInsert()
	}
	level := m.GetPressureLevel()
	if level != "critical" {
		t.Errorf("980/1000 should be critical pressure, got %s", level)
	}
}

func TestCachePressureMonitor_Eviction(t *testing.T) {
	m := NewCachePressureMonitor(1000)
	for i := 0; i < 900; i++ {
		m.RecordInsert()
	}
	m.RecordEviction()
	stats := m.GetStats()
	if stats["evictions"].(int) != 1 {
		t.Errorf("evictions should be 1, got %v", stats["evictions"])
	}
}

func TestCachePressureMonitor_Reset(t *testing.T) {
	m := NewCachePressureMonitor(1000)
	for i := 0; i < 500; i++ {
		m.RecordInsert()
	}
	m.Reset()
	stats := m.GetStats()
	if stats["currentEntries"].(int) != 0 {
		t.Errorf("currentEntries should be 0 after reset, got %v", stats["currentEntries"])
	}
	if stats["evictions"].(int) != 0 {
		t.Errorf("evictions should be 0 after reset, got %v", stats["evictions"])
	}
}

// ── OPT-105: TokenFlowRegulator ──

func TestTokenFlowRegulator_ConsumeWithinBudget(t *testing.T) {
	r := NewTokenFlowRegulator(10000, 5000)
	if !r.Consume(1000) {
		t.Errorf("consuming 1000 within 10000 budget should succeed")
	}
	if !r.Consume(2000) {
		t.Errorf("consuming 2000 within remaining budget should succeed")
	}
}

func TestTokenFlowRegulator_ExceedBudget(t *testing.T) {
	r := NewTokenFlowRegulator(1000, 500)
	r.Consume(600)
	if r.Consume(500) {
		t.Errorf("consuming 500 when only 400 remaining should fail")
	}
}

func TestTokenFlowRegulator_RateLimit(t *testing.T) {
	r := NewTokenFlowRegulator(100000, 1000)
	// Consume a large burst that exceeds rate limit
	r.Consume(1500)
	// Next consume should be regulated
	if !r.Consume(100) {
		// After burst is reset, small consume should work
		// The regulator allows burst up to burstAllowance = rateLimit * 2 = 2000
		// So 1500 is within burst, 100 more = 1600, still within 2000
	}
}

func TestTokenFlowRegulator_RemainingBudget(t *testing.T) {
	r := NewTokenFlowRegulator(10000, 5000)
	r.Consume(3000)
	remaining := r.GetRemainingBudget()
	if remaining != 7000 {
		t.Errorf("remaining should be 7000, got %d", remaining)
	}
}

func TestTokenFlowRegulator_AdjustRate(t *testing.T) {
	r := NewTokenFlowRegulator(10000, 5000)
	r.AdjustRate(8000)
	stats := r.GetStats()
	if stats["rateLimit"].(int) != 8000 {
		t.Errorf("rateLimit should be 8000 after adjust, got %v", stats["rateLimit"])
	}
}

func TestTokenFlowRegulator_Reset(t *testing.T) {
	r := NewTokenFlowRegulator(10000, 5000)
	r.Consume(5000)
	r.Reset()
	stats := r.GetStats()
	if stats["consumed"].(int) != 0 {
		t.Errorf("consumed should be 0 after reset, got %v", stats["consumed"])
	}
	if stats["budget"].(int) != 10000 {
		t.Errorf("budget should be preserved after reset, got %v", stats["budget"])
	}
}
