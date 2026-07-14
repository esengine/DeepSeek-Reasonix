package agent

import "testing"

// ── OPT-186: TokenAwarePipeline ──

func TestPipelineAddStageAndProcess(t *testing.T) {
	p := NewTokenAwarePipeline()
	p.AddStage("parse", 10)
	p.AddStage("retrieve", 20)

	result := p.Process("hello world")
	if result != "hello world" {
		t.Errorf("Process() = %q, want %q", result, "hello world")
	}

	stats := p.GetStats()
	totalTokens, ok := stats["totalTokensProcessed"].(int)
	if !ok || totalTokens != 30 {
		t.Errorf("totalTokensProcessed = %v, want 30", stats["totalTokensProcessed"])
	}
}

func TestPipelineGetStages(t *testing.T) {
	p := NewTokenAwarePipeline()
	p.AddStage("parse", 10)
	p.AddStage("retrieve", 20)
	p.AddStage("compress", 5)

	stages := p.GetStages()
	if len(stages) != 3 {
		t.Fatalf("GetStages() returned %d stages, want 3", len(stages))
	}
	if stages[0].Name != "parse" || stages[0].TokenCost != 10 {
		t.Errorf("stage[0] = {Name:%s, TokenCost:%d}, want {parse, 10}", stages[0].Name, stages[0].TokenCost)
	}
	if stages[2].Name != "compress" || stages[2].TokenCost != 5 {
		t.Errorf("stage[2] = {Name:%s, TokenCost:%d}, want {compress, 5}", stages[2].Name, stages[2].TokenCost)
	}

	// Verify GetStages returns a copy: modifying the returned slice must not affect internal state
	stages[0].Name = "modified"
	stages2 := p.GetStages()
	if stages2[0].Name != "parse" {
		t.Errorf("GetStages() did not return a copy; internal stage name became %q", stages2[0].Name)
	}
}

func TestPipelineStats(t *testing.T) {
	p := NewTokenAwarePipeline()
	p.AddStage("parse", 10)
	p.AddStage("retrieve", 20)

	p.Process("input1")
	p.Process("input2")

	stats := p.GetStats()

	stageCount, ok := stats["stageCount"].(int)
	if !ok || stageCount != 2 {
		t.Errorf("stageCount = %v, want 2", stats["stageCount"])
	}

	processedCount, ok := stats["processedCount"].(int)
	if !ok || processedCount != 2 {
		t.Errorf("processedCount = %v, want 2", stats["processedCount"])
	}

	totalTokens, ok := stats["totalTokensProcessed"].(int)
	if !ok || totalTokens != 60 {
		t.Errorf("totalTokensProcessed = %v, want 60", stats["totalTokensProcessed"])
	}
}

func TestPipelineProcessMultipleTimes(t *testing.T) {
	p := NewTokenAwarePipeline()
	p.AddStage("stage1", 15)

	for i := 0; i < 5; i++ {
		p.Process("data")
	}

	stats := p.GetStats()

	processedCount, ok := stats["processedCount"].(int)
	if !ok || processedCount != 5 {
		t.Errorf("processedCount = %v, want 5", stats["processedCount"])
	}

	totalTokens, ok := stats["totalTokensProcessed"].(int)
	if !ok || totalTokens != 75 {
		t.Errorf("totalTokensProcessed = %v, want 75", stats["totalTokensProcessed"])
	}
}

func TestPipelineReset(t *testing.T) {
	p := NewTokenAwarePipeline()
	p.AddStage("parse", 10)
	p.Process("input")

	p.Reset()

	stats := p.GetStats()

	stageCount, ok := stats["stageCount"].(int)
	if !ok || stageCount != 0 {
		t.Errorf("after Reset: stageCount = %v, want 0", stats["stageCount"])
	}

	processedCount, ok := stats["processedCount"].(int)
	if !ok || processedCount != 0 {
		t.Errorf("after Reset: processedCount = %v, want 0", stats["processedCount"])
	}

	totalTokens, ok := stats["totalTokensProcessed"].(int)
	if !ok || totalTokens != 0 {
		t.Errorf("after Reset: totalTokensProcessed = %v, want 0", stats["totalTokensProcessed"])
	}

	stages := p.GetStages()
	if len(stages) != 0 {
		t.Errorf("after Reset: GetStages() returned %d stages, want 0", len(stages))
	}
}

// ── OPT-187: CacheWarmingOptimizer ──

func TestWarmupRecordAndEffectiveness(t *testing.T) {
	c := NewCacheWarmingOptimizer()
	c.RecordWarmup("key1", true)

	eff := c.GetWarmupEffectiveness("key1")
	if eff != 1.0 {
		t.Errorf("GetWarmupEffectiveness(\"key1\") = %v, want 1.0", eff)
	}
}

func TestWarmupEffectivenessNoHit(t *testing.T) {
	c := NewCacheWarmingOptimizer()
	c.RecordWarmup("key1", false)

	eff := c.GetWarmupEffectiveness("key1")
	if eff != 0.0 {
		t.Errorf("GetWarmupEffectiveness(\"key1\") = %v, want 0.0", eff)
	}

	// Non-existent key should also return 0.0
	eff = c.GetWarmupEffectiveness("nonexistent")
	if eff != 0.0 {
		t.Errorf("GetWarmupEffectiveness(\"nonexistent\") = %v, want 0.0", eff)
	}
}

func TestWarmupShouldOptimize(t *testing.T) {
	c := NewCacheWarmingOptimizer()

	// No record -> should optimize
	if !c.ShouldOptimize("key1") {
		t.Errorf("ShouldOptimize(\"key1\") = false, want true (no record)")
	}

	// Record with hit -> should not optimize
	c.RecordWarmup("key1", true)
	if c.ShouldOptimize("key1") {
		t.Errorf("ShouldOptimize(\"key1\") = true, want false (hit after warmup)")
	}

	// Record with no hit -> should optimize
	c.RecordWarmup("key2", false)
	if !c.ShouldOptimize("key2") {
		t.Errorf("ShouldOptimize(\"key2\") = false, want true (no hit after warmup)")
	}
}

func TestWarmupStats(t *testing.T) {
	c := NewCacheWarmingOptimizer()
	c.RecordWarmup("key1", true)
	c.RecordWarmup("key2", false)
	c.RecordWarmup("key1", true) // update existing key1

	stats := c.GetStats()

	trackedKeys, ok := stats["trackedKeys"].(int)
	if !ok || trackedKeys != 2 {
		t.Errorf("trackedKeys = %v, want 2", stats["trackedKeys"])
	}

	optimizedCount, ok := stats["optimizedCount"].(int)
	if !ok || optimizedCount != 3 {
		t.Errorf("optimizedCount = %v, want 3", stats["optimizedCount"])
	}
}

func TestWarmupImprovementTracking(t *testing.T) {
	c := NewCacheWarmingOptimizer()

	// First record: miss (no improvement since hitAfterWarmup=false)
	c.RecordWarmup("key1", false)
	// Second record: hit (prev was miss, now hit -> improvement +1.0)
	c.RecordWarmup("key1", true)

	stats := c.GetStats()

	totalImprovement, ok := stats["totalHitImprovement"].(float64)
	if !ok || totalImprovement != 1.0 {
		t.Errorf("totalHitImprovement = %v, want 1.0", stats["totalHitImprovement"])
	}
}

func TestWarmupReset(t *testing.T) {
	c := NewCacheWarmingOptimizer()
	c.RecordWarmup("key1", true)
	c.RecordWarmup("key2", false)

	c.Reset()

	stats := c.GetStats()

	trackedKeys, ok := stats["trackedKeys"].(int)
	if !ok || trackedKeys != 0 {
		t.Errorf("after Reset: trackedKeys = %v, want 0", stats["trackedKeys"])
	}

	optimizedCount, ok := stats["optimizedCount"].(int)
	if !ok || optimizedCount != 0 {
		t.Errorf("after Reset: optimizedCount = %v, want 0", stats["optimizedCount"])
	}
}

// ── OPT-188: ContextBoundaryOptimizer ──

func TestBoundarySetAndOptimize(t *testing.T) {
	c := NewContextBoundaryOptimizer(10)
	c.SetBoundaries([]int{3, 5, 20, 25})

	optimized := c.Optimize()
	// b=3:  3-0=3  < 10, skip
	// b=5:  5-0=5  < 10, skip
	// b=20: 20-0=20 >= 10, keep
	// b=25: 25-20=5 < 10, skip
	if len(optimized) != 1 {
		t.Fatalf("Optimize() returned %d boundaries, want 1", len(optimized))
	}
	if optimized[0] != 20 {
		t.Errorf("optimized[0] = %d, want 20", optimized[0])
	}
}

func TestBoundaryGetFragmentCount(t *testing.T) {
	c := NewContextBoundaryOptimizer(10)
	c.SetBoundaries([]int{3, 5, 20, 25})

	// Before optimize: 4 boundaries -> 5 fragments
	if c.GetFragmentCount() != 5 {
		t.Errorf("before Optimize: GetFragmentCount() = %d, want 5", c.GetFragmentCount())
	}

	c.Optimize()

	// After optimize: 1 boundary -> 2 fragments
	if c.GetFragmentCount() != 2 {
		t.Errorf("after Optimize: GetFragmentCount() = %d, want 2", c.GetFragmentCount())
	}
}

func TestBoundaryStats(t *testing.T) {
	c := NewContextBoundaryOptimizer(10)
	c.SetBoundaries([]int{3, 5, 20, 25})
	c.Optimize()

	stats := c.GetStats()

	boundaryCount, ok := stats["boundaryCount"].(int)
	if !ok || boundaryCount != 1 {
		t.Errorf("boundaryCount = %v, want 1", stats["boundaryCount"])
	}

	optimizationCount, ok := stats["optimizationCount"].(int)
	if !ok || optimizationCount != 1 {
		t.Errorf("optimizationCount = %v, want 1", stats["optimizationCount"])
	}

	fragmentsReduced, ok := stats["fragmentsReduced"].(int)
	if !ok || fragmentsReduced != 3 {
		t.Errorf("fragmentsReduced = %v, want 3", stats["fragmentsReduced"])
	}

	minSegmentSize, ok := stats["minSegmentSize"].(int)
	if !ok || minSegmentSize != 10 {
		t.Errorf("minSegmentSize = %v, want 10", stats["minSegmentSize"])
	}
}

func TestBoundaryOptimizeNoChange(t *testing.T) {
	c := NewContextBoundaryOptimizer(10)
	c.SetBoundaries([]int{10, 20, 30})

	optimized := c.Optimize()
	if len(optimized) != 3 {
		t.Fatalf("Optimize() returned %d boundaries, want 3 (no change expected)", len(optimized))
	}
	if optimized[0] != 10 || optimized[1] != 20 || optimized[2] != 30 {
		t.Errorf("optimized = %v, want [10 20 30]", optimized)
	}

	stats := c.GetStats()
	fragmentsReduced, ok := stats["fragmentsReduced"].(int)
	if !ok || fragmentsReduced != 0 {
		t.Errorf("fragmentsReduced = %v, want 0", stats["fragmentsReduced"])
	}
}

func TestBoundaryOptimizeEmpty(t *testing.T) {
	c := NewContextBoundaryOptimizer(10)
	c.SetBoundaries([]int{})

	optimized := c.Optimize()
	if len(optimized) != 0 {
		t.Errorf("Optimize() returned %d boundaries, want 0", len(optimized))
	}
	if c.GetFragmentCount() != 1 {
		t.Errorf("GetFragmentCount() = %d, want 1", c.GetFragmentCount())
	}
}

func TestBoundaryReset(t *testing.T) {
	c := NewContextBoundaryOptimizer(10)
	c.SetBoundaries([]int{3, 5, 20, 25})
	c.Optimize()

	c.Reset()

	stats := c.GetStats()

	boundaryCount, ok := stats["boundaryCount"].(int)
	if !ok || boundaryCount != 0 {
		t.Errorf("after Reset: boundaryCount = %v, want 0", stats["boundaryCount"])
	}

	optimizationCount, ok := stats["optimizationCount"].(int)
	if !ok || optimizationCount != 0 {
		t.Errorf("after Reset: optimizationCount = %v, want 0", stats["optimizationCount"])
	}

	if c.GetFragmentCount() != 1 {
		t.Errorf("after Reset: GetFragmentCount() = %d, want 1", c.GetFragmentCount())
	}
}

// ── OPT-189: TokenAwareLoadBalancer ──

func TestLoadBalancerAddAndDistribute(t *testing.T) {
	lb := NewTokenAwareLoadBalancer()
	lb.AddHandler("A", 100)
	lb.AddHandler("B", 100)

	// First distribute: both load 0, assigns to first min-load handler (A)
	name, ok := lb.Distribute(50)
	if !ok || name != "A" {
		t.Errorf("Distribute(50) = (%q, %v), want (\"A\", true)", name, ok)
	}

	// Second distribute: A=50, B=0, B has lower load
	name, ok = lb.Distribute(50)
	if !ok || name != "B" {
		t.Errorf("Distribute(50) = (%q, %v), want (\"B\", true)", name, ok)
	}

	// Both at 50, assigns to first min-load handler (A)
	name, ok = lb.Distribute(30)
	if !ok || name != "A" {
		t.Errorf("Distribute(30) = (%q, %v), want (\"A\", true)", name, ok)
	}

	if lb.GetHandlerLoad("A") != 80 {
		t.Errorf("GetHandlerLoad(\"A\") = %d, want 80", lb.GetHandlerLoad("A"))
	}
	if lb.GetHandlerLoad("B") != 50 {
		t.Errorf("GetHandlerLoad(\"B\") = %d, want 50", lb.GetHandlerLoad("B"))
	}
}

func TestLoadBalancerRelease(t *testing.T) {
	lb := NewTokenAwareLoadBalancer()
	lb.AddHandler("A", 100)

	lb.Distribute(50)
	if lb.GetHandlerLoad("A") != 50 {
		t.Fatalf("GetHandlerLoad(\"A\") = %d, want 50 before release", lb.GetHandlerLoad("A"))
	}

	lb.Release("A", 20)
	if lb.GetHandlerLoad("A") != 30 {
		t.Errorf("after Release(\"A\", 20): GetHandlerLoad(\"A\") = %d, want 30", lb.GetHandlerLoad("A"))
	}

	// Release more than current load -> should clamp to 0
	lb.Release("A", 100)
	if lb.GetHandlerLoad("A") != 0 {
		t.Errorf("after Release(\"A\", 100): GetHandlerLoad(\"A\") = %d, want 0 (clamped)", lb.GetHandlerLoad("A"))
	}
}

func TestLoadBalancerGetHandlerLoad(t *testing.T) {
	lb := NewTokenAwareLoadBalancer()
	lb.AddHandler("A", 100)

	if lb.GetHandlerLoad("A") != 0 {
		t.Errorf("GetHandlerLoad(\"A\") = %d, want 0 (initial)", lb.GetHandlerLoad("A"))
	}

	// Non-existent handler should return -1
	if lb.GetHandlerLoad("X") != -1 {
		t.Errorf("GetHandlerLoad(\"X\") = %d, want -1 (not found)", lb.GetHandlerLoad("X"))
	}
}

func TestLoadBalancerStats(t *testing.T) {
	lb := NewTokenAwareLoadBalancer()
	lb.AddHandler("A", 100)
	lb.AddHandler("B", 100)

	lb.Distribute(50)
	lb.Distribute(30)

	stats := lb.GetStats()

	handlerCount, ok := stats["handlerCount"].(int)
	if !ok || handlerCount != 2 {
		t.Errorf("handlerCount = %v, want 2", stats["handlerCount"])
	}

	balancedCount, ok := stats["balancedCount"].(int)
	if !ok || balancedCount != 2 {
		t.Errorf("balancedCount = %v, want 2", stats["balancedCount"])
	}

	totalTokens, ok := stats["totalTokensBalanced"].(int)
	if !ok || totalTokens != 80 {
		t.Errorf("totalTokensBalanced = %v, want 80", stats["totalTokensBalanced"])
	}
}

func TestLoadBalancerDistributeNoCapacity(t *testing.T) {
	lb := NewTokenAwareLoadBalancer()
	lb.AddHandler("A", 100)

	// Fill to capacity
	name, ok := lb.Distribute(100)
	if !ok || name != "A" {
		t.Fatalf("Distribute(100) = (%q, %v), want (\"A\", true)", name, ok)
	}

	// Exceeds capacity -> should fail
	name, ok = lb.Distribute(1)
	if ok || name != "" {
		t.Errorf("Distribute(1) = (%q, %v), want (\"\", false) (no capacity)", name, ok)
	}
}

func TestLoadBalancerReset(t *testing.T) {
	lb := NewTokenAwareLoadBalancer()
	lb.AddHandler("A", 100)
	lb.Distribute(50)

	lb.Reset()

	stats := lb.GetStats()

	handlerCount, ok := stats["handlerCount"].(int)
	if !ok || handlerCount != 0 {
		t.Errorf("after Reset: handlerCount = %v, want 0", stats["handlerCount"])
	}

	balancedCount, ok := stats["balancedCount"].(int)
	if !ok || balancedCount != 0 {
		t.Errorf("after Reset: balancedCount = %v, want 0", stats["balancedCount"])
	}
}

// ── OPT-190: PromptSegmentCacheV3 ──

func TestCacheV3PutAndGet(t *testing.T) {
	c := NewPromptSegmentCacheV3(10)
	c.Put("k1", "content1", 10)

	entry, ok := c.Get("k1")
	if !ok {
		t.Fatalf("Get(\"k1\") returned ok=false, want true")
	}
	if entry.Content != "content1" {
		t.Errorf("entry.Content = %q, want %q", entry.Content, "content1")
	}
	if entry.Tokens != 10 {
		t.Errorf("entry.Tokens = %d, want 10", entry.Tokens)
	}
	if entry.AccessCount != 1 {
		t.Errorf("entry.AccessCount = %d, want 1 (first access)", entry.AccessCount)
	}

	// Additional Gets increase AccessCount
	c.Get("k1")
	entry, _ = c.Get("k1")
	if entry.AccessCount != 3 {
		t.Errorf("entry.AccessCount = %d, want 3 (after 3 Gets)", entry.AccessCount)
	}

	// Miss on non-existent key
	_, ok = c.Get("missing")
	if ok {
		t.Errorf("Get(\"missing\") returned ok=true, want false")
	}
}

func TestCacheV3LRUEviction(t *testing.T) {
	c := NewPromptSegmentCacheV3(2)
	c.Put("a", "ca", 1)
	c.Put("b", "cb", 2)

	// Access "a" to make it recently used (LRU order becomes [b, a])
	c.Get("a")

	// Put "c": cache full, should evict "b" (head of accessOrder = LRU)
	c.Put("c", "cc", 3)

	// "b" should have been evicted
	if _, ok := c.Get("b"); ok {
		t.Errorf("Get(\"b\") returned ok=true, want false (should be evicted by LRU)")
	}
	// "a" should still exist
	if _, ok := c.Get("a"); !ok {
		t.Errorf("Get(\"a\") returned ok=false, want true (should still exist)")
	}
	// "c" should exist
	if _, ok := c.Get("c"); !ok {
		t.Errorf("Get(\"c\") returned ok=false, want true")
	}
}

func TestCacheV3Invalidate(t *testing.T) {
	c := NewPromptSegmentCacheV3(10)
	c.Put("k1", "content1", 10)

	// Verify entry exists before invalidation
	if _, ok := c.Get("k1"); !ok {
		t.Fatalf("Get(\"k1\") returned ok=false before Invalidate")
	}

	c.Invalidate("k1")

	if _, ok := c.Get("k1"); ok {
		t.Errorf("Get(\"k1\") returned ok=true after Invalidate, want false")
	}
}

func TestCacheV3GetHitRate(t *testing.T) {
	c := NewPromptSegmentCacheV3(10)
	c.Put("a", "ca", 1)

	c.Get("a") // hit
	c.Get("b") // miss

	rate := c.GetHitRate()
	if rate != 0.5 {
		t.Errorf("GetHitRate() = %v, want 0.5", rate)
	}
}

func TestCacheV3Stats(t *testing.T) {
	c := NewPromptSegmentCacheV3(10)
	c.Put("a", "ca", 1)
	c.Put("b", "cb", 2)

	c.Get("a") // hit
	c.Get("a") // hit
	c.Get("x") // miss

	stats := c.GetStats()

	entries, ok := stats["entries"].(int)
	if !ok || entries != 2 {
		t.Errorf("entries = %v, want 2", stats["entries"])
	}

	maxEntries, ok := stats["maxEntries"].(int)
	if !ok || maxEntries != 10 {
		t.Errorf("maxEntries = %v, want 10", stats["maxEntries"])
	}

	hits, ok := stats["hits"].(int)
	if !ok || hits != 2 {
		t.Errorf("hits = %v, want 2", stats["hits"])
	}

	misses, ok := stats["misses"].(int)
	if !ok || misses != 1 {
		t.Errorf("misses = %v, want 1", stats["misses"])
	}
}

func TestCacheV3Reset(t *testing.T) {
	c := NewPromptSegmentCacheV3(10)
	c.Put("a", "ca", 1)
	c.Get("a")
	c.Get("b")

	c.Reset()

	stats := c.GetStats()

	entries, ok := stats["entries"].(int)
	if !ok || entries != 0 {
		t.Errorf("after Reset: entries = %v, want 0", stats["entries"])
	}

	hits, ok := stats["hits"].(int)
	if !ok || hits != 0 {
		t.Errorf("after Reset: hits = %v, want 0", stats["hits"])
	}

	misses, ok := stats["misses"].(int)
	if !ok || misses != 0 {
		t.Errorf("after Reset: misses = %v, want 0", stats["misses"])
	}

	if c.GetHitRate() != 0 {
		t.Errorf("after Reset: GetHitRate() = %v, want 0", c.GetHitRate())
	}
}
