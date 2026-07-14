package agent

import (
	"testing"
)

// ── OPT-111: ContextDiffCompressor ──

func TestContextDiffCompressor_CompressSimilar(t *testing.T) {
	c := NewContextDiffCompressor()
	msg1 := "The quick brown fox jumps over the lazy dog and runs to the river"
	msg2 := "The quick brown fox jumps over the lazy dog and walks to the forest"

	out1 := c.CompressMessage(msg1)
	if out1 != msg1 {
		t.Errorf("first message should be returned in full, got %q", out1)
	}
	out2 := c.CompressMessage(msg2)
	// Should be compressed since they share a long prefix
	if len(out2) >= len(msg2) {
		t.Errorf("similar message should be compressed, got length %d (original %d)", len(out2), len(msg2))
	}
}

func TestContextDiffCompressor_CompressDifferent(t *testing.T) {
	c := NewContextDiffCompressor()
	c.CompressMessage("hello world")
	out := c.CompressMessage("completely different topic about database")
	if out != "completely different topic about database" {
		t.Errorf("different message should be returned in full, got %q", out)
	}
}

func TestContextDiffCompressor_DiffRatio(t *testing.T) {
	c := NewContextDiffCompressor()
	ratio := c.GetDiffRatio("hello world test", "hello world test")
	if ratio != 1.0 {
		t.Errorf("identical messages should have ratio 1.0, got %f", ratio)
	}
	ratio2 := c.GetDiffRatio("hello world", "goodbye universe")
	if ratio2 > 0.1 {
		t.Errorf("different messages should have low ratio, got %f", ratio2)
	}
}

func TestContextDiffCompressor_Stats(t *testing.T) {
	c := NewContextDiffCompressor()
	c.CompressMessage("hello world test message")
	c.CompressMessage("hello world test message two")
	stats := c.GetStats()
	if stats["totalCompressions"].(int) < 1 {
		t.Errorf("should have at least 1 compression, got %v", stats["totalCompressions"])
	}
}

func TestContextDiffCompressor_Reset(t *testing.T) {
	c := NewContextDiffCompressor()
	c.CompressMessage("hello")
	c.Reset()
	stats := c.GetStats()
	if stats["totalCompressions"].(int) != 0 {
		t.Errorf("totalCompressions should be 0 after reset, got %v", stats["totalCompressions"])
	}
}

// ── OPT-112: TokenBudgetPredictor ──

func TestTokenBudgetPredictor_EmptyHistory(t *testing.T) {
	p := NewTokenBudgetPredictor(100)
	pred := p.PredictNext()
	if pred != 0 {
		t.Errorf("empty history should predict 0, got %d", pred)
	}
}

func TestTokenBudgetPredictor_Predict(t *testing.T) {
	p := NewTokenBudgetPredictor(100)
	p.RecordUsage(1000)
	p.RecordUsage(1200)
	p.RecordUsage(800)
	pred := p.PredictNext()
	if pred <= 0 {
		t.Errorf("prediction should be positive, got %d", pred)
	}
	// Average of 1000, 1200, 800 = 1000
	if pred < 800 || pred > 1200 {
		t.Errorf("prediction should be near average ~1000, got %d", pred)
	}
}

func TestTokenBudgetPredictor_Accuracy(t *testing.T) {
	p := NewTokenBudgetPredictor(100)
	p.RecordUsage(1000)
	p.PredictNext() // prediction ~1000
	p.RecordAccuracy(1050) // within 20% of 1000
	stats := p.GetStats()
	if stats["accuratePredictions"].(int) == 0 {
		t.Errorf("should have at least 1 accurate prediction")
	}
}

func TestTokenBudgetPredictor_Inaccurate(t *testing.T) {
	p := NewTokenBudgetPredictor(100)
	p.RecordUsage(1000)
	p.PredictNext() // prediction ~1000
	p.RecordAccuracy(5000) // way off, 500% over
	stats := p.GetStats()
	if stats["accuratePredictions"].(int) != 0 {
		t.Errorf("should have 0 accurate predictions for 5x deviation")
	}
}

func TestTokenBudgetPredictor_Reset(t *testing.T) {
	p := NewTokenBudgetPredictor(100)
	p.RecordUsage(1000)
	p.Reset()
	stats := p.GetStats()
	if stats["totalPredictions"].(int) != 0 {
		t.Errorf("totalPredictions should be 0 after reset, got %v", stats["totalPredictions"])
	}
}

// ── OPT-113: CacheKeyOptimizer ──

func TestCacheKeyOptimizer_GenerateKey(t *testing.T) {
	o := NewCacheKeyOptimizer()
	key1 := o.GenerateKey("hello world")
	key2 := o.GenerateKey("hello world")
	if key1 != key2 {
		t.Errorf("same content should generate same key")
	}
	if len(key1) == 0 {
		t.Errorf("key should not be empty")
	}
}

func TestCacheKeyOptimizer_DifferentKeys(t *testing.T) {
	o := NewCacheKeyOptimizer()
	key1 := o.GenerateKey("hello world")
	key2 := o.GenerateKey("goodbye universe")
	if key1 == key2 {
		t.Errorf("different content should generate different keys")
	}
}

func TestCacheKeyOptimizer_Collision(t *testing.T) {
	o := NewCacheKeyOptimizer()
	key := o.GenerateKey("test content")
	o.RegisterKey(key)
	if !o.CheckCollision(key) {
		t.Errorf("should detect collision for registered key")
	}
	o.RegisterKey(key) // register again
	stats := o.GetStats()
	if stats["collisions"].(int) < 1 {
		t.Errorf("should have at least 1 collision, got %v", stats["collisions"])
	}
}

func TestCacheKeyOptimizer_Stats(t *testing.T) {
	o := NewCacheKeyOptimizer()
	o.GenerateKey("test1")
	o.GenerateKey("test2")
	stats := o.GetStats()
	if stats["totalGenerated"].(int) != 2 {
		t.Errorf("totalGenerated should be 2, got %v", stats["totalGenerated"])
	}
}

func TestCacheKeyOptimizer_Reset(t *testing.T) {
	o := NewCacheKeyOptimizer()
	o.GenerateKey("test")
	o.Reset()
	stats := o.GetStats()
	if stats["totalGenerated"].(int) != 0 {
		t.Errorf("totalGenerated should be 0 after reset, got %v", stats["totalGenerated"])
	}
}

// ── OPT-114: ResponseLengthOptimizer ──

func TestResponseLengthOptimizer_NoCompression(t *testing.T) {
	o := NewResponseLengthOptimizer(100)
	result := o.OptimizeLength("short response", 50)
	if result != "short response" {
		t.Errorf("short response should not be compressed, got %q", result)
	}
}

func TestResponseLengthOptimizer_Compress(t *testing.T) {
	o := NewResponseLengthOptimizer(20)
	long := "this is a very long response that should be compressed because it exceeds the target length significantly"
	result := o.OptimizeLength(long, 100) // contextSize > targetLength*2
	if len(result) > 25 { // 20 + "..."
		t.Errorf("compressed response should be short, got length %d", len(result))
	}
}

func TestResponseLengthOptimizer_ShouldCompress(t *testing.T) {
	o := NewResponseLengthOptimizer(50)
	if o.ShouldCompress("short", 10) {
		t.Errorf("should not compress short response with small context")
	}
	if !o.ShouldCompress("this is a very long response that exceeds the target length", 200) {
		t.Errorf("should compress long response with large context")
	}
}

func TestResponseLengthOptimizer_SetTargetLength(t *testing.T) {
	o := NewResponseLengthOptimizer(100)
	o.SetTargetLength(50)
	stats := o.GetStats()
	if stats["targetLength"].(int) != 50 {
		t.Errorf("targetLength should be 50, got %v", stats["targetLength"])
	}
}

func TestResponseLengthOptimizer_Reset(t *testing.T) {
	o := NewResponseLengthOptimizer(100)
	o.OptimizeLength("test", 50)
	o.Reset()
	stats := o.GetStats()
	if stats["totalOptimized"].(int) != 0 {
		t.Errorf("totalOptimized should be 0 after reset, got %v", stats["totalOptimized"])
	}
}

// ── OPT-115: TokenAwarePrioritizer ──

func TestTokenAwarePrioritizer_Prioritize(t *testing.T) {
	p := NewTokenAwarePrioritizer()
	items := []PrioritizerItem{
		{Content: "low value high cost", Value: 1, Tokens: 100},
		{Content: "high value low cost", Value: 10, Tokens: 10},
		{Content: "medium", Value: 5, Tokens: 50},
	}
	result := p.Prioritize(items)
	if result[0].Value != 10 {
		t.Errorf("first item should be highest efficiency (10/10=1.0), got value %d", result[0].Value)
	}
}

func TestTokenAwarePrioritizer_CalculateEfficiency(t *testing.T) {
	p := NewTokenAwarePrioritizer()
	eff := p.CalculateEfficiency(10, 100)
	if eff != 0.1 {
		t.Errorf("efficiency should be 0.1, got %f", eff)
	}
	effZero := p.CalculateEfficiency(10, 0)
	if effZero != 0 {
		t.Errorf("efficiency with 0 tokens should be 0, got %f", effZero)
	}
}

func TestTokenAwarePrioritizer_AlreadySorted(t *testing.T) {
	p := NewTokenAwarePrioritizer()
	items := []PrioritizerItem{
		{Content: "best", Value: 10, Tokens: 10},
		{Content: "medium", Value: 5, Tokens: 50},
		{Content: "worst", Value: 1, Tokens: 100},
	}
	result := p.Prioritize(items)
	// Already sorted, should not count as reorder
	if result[0].Content != "best" {
		t.Errorf("first should be 'best', got %q", result[0].Content)
	}
}

func TestTokenAwarePrioritizer_Stats(t *testing.T) {
	p := NewTokenAwarePrioritizer()
	items := []PrioritizerItem{
		{Content: "a", Value: 1, Tokens: 100},
		{Content: "b", Value: 10, Tokens: 10},
	}
	p.Prioritize(items)
	stats := p.GetStats()
	if stats["totalSorted"].(int) != 1 {
		t.Errorf("totalSorted should be 1, got %v", stats["totalSorted"])
	}
}

func TestTokenAwarePrioritizer_Reset(t *testing.T) {
	p := NewTokenAwarePrioritizer()
	p.Prioritize([]PrioritizerItem{{Content: "a", Value: 1, Tokens: 1}})
	p.Reset()
	stats := p.GetStats()
	if stats["totalSorted"].(int) != 0 {
		t.Errorf("totalSorted should be 0 after reset, got %v", stats["totalSorted"])
	}
}
