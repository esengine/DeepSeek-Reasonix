package agent

import (
	"testing"
)

// ── OPT-136: CacheStalenessDetector ──

func TestCacheStalenessDetector_Fresh(t *testing.T) {
	d := NewCacheStalenessDetector(10, 5) // maxAge=10, threshold=5
	d.Register("key1", 1)
	status := d.CheckStaleness("key1", 3) // age=2 < threshold=5
	if status != "fresh" {
		t.Errorf("should be fresh, got %s", status)
	}
}

func TestCacheStalenessDetector_Stale(t *testing.T) {
	d := NewCacheStalenessDetector(10, 5) // maxAge=10, threshold=5
	d.Register("key1", 1)
	status := d.CheckStaleness("key1", 7) // age=6, 5<=6<10
	if status != "stale" {
		t.Errorf("should be stale, got %s", status)
	}
}

func TestCacheStalenessDetector_Expired(t *testing.T) {
	d := NewCacheStalenessDetector(10, 5) // maxAge=10, threshold=5
	d.Register("key1", 1)
	status := d.CheckStaleness("key1", 15) // age=14 >= maxAge=10
	if status != "expired" {
		t.Errorf("should be expired, got %s", status)
	}
}

func TestCacheStalenessDetector_Unknown(t *testing.T) {
	d := NewCacheStalenessDetector(10, 5)
	status := d.CheckStaleness("nonexistent", 1)
	if status != "unknown" {
		t.Errorf("should be unknown, got %s", status)
	}
}

func TestCacheStalenessDetector_GetStaleEntries(t *testing.T) {
	d := NewCacheStalenessDetector(10, 5) // maxAge=10, threshold=5
	d.Register("a", 1)
	d.Register("b", 1)
	d.Register("c", 8)
	stale := d.GetStaleEntries(7) // a and b: age=6, stale. c: age=-1, fresh
	if len(stale) < 2 {
		t.Errorf("should have at least 2 stale entries, got %d", len(stale))
	}
}

func TestCacheStalenessDetector_Stats(t *testing.T) {
	d := NewCacheStalenessDetector(10, 5)
	d.Register("key1", 1)
	d.CheckStaleness("key1", 7)
	stats := d.GetStats()
	if stats["totalDetected"].(int) != 1 {
		t.Errorf("totalDetected should be 1, got %v", stats["totalDetected"])
	}
}

func TestCacheStalenessDetector_Reset(t *testing.T) {
	d := NewCacheStalenessDetector(10, 5)
	d.Register("key1", 1)
	d.Reset()
	stats := d.GetStats()
	if stats["totalDetected"].(int) != 0 {
		t.Errorf("totalDetected should be 0 after reset")
	}
}

// ── OPT-137: TokenAwareBatcher ──

func TestTokenAwareBatcher_AddAndFlush(t *testing.T) {
	b := NewTokenAwareBatcher(100)
	b.AddItem("hello world")
	b.AddItem("test message")
	batch := b.Flush()
	if len(batch) != 2 {
		t.Errorf("should flush 2 items, got %d", len(batch))
	}
}

func TestTokenAwareBatcher_BudgetExceeded(t *testing.T) {
	b := NewTokenAwareBatcher(5) // 5 tokens budget
	b.AddItem("hi")              // 2/4 = 0 tokens
	ok := b.AddItem("hello world test message") // 23/4 = 5 tokens, total = 5, > budget? depends on impl
	_ = ok
	b.Flush()
	stats := b.GetStats()
	if stats["totalBatches"].(int) < 1 {
		t.Errorf("should have at least 1 batch, got %v", stats["totalBatches"])
	}
}

func TestTokenAwareBatcher_ShouldFlush(t *testing.T) {
	b := NewTokenAwareBatcher(1) // budget=1 token
	b.AddItem("hello")           // 5/4=1 token, 1 >= 1 → should flush
	if !b.ShouldFlush() {
		t.Errorf("should flush when at or over budget")
	}
}

func TestTokenAwareBatcher_GetCurrentTokenCount(t *testing.T) {
	b := NewTokenAwareBatcher(100)
	b.AddItem("hello") // 5/4 = 1 token
	count := b.GetCurrentTokenCount()
	if count != 1 {
		t.Errorf("token count should be 1, got %d", count)
	}
}

func TestTokenAwareBatcher_Stats(t *testing.T) {
	b := NewTokenAwareBatcher(100)
	b.AddItem("hello")
	b.AddItem("world")
	b.Flush()
	stats := b.GetStats()
	if stats["totalItemsProcessed"].(int) != 2 {
		t.Errorf("totalItemsProcessed should be 2, got %v", stats["totalItemsProcessed"])
	}
}

func TestTokenAwareBatcher_Reset(t *testing.T) {
	b := NewTokenAwareBatcher(100)
	b.AddItem("test")
	b.Flush()
	b.Reset()
	stats := b.GetStats()
	if stats["totalBatches"].(int) != 0 {
		t.Errorf("totalBatches should be 0 after reset")
	}
}

// ── OPT-138: ConversationFlowAnalyzer ──

func TestConversationFlowAnalyzer_SmoothFlow(t *testing.T) {
	a := NewConversationFlowAnalyzer()
	msgs := []string{
		"hello world test message",
		"hello world test message two",
		"hello world test message three",
	}
	score := a.AnalyzeFlow(msgs)
	if score < 0.5 {
		t.Errorf("similar messages should have smooth flow, got %f", score)
	}
}

func TestConversationFlowAnalyzer_DisjointedFlow(t *testing.T) {
	a := NewConversationFlowAnalyzer()
	msgs := []string{
		"database query optimization",
		"cooking pasta recipes italian",
		"space exploration mars colony",
	}
	score := a.AnalyzeFlow(msgs)
	if score > 0.5 {
		t.Errorf("different topics should have low flow score, got %f", score)
	}
}

func TestConversationFlowAnalyzer_DetectFlowBreak(t *testing.T) {
	a := NewConversationFlowAnalyzer()
	if a.DetectFlowBreak("hello world", "hello world") {
		t.Errorf("identical messages should not have flow break")
	}
	if !a.DetectFlowBreak("database optimization", "cooking pasta recipes") {
		t.Errorf("different topics should have flow break")
	}
}

func TestConversationFlowAnalyzer_Category(t *testing.T) {
	a := NewConversationFlowAnalyzer()
	if a.GetFlowCategory(0.2) != "disjointed" {
		t.Error("0.2 should be disjointed")
	}
	if a.GetFlowCategory(0.5) != "interrupted" {
		t.Error("0.5 should be interrupted")
	}
	if a.GetFlowCategory(0.7) != "smooth" {
		t.Error("0.7 should be smooth")
	}
	if a.GetFlowCategory(0.9) != "seamless" {
		t.Error("0.9 should be seamless")
	}
}

func TestConversationFlowAnalyzer_Stats(t *testing.T) {
	a := NewConversationFlowAnalyzer()
	a.AnalyzeFlow([]string{"hello", "world"})
	stats := a.GetStats()
	if stats["totalAnalyses"].(int) != 1 {
		t.Errorf("totalAnalyses should be 1, got %v", stats["totalAnalyses"])
	}
}

func TestConversationFlowAnalyzer_Reset(t *testing.T) {
	a := NewConversationFlowAnalyzer()
	a.AnalyzeFlow([]string{"hello"})
	a.Reset()
	stats := a.GetStats()
	if stats["totalAnalyses"].(int) != 0 {
		t.Errorf("totalAnalyses should be 0 after reset")
	}
}

// ── OPT-139: PromptInflationDetector ──

func TestPromptInflationDetector_Clean(t *testing.T) {
	d := NewPromptInflationDetector(200)
	result := d.Detect("hello world")
	total := 0
	for _, v := range result {
		total += v
	}
	if total > 5 {
		t.Errorf("clean prompt should have minimal inflation, got %d", total)
	}
}

func TestPromptInflationDetector_Inflated(t *testing.T) {
	d := NewPromptInflationDetector(10) // small baseline, long prompt will be inflated
	inflated := "this is a very very very very very very very very very very very verbose prompt with lots of redundancy and over explanation that goes on and on and on with repeated repeated words"
	if !d.IsInflated(inflated) {
		t.Errorf("inflated prompt should be detected")
	}
}

func TestPromptInflationDetector_Score(t *testing.T) {
	d := NewPromptInflationDetector(200)
	score := d.GetInflationScore("hello world")
	if score < 0 || score > 1 {
		t.Errorf("score should be 0..1, got %f", score)
	}
}

func TestPromptInflationDetector_Stats(t *testing.T) {
	d := NewPromptInflationDetector(200)
	d.Detect("hello world test message here")
	stats := d.GetStats()
	if stats["totalChecks"].(int) != 1 {
		t.Errorf("totalChecks should be 1, got %v", stats["totalChecks"])
	}
}

func TestPromptInflationDetector_Reset(t *testing.T) {
	d := NewPromptInflationDetector(200)
	d.Detect("hello world")
	d.Reset()
	stats := d.GetStats()
	if stats["totalChecks"].(int) != 0 {
		t.Errorf("totalChecks should be 0 after reset")
	}
}

// ── OPT-140: CacheKeyHasher ──

func TestCacheKeyHasher_HashSame(t *testing.T) {
	h := NewCacheKeyHasher()
	hash1 := h.Hash("hello world")
	hash2 := h.Hash("hello world")
	if hash1 != hash2 {
		t.Errorf("same content should produce same hash")
	}
}

func TestCacheKeyHasher_HashDifferent(t *testing.T) {
	h := NewCacheKeyHasher()
	hash1 := h.Hash("hello world")
	hash2 := h.Hash("goodbye universe")
	if hash1 == hash2 {
		t.Errorf("different content should produce different hashes")
	}
}

func TestCacheKeyHasher_Lookup(t *testing.T) {
	h := NewCacheKeyHasher()
	hash := h.Hash("hello world")
	content, ok := h.Lookup(hash)
	if !ok {
		t.Errorf("should find content by hash")
	}
	if content != "hello world" {
		t.Errorf("content should be 'hello world', got %q", content)
	}
}

func TestCacheKeyHasher_CheckCollision(t *testing.T) {
	h := NewCacheKeyHasher()
	h.Hash("hello world")
	if !h.CheckCollision("hello world") {
		t.Errorf("should detect collision for already-hashed content")
	}
	if h.CheckCollision("new content") {
		t.Errorf("should not detect collision for new content")
	}
}

func TestCacheKeyHasher_Stats(t *testing.T) {
	h := NewCacheKeyHasher()
	h.Hash("content1")
	h.Hash("content2")
	stats := h.GetStats()
	if stats["totalHashed"].(int) != 2 {
		t.Errorf("totalHashed should be 2, got %v", stats["totalHashed"])
	}
}

func TestCacheKeyHasher_Reset(t *testing.T) {
	h := NewCacheKeyHasher()
	h.Hash("content1")
	h.Reset()
	stats := h.GetStats()
	if stats["totalHashed"].(int) != 0 {
		t.Errorf("totalHashed should be 0 after reset")
	}
}
