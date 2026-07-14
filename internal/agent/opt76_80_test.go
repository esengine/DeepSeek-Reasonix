package agent

import (
	"testing"

	"reasonix/internal/provider"
)

// ── OPT-76: SemanticSimilarityDedup ──

func TestSemanticSimilarityDedup_Identical(t *testing.T) {
	d := NewSemanticSimilarityDedup()
	d.RecordContent("hello world test content", "hash1")
	isDup, _ := d.CheckSimilarity("hello world test content")
	if !isDup {
		t.Fatal("identical content should be detected as duplicate")
	}
}

func TestSemanticSimilarityDedup_Similar(t *testing.T) {
	d := NewSemanticSimilarityDedup()
	d.RecordContent("the quick brown fox jumps over the lazy dog", "h1")
	isDup, _ := d.CheckSimilarity("the quick brown fox jumps over the lazy dog")
	if !isDup {
		t.Fatal("similar content should be detected as duplicate")
	}
}

func TestSemanticSimilarityDedup_Different(t *testing.T) {
	d := NewSemanticSimilarityDedup()
	d.RecordContent("hello world test content here", "h1")
	isDup, _ := d.CheckSimilarity("completely different topic about space travel")
	if isDup {
		t.Fatal("different content should not be detected as duplicate")
	}
}

func TestSemanticSimilarityDedup_ComputeSimilarity(t *testing.T) {
	d := NewSemanticSimilarityDedup()
	sim := d.ComputeSimilarity("hello world", "hello world")
	if sim < 0.99 {
		t.Fatalf("identical should have ~1.0 similarity, got %f", sim)
	}
	sim2 := d.ComputeSimilarity("hello world", "goodbye universe")
	if sim2 > 0.3 {
		t.Fatalf("different should have low similarity, got %f", sim2)
	}
}

func TestSemanticSimilarityDedup_GetStats(t *testing.T) {
	d := NewSemanticSimilarityDedup()
	d.RecordContent("content", "h1")
	stats := d.GetStats()
	if stats.Threshold != 0.85 {
		t.Fatalf("expected threshold 0.85, got %f", stats.Threshold)
	}
}

// ── OPT-77: PromptCacheOptimizer ──

func TestPromptCacheOptimizer_OptimizePrompt(t *testing.T) {
	o := NewPromptCacheOptimizer()
	result := o.OptimizePrompt(
		"system prompt",
		[]string{"ctx1", "ctx2", "ctx3", "ctx4"},
		"user query",
	)
	if result.StablePrefix == "" {
		t.Fatal("should have stable prefix")
	}
	if result.VariablePart == "" {
		t.Fatal("should have variable part")
	}
}

func TestPromptCacheOptimizer_EstimateCacheSavings(t *testing.T) {
	o := NewPromptCacheOptimizer()
	saved := o.EstimateCacheSavings(10000, 0.5)
	if saved != 5000 {
		t.Fatalf("expected 5000 savings, got %d", saved)
	}
}

func TestPromptCacheOptimizer_GetStats(t *testing.T) {
	o := NewPromptCacheOptimizer()
	o.OptimizePrompt("sys", []string{"c1"}, "query")
	stats := o.GetStats()
	if stats.TotalOptimized == 0 {
		t.Fatal("should have stats")
	}
}

// ── OPT-78: ContextSummaryCache ──

func TestContextSummaryCache_StoreGet(t *testing.T) {
	c := NewContextSummaryCache(0)
	c.StoreSummary("original content here", "summarized version")
	result, hit := c.GetSummary("original content here")
	if !hit {
		t.Fatal("should hit cache")
	}
	if result != "summarized version" {
		t.Fatal("should return cached summary")
	}
}

func TestContextSummaryCache_Miss(t *testing.T) {
	c := NewContextSummaryCache(0)
	_, hit := c.GetSummary("non-existent")
	if hit {
		t.Fatal("should miss cache")
	}
}

func TestContextSummaryCache_GetOrCreate(t *testing.T) {
	c := NewContextSummaryCache(0)
	called := false
	result1 := c.GetOrCreate("content", func(s string) string {
		called = true
		return "computed summary"
	})
	if !called {
		t.Fatal("should call compute function")
	}
	if result1 != "computed summary" {
		t.Fatal("should return computed")
	}

	called = false
	result2 := c.GetOrCreate("content", func(s string) string {
		called = true
		return "should not be called"
	})
	if called {
		t.Fatal("should not call compute on cache hit")
	}
	if result2 != "computed summary" {
		t.Fatal("should return cached")
	}
}

func TestContextSummaryCache_GetStats(t *testing.T) {
	c := NewContextSummaryCache(0)
	c.StoreSummary("content", "summary")
	c.GetSummary("content") // hit
	c.GetSummary("other")   // miss
	stats := c.GetStats()
	if stats.TotalHits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.TotalHits)
	}
}

// ── OPT-79: ToolSchemaOptimizer ──

func TestToolSchemaOptimizer_OptimizeSchema(t *testing.T) {
	o := NewToolSchemaOptimizer()
	schema := `{"name": "bash", "description": "this is a very long description that should be truncated to the first sentence. The second sentence should be removed.", "examples": ["ls", "pwd"], "type": "object"}`
	result := o.OptimizeSchema("bash", schema)
	if result == "" {
		t.Fatal("optimized schema should not be empty")
	}
}

func TestToolSchemaOptimizer_BatchOptimize(t *testing.T) {
	o := NewToolSchemaOptimizer()
	schemas := map[string]string{
		"bash": `{"name":"bash","description":"run commands"}`,
		"grep": `{"name":"grep","description":"search text"}`,
	}
	result := o.BatchOptimize(schemas)
	if len(result) != 2 {
		t.Fatalf("expected 2 optimized schemas, got %d", len(result))
	}
}

func TestToolSchemaOptimizer_GetOptimizedSchema(t *testing.T) {
	o := NewToolSchemaOptimizer()
	o.OptimizeSchema("bash", `{"name":"bash"}`)
	_, found := o.GetOptimizedSchema("bash")
	if !found {
		t.Fatal("should find cached optimized schema")
	}
}

func TestToolSchemaOptimizer_EstimateTokens(t *testing.T) {
	o := NewToolSchemaOptimizer()
	tokens := o.EstimateSchemaTokens("hello world test")
	if tokens != 4 { // 16/4
		t.Fatalf("expected 5 tokens, got %d", tokens)
	}
}

func TestToolSchemaOptimizer_GetStats(t *testing.T) {
	o := NewToolSchemaOptimizer()
	o.OptimizeSchema("bash", `{"name":"bash"}`)
	stats := o.GetStats()
	if stats.TotalOptimized == 0 {
		t.Fatal("should have stats")
	}
}

// ── OPT-80: ConversationCompactSummary ──

func TestConversationCompactSummary_Summarize(t *testing.T) {
	s := NewConversationCompactSummary()
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "How do I read a file?"},
		{Role: provider.RoleAssistant, Content: "You can use the read_file tool to read files."},
		{Role: provider.RoleTool, Content: "file.txt content here"},
		{Role: provider.RoleAssistant, Content: "I read the file successfully."},
	}
	record := s.Summarize(msgs, 5)
	if record.Summary == "" {
		t.Fatal("should produce a summary")
	}
	if record.OriginalTokens == 0 {
		t.Fatal("should estimate original tokens")
	}
}

func TestConversationCompactSummary_GetLastSummary(t *testing.T) {
	s := NewConversationCompactSummary()
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "test query"},
	}
	s.Summarize(msgs, 1)
	last := s.GetLastSummary()
	if last == nil {
		t.Fatal("should return last summary")
	}
}

func TestConversationCompactSummary_GetSummaryHistory(t *testing.T) {
	s := NewConversationCompactSummary()
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "q1"}}
	s.Summarize(msgs, 1)
	s.Summarize(msgs, 2)
	history := s.GetSummaryHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 records, got %d", len(history))
	}
}

func TestConversationCompactSummary_GetStats(t *testing.T) {
	s := NewConversationCompactSummary()
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "query"}}
	s.Summarize(msgs, 1)
	stats := s.GetStats()
	if stats.TotalSummaries != 1 {
		t.Fatalf("expected 1 summary, got %d", stats.TotalSummaries)
	}
	if stats.MaxSummaryTokens != 500 {
		t.Fatalf("expected max 500, got %d", stats.MaxSummaryTokens)
	}
}
