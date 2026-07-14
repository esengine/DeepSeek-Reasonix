package agent

import (
	"strings"
	"testing"
)

// ── OPT-156: TokenAwareFragmenter 测试 ──

func TestTokenAwareFragmenter_Fragment_LongText(t *testing.T) {
	f := NewTokenAwareFragmenter(8) // fragmentSize=8, ~32 chars per fragment
	longText := strings.Repeat("hello world ", 100)
	fragments := f.Fragment(longText)
	if len(fragments) <= 0 {
		t.Errorf("expected fragments count > 0, got %d", len(fragments))
	}
	for i, frag := range fragments {
		if frag == "" {
			t.Errorf("fragment %d is empty", i)
		}
	}
}

func TestTokenAwareFragmenter_Fragment_ShortText(t *testing.T) {
	f := NewTokenAwareFragmenter(512)
	fragments := f.Fragment("hello")
	if len(fragments) != 1 {
		t.Errorf("expected 1 fragment for short text, got %d", len(fragments))
	}
}

func TestTokenAwareFragmenter_EstimateFragmentCount(t *testing.T) {
	f := NewTokenAwareFragmenter(8) // fragmentSize=8, each fragment ~32 chars
	text := strings.Repeat("a", 100) // 100 chars / 4 = 25 tokens, 25/8 = 3.125 -> 4
	count := f.EstimateFragmentCount(text)
	if count <= 0 {
		t.Errorf("expected estimated fragment count > 0, got %d", count)
	}
	if count != 4 {
		t.Errorf("expected 4 fragments, got %d", count)
	}
}

func TestTokenAwareFragmenter_GetLastFragmentTokens(t *testing.T) {
	f := NewTokenAwareFragmenter(8)
	text := strings.Repeat("a", 100)
	f.Fragment(text)
	lastTokens := f.GetLastFragmentTokens()
	if lastTokens <= 0 {
		t.Errorf("expected last fragment tokens > 0, got %d", lastTokens)
	}
}

func TestTokenAwareFragmenter_GetStats(t *testing.T) {
	f := NewTokenAwareFragmenter(8)
	f.Fragment(strings.Repeat("a", 100))
	stats := f.GetStats()
	fragmentSize, ok := stats["fragmentSize"]
	if !ok {
		t.Errorf("expected fragmentSize key in stats")
	}
	if fragmentSize.(int) != 8 {
		t.Errorf("expected fragmentSize=8, got %v", fragmentSize)
	}
}

func TestTokenAwareFragmenter_Reset(t *testing.T) {
	f := NewTokenAwareFragmenter(8)
	f.Fragment(strings.Repeat("a", 100))
	f.Reset()
	stats := f.GetStats()
	if stats["fragmentCount"].(int) != 0 {
		t.Errorf("expected fragmentCount=0 after reset, got %v", stats["fragmentCount"])
	}
	if stats["totalFragments"].(int) != 0 {
		t.Errorf("expected totalFragments=0 after reset, got %v", stats["totalFragments"])
	}
	if stats["lastFragmentTokens"].(int) != 0 {
		t.Errorf("expected lastFragmentTokens=0 after reset, got %v", stats["lastFragmentTokens"])
	}
}

// ── OPT-157: CacheWarmingStrategy 测试 ──

func TestCacheWarmingStrategy_ShouldWarm_AfterThreshold(t *testing.T) {
	c := NewCacheWarmingStrategy(2) // warmThreshold=2, need count > 2
	c.RecordAccess("key1")
	c.RecordAccess("key1")
	c.RecordAccess("key1") // 3 accesses, 3 > 2 = true
	if !c.ShouldWarm("key1") {
		t.Errorf("expected ShouldWarm=true after 3 accesses with threshold=2")
	}
}

func TestCacheWarmingStrategy_ShouldWarm_BelowThreshold(t *testing.T) {
	c := NewCacheWarmingStrategy(2) // warmThreshold=2
	c.RecordAccess("key1")
	c.RecordAccess("key1") // 2 accesses, 2 > 2 = false
	if c.ShouldWarm("key1") {
		t.Errorf("expected ShouldWarm=false with 2 accesses and threshold=2")
	}
}

func TestCacheWarmingStrategy_MarkWarmed(t *testing.T) {
	c := NewCacheWarmingStrategy(2)
	c.RecordAccess("key1")
	c.RecordAccess("key1")
	c.RecordAccess("key1") // 3 > 2 = true
	if !c.ShouldWarm("key1") {
		t.Errorf("expected ShouldWarm=true before MarkWarmed")
	}
	c.MarkWarmed("key1")
	if c.ShouldWarm("key1") {
		t.Errorf("expected ShouldWarm=false after MarkWarmed")
	}
}

func TestCacheWarmingStrategy_GetWarmCandidates(t *testing.T) {
	c := NewCacheWarmingStrategy(2)
	// key1: 3 accesses (>2), should be candidate
	c.RecordAccess("key1")
	c.RecordAccess("key1")
	c.RecordAccess("key1")
	// key2: 1 access, should not be candidate
	c.RecordAccess("key2")
	candidates := c.GetWarmCandidates()
	foundKey1 := false
	for _, k := range candidates {
		if k == "key1" {
			foundKey1 = true
		}
		if k == "key2" {
			t.Errorf("key2 should not be in warm candidates")
		}
	}
	if !foundKey1 {
		t.Errorf("expected key1 in warm candidates")
	}
}

func TestCacheWarmingStrategy_GetStats(t *testing.T) {
	c := NewCacheWarmingStrategy(3)
	c.RecordAccess("key1")
	c.RecordAccess("key1")
	c.RecordAccess("key2")
	stats := c.GetStats()
	if stats["trackedKeys"].(int) != 2 {
		t.Errorf("expected trackedKeys=2, got %v", stats["trackedKeys"])
	}
	if stats["warmThreshold"].(int) != 3 {
		t.Errorf("expected warmThreshold=3, got %v", stats["warmThreshold"])
	}
}

func TestCacheWarmingStrategy_Reset(t *testing.T) {
	c := NewCacheWarmingStrategy(2)
	c.RecordAccess("key1")
	c.RecordAccess("key1")
	c.RecordAccess("key1")
	c.MarkWarmed("key1")
	c.Reset()
	stats := c.GetStats()
	if stats["trackedKeys"].(int) != 0 {
		t.Errorf("expected trackedKeys=0 after reset, got %v", stats["trackedKeys"])
	}
	if stats["warmedCount"].(int) != 0 {
		t.Errorf("expected warmedCount=0 after reset, got %v", stats["warmedCount"])
	}
	if stats["totalWarmed"].(int) != 0 {
		t.Errorf("expected totalWarmed=0 after reset, got %v", stats["totalWarmed"])
	}
}

// ── OPT-158: ContextPruningEngine 测试 ──

func TestContextPruningEngine_Prune(t *testing.T) {
	e := NewContextPruningEngine(5) // maxTokens=5
	messages := []string{
		strings.Repeat("a", 40), // 40/4 = 10 tokens
		strings.Repeat("b", 40), // 10 tokens
		strings.Repeat("c", 40), // 10 tokens
	}
	// total tokens = 30, which is > 5
	result := e.Prune(messages, 0) // auto-estimate
	if len(result) >= len(messages) {
		t.Errorf("expected pruned result to have fewer messages, got %d (original %d)", len(result), len(messages))
	}
	if len(result) == 0 {
		t.Errorf("expected at least 1 message remaining after pruning")
	}
	// Verify pruning stats were updated
	stats := e.GetStats()
	if stats["prunedCount"].(int) == 0 {
		t.Errorf("expected prunedCount > 0 after pruning")
	}
}

func TestContextPruningEngine_AddStrategy(t *testing.T) {
	e := NewContextPruningEngine(8192)
	original := e.GetStrategies()
	e.AddStrategy("custom_strategy")
	strategies := e.GetStrategies()
	if len(strategies) != len(original)+1 {
		t.Errorf("expected strategy count to increase by 1, got %d (original %d)", len(strategies), len(original))
	}
	found := false
	for _, s := range strategies {
		if s == "custom_strategy" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected custom_strategy in strategy list")
	}
	// Adding duplicate should not increase count
	e.AddStrategy("custom_strategy")
	strategies2 := e.GetStrategies()
	if len(strategies2) != len(strategies) {
		t.Errorf("expected no duplicate strategy added, got %d (expected %d)", len(strategies2), len(strategies))
	}
}

func TestContextPruningEngine_GetStrategies(t *testing.T) {
	e := NewContextPruningEngine(8192)
	strategies := e.GetStrategies()
	if len(strategies) != 4 {
		t.Errorf("expected 4 default strategies, got %d", len(strategies))
	}
	expected := map[string]bool{
		"low_relevance": false,
		"redundant":     false,
		"outdated":      false,
		"oversized":     false,
	}
	for _, s := range strategies {
		if _, ok := expected[s]; ok {
			expected[s] = true
		}
	}
	for s, found := range expected {
		if !found {
			t.Errorf("expected strategy %q in default strategy list", s)
		}
	}
}

func TestContextPruningEngine_GetStats(t *testing.T) {
	e := NewContextPruningEngine(1024)
	stats := e.GetStats()
	if stats["maxTokens"].(int) != 1024 {
		t.Errorf("expected maxTokens=1024, got %v", stats["maxTokens"])
	}
	if stats["strategyCount"].(int) != 4 {
		t.Errorf("expected strategyCount=4, got %v", stats["strategyCount"])
	}
}

func TestContextPruningEngine_Reset(t *testing.T) {
	e := NewContextPruningEngine(5)
	messages := []string{
		strings.Repeat("a", 40),
		strings.Repeat("b", 40),
		strings.Repeat("c", 40),
	}
	e.Prune(messages, 0)
	stats := e.GetStats()
	if stats["prunedCount"].(int) == 0 {
		t.Errorf("expected prunedCount > 0 before reset")
	}
	e.Reset()
	stats = e.GetStats()
	if stats["prunedCount"].(int) != 0 {
		t.Errorf("expected prunedCount=0 after reset, got %v", stats["prunedCount"])
	}
	if stats["tokensSaved"].(int) != 0 {
		t.Errorf("expected tokensSaved=0 after reset, got %v", stats["tokensSaved"])
	}
}

// ── OPT-159: TokenAwareThrottler 测试 ──

func TestTokenAwareThrottler_Allow_WithinBudget(t *testing.T) {
	th := NewTokenAwareThrottler(100, 1) // rateLimit=100, windowSize=1
	if !th.Allow(30) {
		t.Errorf("expected Allow(30)=true within budget of 100")
	}
	if !th.Allow(50) {
		t.Errorf("expected Allow(50)=true within remaining budget of 70")
	}
}

func TestTokenAwareThrottler_Allow_ExceedsBudget(t *testing.T) {
	th := NewTokenAwareThrottler(100, 1)
	th.Allow(80) // remaining = 20
	if th.Allow(30) {
		t.Errorf("expected Allow(30)=false when only 20 budget remaining")
	}
}

func TestTokenAwareThrottler_GetRemainingBudget(t *testing.T) {
	th := NewTokenAwareThrottler(100, 1)
	th.Allow(30)
	remaining := th.GetRemainingBudget()
	if remaining != 70 {
		t.Errorf("expected remaining budget=70, got %d", remaining)
	}
}

func TestTokenAwareThrottler_GetThrottleRate(t *testing.T) {
	th := NewTokenAwareThrottler(100, 1)
	th.Allow(40) // allowed, remaining=60
	th.Allow(40) // allowed, remaining=20
	th.Allow(30) // throttled, 20+30=50 > 20... wait 20+30=50 > 100? No, 80+30=110 > 100, throttled
	rate := th.GetThrottleRate()
	// 1 throttled / (1 throttled + 2 allowed) = 1/3
	if rate <= 0 {
		t.Errorf("expected throttle rate > 0, got %f", rate)
	}
	expected := 1.0 / 3.0
	if rate != expected {
		t.Errorf("expected throttle rate=%f, got %f", expected, rate)
	}
}

func TestTokenAwareThrottler_GetStats(t *testing.T) {
	th := NewTokenAwareThrottler(100, 1)
	th.Allow(50)
	stats := th.GetStats()
	if stats["rateLimit"].(int) != 100 {
		t.Errorf("expected rateLimit=100, got %v", stats["rateLimit"])
	}
	if stats["allowedCount"].(int) != 1 {
		t.Errorf("expected allowedCount=1, got %v", stats["allowedCount"])
	}
}

func TestTokenAwareThrottler_Reset(t *testing.T) {
	th := NewTokenAwareThrottler(100, 1)
	th.Allow(50)
	th.Allow(60) // throttled, 50+60=110 > 100
	th.Reset()
	stats := th.GetStats()
	if stats["tokensInWindow"].(int) != 0 {
		t.Errorf("expected tokensInWindow=0 after reset, got %v", stats["tokensInWindow"])
	}
	if stats["throttledCount"].(int) != 0 {
		t.Errorf("expected throttledCount=0 after reset, got %v", stats["throttledCount"])
	}
	if stats["allowedCount"].(int) != 0 {
		t.Errorf("expected allowedCount=0 after reset, got %v", stats["allowedCount"])
	}
}

// ── OPT-160: PromptSegmentIndexer 测试 ──

func TestPromptSegmentIndexer_Search(t *testing.T) {
	p := NewPromptSegmentIndexer()
	p.Index("seg1", "hello world foo")
	p.Index("seg2", "bar baz qux")
	p.Index("seg3", "hello baz test")
	results := p.Search("hello")
	foundSeg1 := false
	foundSeg3 := false
	for _, id := range results {
		if id == "seg1" {
			foundSeg1 = true
		}
		if id == "seg3" {
			foundSeg3 = true
		}
		if id == "seg2" {
			t.Errorf("seg2 should not appear in search results for 'hello'")
		}
	}
	if !foundSeg1 {
		t.Errorf("expected seg1 in search results for 'hello'")
	}
	if !foundSeg3 {
		t.Errorf("expected seg3 in search results for 'hello'")
	}
}

func TestPromptSegmentIndexer_GetSegment(t *testing.T) {
	p := NewPromptSegmentIndexer()
	p.Index("seg1", "hello world")
	content, ok := p.GetSegment("seg1")
	if !ok {
		t.Errorf("expected GetSegment to find seg1")
	}
	if content != "hello world" {
		t.Errorf("expected content='hello world', got %q", content)
	}
	_, ok2 := p.GetSegment("nonexistent")
	if ok2 {
		t.Errorf("expected GetSegment to return false for nonexistent id")
	}
}

func TestPromptSegmentIndexer_Remove(t *testing.T) {
	p := NewPromptSegmentIndexer()
	p.Index("seg1", "hello world")
	p.Index("seg2", "hello baz")
	results := p.Search("hello")
	if len(results) != 2 {
		t.Errorf("expected 2 results before remove, got %d", len(results))
	}
	p.Remove("seg1")
	results = p.Search("hello")
	for _, id := range results {
		if id == "seg1" {
			t.Errorf("seg1 should not appear in search results after Remove")
		}
	}
	foundSeg2 := false
	for _, id := range results {
		if id == "seg2" {
			foundSeg2 = true
		}
	}
	if !foundSeg2 {
		t.Errorf("seg2 should still appear in search results after removing seg1")
	}
}

func TestPromptSegmentIndexer_GetStats(t *testing.T) {
	p := NewPromptSegmentIndexer()
	p.Index("seg1", "hello world")
	p.Index("seg2", "foo bar")
	stats := p.GetStats()
	if stats["segmentCount"].(int) != 2 {
		t.Errorf("expected segmentCount=2, got %v", stats["segmentCount"])
	}
}

func TestPromptSegmentIndexer_Reset(t *testing.T) {
	p := NewPromptSegmentIndexer()
	p.Index("seg1", "hello world")
	p.Index("seg2", "foo bar")
	p.Reset()
	stats := p.GetStats()
	if stats["segmentCount"].(int) != 0 {
		t.Errorf("expected segmentCount=0 after reset, got %v", stats["segmentCount"])
	}
	if stats["totalIndexed"].(int) != 0 {
		t.Errorf("expected totalIndexed=0 after reset, got %v", stats["totalIndexed"])
	}
}
