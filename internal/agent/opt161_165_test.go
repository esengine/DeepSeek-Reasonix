package agent

import (
	"testing"
)

// ════════════════════════════════════════════════════════════════════════
// OPT-161: TokenAwarePrioritizerV2 测试
// 优先级分数 = relevance / max(tokenCost, 1)，按 PriorityScore 降序排序。
// ════════════════════════════════════════════════════════════════════════

// TestTokenAwarePrioritizerV2_AddRankGetTopN 验证 Add + Rank + GetTopN：
// 高 relevance 低 tokenCost 的项目（PriorityScore 高）应排在前面。
func TestTokenAwarePrioritizerV2_AddRankGetTopN(t *testing.T) {
	p := NewTokenAwarePrioritizerV2()
	p.Add("high-rel-low-cost", 10, 10.0)  // score = 10/10 = 1.0
	p.Add("low-rel-high-cost", 100, 1.0)  // score = 1/100 = 0.01
	p.Add("mid", 10, 5.0)                 // score = 5/10 = 0.5
	p.Rank()

	top := p.GetTopN(2)
	if len(top) != 2 {
		t.Errorf("GetTopN(2) len = %d, want 2", len(top))
	}
	if top[0].ID != "high-rel-low-cost" {
		t.Errorf("top[0].ID = %q, want %q", top[0].ID, "high-rel-low-cost")
	}
	if top[1].ID != "mid" {
		t.Errorf("top[1].ID = %q, want %q", top[1].ID, "mid")
	}
	if top[0].PriorityScore != 1.0 {
		t.Errorf("top[0].PriorityScore = %v, want 1.0", top[0].PriorityScore)
	}
	if top[1].PriorityScore != 0.5 {
		t.Errorf("top[1].PriorityScore = %v, want 0.5", top[1].PriorityScore)
	}
}

// TestTokenAwarePrioritizerV2_GetTopNBeforeRank 验证 Rank 前 GetTopN 仍可用
// （按插入顺序返回前 n 项）。
func TestTokenAwarePrioritizerV2_GetTopNBeforeRank(t *testing.T) {
	p := NewTokenAwarePrioritizerV2()
	p.Add("first", 10, 1.0)  // score 0.1
	p.Add("second", 10, 2.0) // score 0.2
	p.Add("third", 10, 3.0)  // score 0.3

	// 未调用 Rank，应按插入顺序返回
	top := p.GetTopN(2)
	if len(top) != 2 {
		t.Errorf("GetTopN(2) len = %d, want 2", len(top))
	}
	if top[0].ID != "first" {
		t.Errorf("top[0].ID = %q, want %q (insertion order before Rank)", top[0].ID, "first")
	}
	if top[1].ID != "second" {
		t.Errorf("top[1].ID = %q, want %q (insertion order before Rank)", top[1].ID, "second")
	}
}

// TestTokenAwarePrioritizerV2_Stats 验证 Stats 中的 itemCount 与 sortCount。
func TestTokenAwarePrioritizerV2_Stats(t *testing.T) {
	p := NewTokenAwarePrioritizerV2()
	p.Add("a", 10, 1.0)
	p.Add("b", 10, 2.0)
	p.Add("c", 10, 3.0)
	p.Rank()
	p.Rank()

	stats := p.GetStats()
	itemCount, ok := stats["itemCount"].(int)
	if !ok {
		t.Errorf("stats[itemCount] type = %T, want int", stats["itemCount"])
	}
	if itemCount != 3 {
		t.Errorf("itemCount = %d, want 3", itemCount)
	}
	sortCount, ok := stats["sortCount"].(int)
	if !ok {
		t.Errorf("stats[sortCount] type = %T, want int", stats["sortCount"])
	}
	if sortCount != 2 {
		t.Errorf("sortCount = %d, want 2", sortCount)
	}
	ranked, ok := stats["ranked"].(bool)
	if !ok {
		t.Errorf("stats[ranked] type = %T, want bool", stats["ranked"])
	}
	if !ranked {
		t.Errorf("ranked = false, want true after Rank")
	}
}

// TestTokenAwarePrioritizerV2_GetTopNEdgeCases 验证 n<=0 返回空切片，
// n 超过项目总数时返回全部。
func TestTokenAwarePrioritizerV2_GetTopNEdgeCases(t *testing.T) {
	p := NewTokenAwarePrioritizerV2()
	p.Add("a", 10, 1.0)
	p.Add("b", 10, 2.0)
	p.Rank()

	if got := p.GetTopN(0); len(got) != 0 {
		t.Errorf("GetTopN(0) len = %d, want 0", len(got))
	}
	if got := p.GetTopN(-1); len(got) != 0 {
		t.Errorf("GetTopN(-1) len = %d, want 0", len(got))
	}
	all := p.GetTopN(100)
	if len(all) != 2 {
		t.Errorf("GetTopN(100) len = %d, want 2 (all items)", len(all))
	}
}

// TestTokenAwarePrioritizerV2_PriorityScoreZeroCost 验证 tokenCost<1 时按 1 计算分数。
func TestTokenAwarePrioritizerV2_PriorityScoreZeroCost(t *testing.T) {
	p := NewTokenAwarePrioritizerV2()
	p.Add("zero-cost", 0, 5.0) // score = 5/max(0,1) = 5.0
	p.Add("normal", 5, 5.0)    // score = 5/5 = 1.0
	p.Rank()

	top := p.GetTopN(2)
	if top[0].ID != "zero-cost" {
		t.Errorf("top[0].ID = %q, want %q", top[0].ID, "zero-cost")
	}
	if top[0].PriorityScore != 5.0 {
		t.Errorf("top[0].PriorityScore = %v, want 5.0 (cost clamped to 1)", top[0].PriorityScore)
	}
	if top[1].PriorityScore != 1.0 {
		t.Errorf("top[1].PriorityScore = %v, want 1.0", top[1].PriorityScore)
	}
}

// TestTokenAwarePrioritizerV2_Reset 验证 Reset 后状态全部清空。
func TestTokenAwarePrioritizerV2_Reset(t *testing.T) {
	p := NewTokenAwarePrioritizerV2()
	p.Add("a", 10, 1.0)
	p.Rank()
	p.Reset()

	stats := p.GetStats()
	itemCount := stats["itemCount"].(int)
	if itemCount != 0 {
		t.Errorf("after Reset itemCount = %d, want 0", itemCount)
	}
	sortCount := stats["sortCount"].(int)
	if sortCount != 0 {
		t.Errorf("after Reset sortCount = %d, want 0", sortCount)
	}
	ranked := stats["ranked"].(bool)
	if ranked {
		t.Errorf("after Reset ranked = true, want false")
	}
	if got := p.GetTopN(5); len(got) != 0 {
		t.Errorf("after Reset GetTopN(5) len = %d, want 0", len(got))
	}
}

// ════════════════════════════════════════════════════════════════════════
// OPT-162: CachePrefetchScheduler 测试
// 按 Priority 降序调度预取任务，数值越大越优先。
// ════════════════════════════════════════════════════════════════════════

// TestCachePrefetchScheduler_ScheduleNext 验证高优先级任务先出队。
func TestCachePrefetchScheduler_ScheduleNext(t *testing.T) {
	s := NewCachePrefetchScheduler(4)
	s.Schedule("low", 1, 100)
	s.Schedule("high", 10, 50)
	s.Schedule("mid", 5, 80)

	task, ok := s.Next()
	if !ok {
		t.Errorf("Next() ok = false, want true")
	}
	if task.Key != "high" {
		t.Errorf("first Next().Key = %q, want %q", task.Key, "high")
	}
	if task.Priority != 10 {
		t.Errorf("first Next().Priority = %d, want 10", task.Priority)
	}
	if task.EstimatedTokens != 50 {
		t.Errorf("first Next().EstimatedTokens = %d, want 50", task.EstimatedTokens)
	}

	task2, _ := s.Next()
	if task2.Key != "mid" {
		t.Errorf("second Next().Key = %q, want %q", task2.Key, "mid")
	}
	task3, _ := s.Next()
	if task3.Key != "low" {
		t.Errorf("third Next().Key = %q, want %q", task3.Key, "low")
	}
}

// TestCachePrefetchScheduler_CompleteSuccess 验证 Complete(success=true) 增加 completedCount。
func TestCachePrefetchScheduler_CompleteSuccess(t *testing.T) {
	s := NewCachePrefetchScheduler(4)
	s.Schedule("k1", 1, 10)
	_, _ = s.Next()
	s.Complete("k1", true)

	stats := s.GetStats()
	completedCount := stats["completedCount"].(int)
	if completedCount != 1 {
		t.Errorf("completedCount = %d, want 1", completedCount)
	}
	failedCount := stats["failedCount"].(int)
	if failedCount != 0 {
		t.Errorf("failedCount = %d, want 0", failedCount)
	}
}

// TestCachePrefetchScheduler_CompleteFailure 验证 Complete(success=false) 增加 failedCount。
func TestCachePrefetchScheduler_CompleteFailure(t *testing.T) {
	s := NewCachePrefetchScheduler(4)
	s.Schedule("k1", 1, 10)
	_, _ = s.Next()
	s.Complete("k1", false)

	stats := s.GetStats()
	failedCount := stats["failedCount"].(int)
	if failedCount != 1 {
		t.Errorf("failedCount = %d, want 1", failedCount)
	}
	completedCount := stats["completedCount"].(int)
	if completedCount != 0 {
		t.Errorf("completedCount = %d, want 0", completedCount)
	}
}

// TestCachePrefetchScheduler_NextEmpty 验证空队列 Next 返回 false 与零值。
func TestCachePrefetchScheduler_NextEmpty(t *testing.T) {
	s := NewCachePrefetchScheduler(4)
	task, ok := s.Next()
	if ok {
		t.Errorf("Next() on empty queue ok = true, want false")
	}
	if task.Key != "" {
		t.Errorf("empty Next().Key = %q, want empty string", task.Key)
	}
	if task.Priority != 0 {
		t.Errorf("empty Next().Priority = %d, want 0", task.Priority)
	}
}

// TestCachePrefetchScheduler_Stats 验证 Stats 中的 maxConcurrent 与 queueSize。
func TestCachePrefetchScheduler_Stats(t *testing.T) {
	s := NewCachePrefetchScheduler(8)
	s.Schedule("k1", 1, 10)
	s.Schedule("k2", 2, 20)

	stats := s.GetStats()
	maxConcurrent := stats["maxConcurrent"].(int)
	if maxConcurrent != 8 {
		t.Errorf("maxConcurrent = %d, want 8", maxConcurrent)
	}
	queueSize := stats["queueSize"].(int)
	if queueSize != 2 {
		t.Errorf("queueSize = %d, want 2", queueSize)
	}
}

// TestCachePrefetchScheduler_Reset 验证 Reset 后清空队列与计数（保留 maxConcurrent）。
func TestCachePrefetchScheduler_Reset(t *testing.T) {
	s := NewCachePrefetchScheduler(4)
	s.Schedule("k1", 1, 10)
	_, _ = s.Next()
	s.Complete("k1", true)
	s.Reset()

	stats := s.GetStats()
	queueSize := stats["queueSize"].(int)
	if queueSize != 0 {
		t.Errorf("after Reset queueSize = %d, want 0", queueSize)
	}
	completedCount := stats["completedCount"].(int)
	if completedCount != 0 {
		t.Errorf("after Reset completedCount = %d, want 0", completedCount)
	}
	failedCount := stats["failedCount"].(int)
	if failedCount != 0 {
		t.Errorf("after Reset failedCount = %d, want 0", failedCount)
	}
	maxConcurrent := stats["maxConcurrent"].(int)
	if maxConcurrent != 4 {
		t.Errorf("after Reset maxConcurrent = %d, want 4 (preserved)", maxConcurrent)
	}
}

// ════════════════════════════════════════════════════════════════════════
// OPT-163: ContextWindowMonitor 测试
// 利用率 = usedTokens / windowSize，维护峰值与平均使用量。
// ════════════════════════════════════════════════════════════════════════

// TestContextWindowMonitor_RecordGetUtilization 验证利用率计算。
func TestContextWindowMonitor_RecordGetUtilization(t *testing.T) {
	m := NewContextWindowMonitor(1000)
	m.Record(250)

	util := m.GetUtilization()
	if util != 0.25 {
		t.Errorf("GetUtilization() = %v, want 0.25", util)
	}
}

// TestContextWindowMonitor_GetPeakUsage 验证历史峰值使用量。
func TestContextWindowMonitor_GetPeakUsage(t *testing.T) {
	m := NewContextWindowMonitor(1000)
	m.Record(100)
	m.Record(500)
	m.Record(200)

	peak := m.GetPeakUsage()
	if peak != 500 {
		t.Errorf("GetPeakUsage() = %d, want 500", peak)
	}
}

// TestContextWindowMonitor_GetAvgUsage 验证平均使用量。
func TestContextWindowMonitor_GetAvgUsage(t *testing.T) {
	m := NewContextWindowMonitor(1000)
	m.Record(100)
	m.Record(300)
	m.Record(200)

	avg := m.GetAvgUsage()
	if avg != 200.0 {
		t.Errorf("GetAvgUsage() = %v, want 200.0", avg)
	}
}

// TestContextWindowMonitor_Stats 验证 Stats 中的 windowSize 等字段。
func TestContextWindowMonitor_Stats(t *testing.T) {
	m := NewContextWindowMonitor(8000)
	m.Record(2000)

	stats := m.GetStats()
	windowSize := stats["windowSize"].(int)
	if windowSize != 8000 {
		t.Errorf("windowSize = %d, want 8000", windowSize)
	}
	usedTokens := stats["usedTokens"].(int)
	if usedTokens != 2000 {
		t.Errorf("usedTokens = %d, want 2000", usedTokens)
	}
	sampleCount := stats["sampleCount"].(int)
	if sampleCount != 1 {
		t.Errorf("sampleCount = %d, want 1", sampleCount)
	}
	utilization := stats["utilization"].(float64)
	if utilization != 0.25 {
		t.Errorf("utilization = %v, want 0.25", utilization)
	}
}

// TestContextWindowMonitor_Reset 验证 Reset 后状态清空（保留 windowSize）。
func TestContextWindowMonitor_Reset(t *testing.T) {
	m := NewContextWindowMonitor(1000)
	m.Record(500)
	m.Record(800)
	m.Reset()

	if util := m.GetUtilization(); util != 0.0 {
		t.Errorf("after Reset GetUtilization() = %v, want 0.0", util)
	}
	if peak := m.GetPeakUsage(); peak != 0 {
		t.Errorf("after Reset GetPeakUsage() = %d, want 0", peak)
	}
	if avg := m.GetAvgUsage(); avg != 0.0 {
		t.Errorf("after Reset GetAvgUsage() = %v, want 0.0", avg)
	}
	stats := m.GetStats()
	sampleCount := stats["sampleCount"].(int)
	if sampleCount != 0 {
		t.Errorf("after Reset sampleCount = %d, want 0", sampleCount)
	}
	windowSize := stats["windowSize"].(int)
	if windowSize != 1000 {
		t.Errorf("after Reset windowSize = %d, want 1000 (preserved)", windowSize)
	}
}

// ════════════════════════════════════════════════════════════════════════
// OPT-164: TokenAwareDeduplicator 测试
// 基于哈希完全匹配去重，哈希与原文同时匹配才视为重复。
// ════════════════════════════════════════════════════════════════════════

// TestTokenAwareDeduplicator_IsDuplicateSameText 验证相同文本第二次返回 true。
func TestTokenAwareDeduplicator_IsDuplicateSameText(t *testing.T) {
	d := NewTokenAwareDeduplicator(0.9)
	if d.IsDuplicate("hello world") {
		t.Errorf("first IsDuplicate() = true, want false")
	}
	if !d.IsDuplicate("hello world") {
		t.Errorf("second IsDuplicate() = false, want true")
	}
	if !d.IsDuplicate("hello world") {
		t.Errorf("third IsDuplicate() = false, want true")
	}
}

// TestTokenAwareDeduplicator_IsDuplicateDifferentText 验证不同文本返回 false。
func TestTokenAwareDeduplicator_IsDuplicateDifferentText(t *testing.T) {
	d := NewTokenAwareDeduplicator(0.9)
	d.IsDuplicate("hello")
	if d.IsDuplicate("world") {
		t.Errorf("IsDuplicate(different text) = true, want false")
	}
}

// TestTokenAwareDeduplicator_Deduplicate 验证从列表移除重复项。
func TestTokenAwareDeduplicator_Deduplicate(t *testing.T) {
	d := NewTokenAwareDeduplicator(0.9)
	texts := []string{"aaa", "bbb", "aaa", "ccc", "bbb"}
	result := d.Deduplicate(texts)

	if len(result) != 3 {
		t.Errorf("Deduplicate len = %d, want 3", len(result))
	}
	expected := []string{"aaa", "bbb", "ccc"}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("Deduplicate[%d] = %q, want %q", i, result[i], want)
		}
	}
}

// TestTokenAwareDeduplicator_Stats 验证 Stats 中的 dedupCount 等字段。
func TestTokenAwareDeduplicator_Stats(t *testing.T) {
	d := NewTokenAwareDeduplicator(0.9)
	d.IsDuplicate("hello")
	d.IsDuplicate("hello") // dedup
	d.IsDuplicate("hello") // dedup

	stats := d.GetStats()
	dedupCount := stats["dedupCount"].(int)
	if dedupCount != 2 {
		t.Errorf("dedupCount = %d, want 2", dedupCount)
	}
	trackedItems := stats["trackedItems"].(int)
	if trackedItems != 1 {
		t.Errorf("trackedItems = %d, want 1", trackedItems)
	}
	similarityThreshold := stats["similarityThreshold"].(float64)
	if similarityThreshold != 0.9 {
		t.Errorf("similarityThreshold = %v, want 0.9", similarityThreshold)
	}
}

// TestTokenAwareDeduplicator_TokensSaved 验证 tokensSaved 随去重累加。
func TestTokenAwareDeduplicator_TokensSaved(t *testing.T) {
	d := NewTokenAwareDeduplicator(0.9)
	text := "abcdefghijklmnop" // 16 chars -> 16/4 = 4 tokens
	d.IsDuplicate(text)
	d.IsDuplicate(text) // dedup, tokensSaved += 4

	stats := d.GetStats()
	tokensSaved := stats["tokensSaved"].(int)
	if tokensSaved != 4 {
		t.Errorf("tokensSaved = %d, want 4", tokensSaved)
	}
}

// TestTokenAwareDeduplicator_Reset 验证 Reset 后状态清空（保留 similarityThreshold）。
func TestTokenAwareDeduplicator_Reset(t *testing.T) {
	d := NewTokenAwareDeduplicator(0.9)
	d.IsDuplicate("hello")
	d.IsDuplicate("hello")
	d.Reset()

	// Reset 后相同文本应被视为新文本
	if d.IsDuplicate("hello") {
		t.Errorf("after Reset IsDuplicate(hello) = true, want false")
	}
	stats := d.GetStats()
	dedupCount := stats["dedupCount"].(int)
	if dedupCount != 0 {
		t.Errorf("after Reset dedupCount = %d, want 0", dedupCount)
	}
	tokensSaved := stats["tokensSaved"].(int)
	if tokensSaved != 0 {
		t.Errorf("after Reset tokensSaved = %d, want 0", tokensSaved)
	}
	trackedItems := stats["trackedItems"].(int)
	if trackedItems != 1 {
		t.Errorf("after Reset trackedItems = %d, want 1 (hello re-recorded)", trackedItems)
	}
}

// ════════════════════════════════════════════════════════════════════════
// OPT-165: PromptAssemblyOptimizer 测试
// 将 Cacheable=true 段落前置以提升缓存命中，按换行组装完整提示。
// ════════════════════════════════════════════════════════════════════════

// TestPromptAssemblyOptimizer_AddSegmentOptimize 验证 cacheable 段落前置。
func TestPromptAssemblyOptimizer_AddSegmentOptimize(t *testing.T) {
	o := NewPromptAssemblyOptimizer()
	o.AddSegment("dyn1", "dynamic-1", false)
	o.AddSegment("cache1", "cache-1", true)
	o.AddSegment("dyn2", "dynamic-2", false)
	o.AddSegment("cache2", "cache-2", true)

	optimized := o.Optimize()
	if len(optimized) != 4 {
		t.Errorf("Optimize len = %d, want 4", len(optimized))
	}
	// cacheable 段应在前，且保持原序
	if !optimized[0].Cacheable {
		t.Errorf("optimized[0].Cacheable = false, want true")
	}
	if !optimized[1].Cacheable {
		t.Errorf("optimized[1].Cacheable = false, want true")
	}
	if optimized[0].ID != "cache1" {
		t.Errorf("optimized[0].ID = %q, want %q", optimized[0].ID, "cache1")
	}
	if optimized[1].ID != "cache2" {
		t.Errorf("optimized[1].ID = %q, want %q", optimized[1].ID, "cache2")
	}
	// 非缓存段在后，且保持原序
	if optimized[2].ID != "dyn1" {
		t.Errorf("optimized[2].ID = %q, want %q", optimized[2].ID, "dyn1")
	}
	if optimized[3].ID != "dyn2" {
		t.Errorf("optimized[3].ID = %q, want %q", optimized[3].ID, "dyn2")
	}
	// Order 应被重新分配为 0..n-1
	if optimized[0].Order != 0 {
		t.Errorf("optimized[0].Order = %d, want 0", optimized[0].Order)
	}
	if optimized[3].Order != 3 {
		t.Errorf("optimized[3].Order = %d, want 3", optimized[3].Order)
	}
}

// TestPromptAssemblyOptimizer_Assemble 验证组装结果（换行分隔，cacheable 在前）。
func TestPromptAssemblyOptimizer_Assemble(t *testing.T) {
	o := NewPromptAssemblyOptimizer()
	o.AddSegment("a", "AAA", true)
	o.AddSegment("b", "BBB", false)
	o.Optimize()

	result := o.Assemble()
	expected := "AAA\nBBB"
	if result != expected {
		t.Errorf("Assemble() = %q, want %q", result, expected)
	}
}

// TestPromptAssemblyOptimizer_OptimizeIncrementsCacheHit 验证发生重排时 cacheHitOptimized 递增。
func TestPromptAssemblyOptimizer_OptimizeIncrementsCacheHit(t *testing.T) {
	o := NewPromptAssemblyOptimizer()
	o.AddSegment("dyn", "dynamic", false) // Order 0
	o.AddSegment("cache", "cacheable", true) // Order 1
	o.Optimize() // 重排：cache 前置 -> reordered

	stats := o.GetStats()
	cacheHitOptimized := stats["cacheHitOptimized"].(int)
	if cacheHitOptimized != 1 {
		t.Errorf("cacheHitOptimized = %d, want 1 (reorder happened)", cacheHitOptimized)
	}
}

// TestPromptAssemblyOptimizer_Stats 验证 Stats 中的 segmentCount 等字段。
func TestPromptAssemblyOptimizer_Stats(t *testing.T) {
	o := NewPromptAssemblyOptimizer()
	o.AddSegment("a", "AAA", true)
	o.AddSegment("b", "BBB", false)
	o.AddSegment("c", "CCC", true)

	stats := o.GetStats()
	segmentCount := stats["segmentCount"].(int)
	if segmentCount != 3 {
		t.Errorf("segmentCount = %d, want 3", segmentCount)
	}
	assemblyCount := stats["assemblyCount"].(int)
	if assemblyCount != 0 {
		t.Errorf("assemblyCount = %d, want 0 (no Assemble yet)", assemblyCount)
	}
}

// TestPromptAssemblyOptimizer_Reset 验证 Reset 后状态清空。
func TestPromptAssemblyOptimizer_Reset(t *testing.T) {
	o := NewPromptAssemblyOptimizer()
	o.AddSegment("a", "AAA", true)
	o.AddSegment("b", "BBB", false)
	o.Optimize()
	o.Assemble()
	o.Reset()

	stats := o.GetStats()
	segmentCount := stats["segmentCount"].(int)
	if segmentCount != 0 {
		t.Errorf("after Reset segmentCount = %d, want 0", segmentCount)
	}
	assemblyCount := stats["assemblyCount"].(int)
	if assemblyCount != 0 {
		t.Errorf("after Reset assemblyCount = %d, want 0", assemblyCount)
	}
	cacheHitOptimized := stats["cacheHitOptimized"].(int)
	if cacheHitOptimized != 0 {
		t.Errorf("after Reset cacheHitOptimized = %d, want 0", cacheHitOptimized)
	}
	// Reset 后 Assemble 应返回空字符串
	if got := o.Assemble(); got != "" {
		t.Errorf("after Reset Assemble() = %q, want empty string", got)
	}
}
