package agent

import "testing"

// =============================================================================
// OPT-221: TokenAwareWeightedRoundRobin (Token感知加权轮询器)
// =============================================================================

// TestTAWRR_AddHandlerAndNext 验证 AddHandler + Next 的加权轮询顺序。
func TestTAWRR_AddHandlerAndNext(t *testing.T) {
	w := NewTokenAwareWeightedRoundRobin()
	w.AddHandler("A", 1)
	w.AddHandler("B", 1)

	// 权重各为 1，序列为 [A, B]，轮询顺序应为 A, B, A, B
	expected := []string{"A", "B", "A", "B"}
	for i, want := range expected {
		got, ok := w.Next()
		if !ok {
			t.Errorf("Next() #%d: ok=false, want true", i)
		}
		if got != want {
			t.Errorf("Next() #%d: got %q, want %q", i, got, want)
		}
	}
}

// TestTAWRR_HighWeightSelectedMore 验证高权重处理器被选中更多次。
func TestTAWRR_HighWeightSelectedMore(t *testing.T) {
	w := NewTokenAwareWeightedRoundRobin()
	w.AddHandler("A", 1)
	w.AddHandler("B", 3)

	// 序列为 [A, B, B, B]，一轮中 B 出现 3 次，A 出现 1 次
	counts := map[string]int{}
	for i := 0; i < 4; i++ {
		name, ok := w.Next()
		if !ok {
			t.Fatalf("Next() #%d: ok=false, want true", i)
		}
		counts[name]++
	}

	if counts["A"] != 1 {
		t.Errorf("A selected %d times, want 1", counts["A"])
	}
	if counts["B"] != 3 {
		t.Errorf("B selected %d times, want 3", counts["B"])
	}
}

// TestTAWRR_GetHandlers 验证 GetHandlers 返回已注册处理器列表。
func TestTAWRR_GetHandlers(t *testing.T) {
	w := NewTokenAwareWeightedRoundRobin()
	w.AddHandler("alpha", 2)
	w.AddHandler("beta", 3)

	handlers := w.GetHandlers()
	if len(handlers) != 2 {
		t.Errorf("GetHandlers() returned %d handlers, want 2", len(handlers))
	}

	// 按名称排序，alpha 在前
	if handlers[0].Name != "alpha" || handlers[0].Weight != 2 {
		t.Errorf("handlers[0] = {Name:%q, Weight:%d}, want {alpha, 2}", handlers[0].Name, handlers[0].Weight)
	}
	if handlers[1].Name != "beta" || handlers[1].Weight != 3 {
		t.Errorf("handlers[1] = {Name:%q, Weight:%d}, want {beta, 3}", handlers[1].Name, handlers[1].Weight)
	}
}

// TestTAWRR_Stats 验证 GetStats 中的 handlerCount 等统计字段。
func TestTAWRR_Stats(t *testing.T) {
	w := NewTokenAwareWeightedRoundRobin()
	w.AddHandler("A", 2)
	w.AddHandler("B", 3)
	w.Next()
	w.Next()

	stats := w.GetStats()
	if hc, ok := stats["handlerCount"].(int); !ok || hc != 2 {
		t.Errorf("stats[handlerCount] = %v, want 2", stats["handlerCount"])
	}
	if tw, ok := stats["totalWeight"].(int); !ok || tw != 5 {
		t.Errorf("stats[totalWeight] = %v, want 5", stats["totalWeight"])
	}
	if dc, ok := stats["dispatchedCount"].(int); !ok || dc != 2 {
		t.Errorf("stats[dispatchedCount] = %v, want 2", stats["dispatchedCount"])
	}
}

// TestTAWRR_Reset 验证 Reset 清除所有处理器与统计。
func TestTAWRR_Reset(t *testing.T) {
	w := NewTokenAwareWeightedRoundRobin()
	w.AddHandler("A", 2)
	w.Next()

	w.Reset()

	stats := w.GetStats()
	if hc, ok := stats["handlerCount"].(int); !ok || hc != 0 {
		t.Errorf("after Reset: handlerCount = %v, want 0", stats["handlerCount"])
	}
	if tw, ok := stats["totalWeight"].(int); !ok || tw != 0 {
		t.Errorf("after Reset: totalWeight = %v, want 0", stats["totalWeight"])
	}
	if dc, ok := stats["dispatchedCount"].(int); !ok || dc != 0 {
		t.Errorf("after Reset: dispatchedCount = %v, want 0", stats["dispatchedCount"])
	}

	// Reset 后 Next 应返回 false
	if _, ok := w.Next(); ok {
		t.Errorf("Next() after Reset: ok=true, want false")
	}
}

// =============================================================================
// OPT-222: CacheInvalidationPropagator (缓存失效传播器)
// =============================================================================

// TestCIP_AddLayerAndPropagate 验证 AddLayer + Propagate 传播到所有层。
func TestCIP_AddLayerAndPropagate(t *testing.T) {
	c := NewCacheInvalidationPropagator()
	c.AddLayer("L1")
	c.AddLayer("L2")
	c.AddLayer("L3")

	// 重复添加应被忽略
	c.AddLayer("L1")

	success := c.Propagate("key1")
	if success != 3 {
		t.Errorf("Propagate() = %d, want 3", success)
	}

	// 每层失效计数应为 1
	for _, layer := range []string{"L1", "L2", "L3"} {
		if got := c.GetLayerStats(layer); got != 1 {
			t.Errorf("GetLayerStats(%q) = %d, want 1", layer, got)
		}
	}
}

// TestCIP_GetLayerStats 验证 GetLayerStats 返回指定层的失效计数。
func TestCIP_GetLayerStats(t *testing.T) {
	c := NewCacheInvalidationPropagator()
	c.AddLayer("cache")
	c.AddLayer("cdn")

	c.Propagate("k1")
	c.Propagate("k2")

	if got := c.GetLayerStats("cache"); got != 2 {
		t.Errorf("GetLayerStats(\"cache\") = %d, want 2", got)
	}
	if got := c.GetLayerStats("cdn"); got != 2 {
		t.Errorf("GetLayerStats(\"cdn\") = %d, want 2", got)
	}
	// 未注册层应返回 0
	if got := c.GetLayerStats("unknown"); got != 0 {
		t.Errorf("GetLayerStats(\"unknown\") = %d, want 0", got)
	}
}

// TestCIP_Stats 验证 GetStats 中的 totalPropagated 等统计字段。
func TestCIP_Stats(t *testing.T) {
	c := NewCacheInvalidationPropagator()
	c.AddLayer("L1")
	c.AddLayer("L2")
	c.Propagate("key1")

	stats := c.GetStats()
	if lc, ok := stats["layerCount"].(int); !ok || lc != 2 {
		t.Errorf("stats[layerCount] = %v, want 2", stats["layerCount"])
	}
	if tp, ok := stats["totalPropagated"].(int); !ok || tp != 2 {
		t.Errorf("stats[totalPropagated] = %v, want 2", stats["totalPropagated"])
	}
	if pf, ok := stats["propagationFailures"].(int); !ok || pf != 0 {
		t.Errorf("stats[propagationFailures] = %v, want 0", stats["propagationFailures"])
	}
}

// TestCIP_EmptyKey 验证空 key 导致所有层计为失败。
func TestCIP_EmptyKey(t *testing.T) {
	c := NewCacheInvalidationPropagator()
	c.AddLayer("L1")
	c.AddLayer("L2")

	success := c.Propagate("")
	if success != 0 {
		t.Errorf("Propagate(\"\") = %d, want 0", success)
	}

	stats := c.GetStats()
	if tp, ok := stats["totalPropagated"].(int); !ok || tp != 0 {
		t.Errorf("stats[totalPropagated] = %v, want 0", stats["totalPropagated"])
	}
	if pf, ok := stats["propagationFailures"].(int); !ok || pf != 2 {
		t.Errorf("stats[propagationFailures] = %v, want 2", stats["propagationFailures"])
	}
}

// TestCIP_Reset 验证 Reset 清除所有层与统计。
func TestCIP_Reset(t *testing.T) {
	c := NewCacheInvalidationPropagator()
	c.AddLayer("L1")
	c.Propagate("k1")

	c.Reset()

	stats := c.GetStats()
	if lc, ok := stats["layerCount"].(int); !ok || lc != 0 {
		t.Errorf("after Reset: layerCount = %v, want 0", stats["layerCount"])
	}
	if tp, ok := stats["totalPropagated"].(int); !ok || tp != 0 {
		t.Errorf("after Reset: totalPropagated = %v, want 0", stats["totalPropagated"])
	}
	if got := c.GetLayerStats("L1"); got != 0 {
		t.Errorf("after Reset: GetLayerStats(\"L1\") = %d, want 0", got)
	}
}

// =============================================================================
// OPT-223: ContextRelevanceScorerV2 (上下文相关性评分器 V2)
// =============================================================================

// TestCRSV2_Score 验证 Score 返回基于词重叠率的相关性分数。
func TestCRSV2_Score(t *testing.T) {
	s := NewContextRelevanceScorerV2(0.3)

	// "hello world" 与 "hello world" 完全重叠 → 分数 1.0
	score := s.Score("hello world", "hello world")
	if score != 1.0 {
		t.Errorf("Score(\"hello world\", \"hello world\") = %v, want 1.0", score)
	}

	// "hello world" 与 "hello" 交集1，并集2 → 分数 0.5
	score = s.Score("hello world", "hello")
	if score != 0.5 {
		t.Errorf("Score(\"hello world\", \"hello\") = %v, want 0.5", score)
	}

	// 无重叠 → 分数 0
	score = s.Score("foo bar", "baz")
	if score != 0 {
		t.Errorf("Score(\"foo bar\", \"baz\") = %v, want 0", score)
	}
}

// TestCRSV2_IsRelevant 验证 IsRelevant 在分数超过阈值时返回 true。
func TestCRSV2_IsRelevant(t *testing.T) {
	// 阈值 0.3：分数 0.5 > 0.3 → 相关
	s := NewContextRelevanceScorerV2(0.3)
	if !s.IsRelevant("hello world", "hello") {
		t.Errorf("IsRelevant(\"hello world\", \"hello\") with threshold 0.3 = false, want true")
	}

	// 无重叠，分数 0，不大于 0.3 → 不相关
	if s.IsRelevant("foo bar", "baz") {
		t.Errorf("IsRelevant(\"foo bar\", \"baz\") with threshold 0.3 = true, want false")
	}
}

// TestCRSV2_ThresholdBoundary 验证阈值边界：分数等于阈值时不算相关（严格大于）。
func TestCRSV2_ThresholdBoundary(t *testing.T) {
	// 阈值 0.5：分数恰好 0.5，不严格大于 → 不相关
	s := NewContextRelevanceScorerV2(0.5)
	if s.IsRelevant("hello world", "hello") {
		t.Errorf("IsRelevant with threshold 0.5 and score 0.5 = true, want false (strictly greater)")
	}

	// 阈值 0.49：分数 0.5 > 0.49 → 相关
	s2 := NewContextRelevanceScorerV2(0.49)
	if !s2.IsRelevant("hello world", "hello") {
		t.Errorf("IsRelevant with threshold 0.49 and score 0.5 = false, want true")
	}
}

// TestCRSV2_GetScore 验证 GetScore 返回已记录的片段分数。
func TestCRSV2_GetScore(t *testing.T) {
	s := NewContextRelevanceScorerV2(0.3)
	s.Score("hello world", "hello")

	got := s.GetScore("hello world")
	if got != 0.5 {
		t.Errorf("GetScore(\"hello world\") = %v, want 0.5", got)
	}

	// 未评分的片段应返回 0
	got = s.GetScore("not scored")
	if got != 0 {
		t.Errorf("GetScore(\"not scored\") = %v, want 0", got)
	}
}

// TestCRSV2_Stats 验证 GetStats 中的 totalScored 等统计字段。
func TestCRSV2_Stats(t *testing.T) {
	s := NewContextRelevanceScorerV2(0.3)
	s.Score("hello world", "hello")
	s.Score("foo bar", "baz")

	stats := s.GetStats()
	if fc, ok := stats["fragmentCount"].(int); !ok || fc != 2 {
		t.Errorf("stats[fragmentCount] = %v, want 2", stats["fragmentCount"])
	}
	if ts, ok := stats["totalScored"].(int); !ok || ts != 2 {
		t.Errorf("stats[totalScored] = %v, want 2", stats["totalScored"])
	}
	if th, ok := stats["threshold"].(float64); !ok || th != 0.3 {
		t.Errorf("stats[threshold] = %v, want 0.3", stats["threshold"])
	}
}

// TestCRSV2_Reset 验证 Reset 清除评分缓存与统计（保留阈值）。
func TestCRSV2_Reset(t *testing.T) {
	s := NewContextRelevanceScorerV2(0.3)
	s.Score("hello world", "hello")

	s.Reset()

	stats := s.GetStats()
	if fc, ok := stats["fragmentCount"].(int); !ok || fc != 0 {
		t.Errorf("after Reset: fragmentCount = %v, want 0", stats["fragmentCount"])
	}
	if ts, ok := stats["totalScored"].(int); !ok || ts != 0 {
		t.Errorf("after Reset: totalScored = %v, want 0", stats["totalScored"])
	}
	// 阈值应保留
	if th, ok := stats["threshold"].(float64); !ok || th != 0.3 {
		t.Errorf("after Reset: threshold = %v, want 0.3", stats["threshold"])
	}
	// 已清除的分数应返回 0
	if got := s.GetScore("hello world"); got != 0 {
		t.Errorf("after Reset: GetScore(\"hello world\") = %v, want 0", got)
	}
}

// =============================================================================
// OPT-224: TokenAwareFairnessScheduler (Token感知公平调度器)
// =============================================================================

// TestTAFS_EnqueueAndDequeue 验证 Enqueue + Dequeue 的基本公平出队。
func TestTAFS_EnqueueAndDequeue(t *testing.T) {
	s := NewTokenAwareFairnessScheduler(10)
	s.Enqueue("tenantA", 5)

	tenant, served, ok := s.Dequeue()
	if !ok {
		t.Errorf("Dequeue() ok=false, want true")
	}
	if tenant != "tenantA" {
		t.Errorf("Dequeue() tenant = %q, want \"tenantA\"", tenant)
	}
	if served != 5 {
		t.Errorf("Dequeue() served = %d, want 5", served)
	}

	// 队列耗尽后应返回 false
	_, _, ok = s.Dequeue()
	if ok {
		t.Errorf("Dequeue() after drain: ok=true, want false")
	}
}

// TestTAFS_MultiTenantRoundRobin 验证多个租户轮流出队。
func TestTAFS_MultiTenantRoundRobin(t *testing.T) {
	s := NewTokenAwareFairnessScheduler(10)
	// tenantA 有两个请求，tenantB 有一个请求
	s.Enqueue("tenantA", 5)
	s.Enqueue("tenantA", 5)
	s.Enqueue("tenantB", 5)

	// 租户按名称排序: [tenantA, tenantB]
	// Dequeue 1: start=0%2=0 → tenantA
	// Dequeue 2: start=1%2=1 → tenantB
	// Dequeue 3: 只剩 tenantA → tenantA
	expected := []string{"tenantA", "tenantB", "tenantA"}
	for i, want := range expected {
		tenant, _, ok := s.Dequeue()
		if !ok {
			t.Fatalf("Dequeue() #%d: ok=false, want true", i)
		}
		if tenant != want {
			t.Errorf("Dequeue() #%d: tenant = %q, want %q", i, tenant, want)
		}
	}
}

// TestTAFS_MaxPerTurn 验证超过 maxPerTurn 时仅服务限额并回填余量。
func TestTAFS_MaxPerTurn(t *testing.T) {
	s := NewTokenAwareFairnessScheduler(3)
	s.Enqueue("tenantA", 10)

	// 10 tokens, maxPerTurn=3 → 3+3+3+1
	expected := []int{3, 3, 3, 1}
	for i, want := range expected {
		_, served, ok := s.Dequeue()
		if !ok {
			t.Fatalf("Dequeue() #%d: ok=false, want true", i)
		}
		if served != want {
			t.Errorf("Dequeue() #%d: served = %d, want %d", i, served, want)
		}
	}

	// 队列应已耗尽
	_, _, ok := s.Dequeue()
	if ok {
		t.Errorf("Dequeue() after drain: ok=true, want false")
	}
}

// TestTAFS_GetQueueDepth 验证 GetQueueDepth 返回指定租户的队列深度。
func TestTAFS_GetQueueDepth(t *testing.T) {
	s := NewTokenAwareFairnessScheduler(10)
	s.Enqueue("tenantA", 5)
	s.Enqueue("tenantA", 3)
	s.Enqueue("tenantB", 7)

	if got := s.GetQueueDepth("tenantA"); got != 2 {
		t.Errorf("GetQueueDepth(\"tenantA\") = %d, want 2", got)
	}
	if got := s.GetQueueDepth("tenantB"); got != 1 {
		t.Errorf("GetQueueDepth(\"tenantB\") = %d, want 1", got)
	}
	// 未入队租户应返回 0
	if got := s.GetQueueDepth("unknown"); got != 0 {
		t.Errorf("GetQueueDepth(\"unknown\") = %d, want 0", got)
	}
}

// TestTAFS_Stats 验证 GetStats 中的 servedCount 等统计字段。
func TestTAFS_Stats(t *testing.T) {
	s := NewTokenAwareFairnessScheduler(10)
	s.Enqueue("tenantA", 5)
	s.Enqueue("tenantB", 3)
	s.Dequeue()
	s.Dequeue()

	stats := s.GetStats()
	if tc, ok := stats["tenantCount"].(int); !ok || tc != 0 {
		t.Errorf("stats[tenantCount] = %v, want 0 (both drained)", stats["tenantCount"])
	}
	if sc, ok := stats["servedCount"].(int); !ok || sc != 2 {
		t.Errorf("stats[servedCount] = %v, want 2", stats["servedCount"])
	}
	if ts, ok := stats["totalServed"].(int); !ok || ts != 8 {
		t.Errorf("stats[totalServed] = %v, want 8", stats["totalServed"])
	}
	if mp, ok := stats["maxPerTurn"].(int); !ok || mp != 10 {
		t.Errorf("stats[maxPerTurn] = %v, want 10", stats["maxPerTurn"])
	}
}

// TestTAFS_Reset 验证 Reset 清除所有队列与统计（保留 maxPerTurn）。
func TestTAFS_Reset(t *testing.T) {
	s := NewTokenAwareFairnessScheduler(10)
	s.Enqueue("tenantA", 5)
	s.Dequeue()

	s.Reset()

	stats := s.GetStats()
	if tc, ok := stats["tenantCount"].(int); !ok || tc != 0 {
		t.Errorf("after Reset: tenantCount = %v, want 0", stats["tenantCount"])
	}
	if sc, ok := stats["servedCount"].(int); !ok || sc != 0 {
		t.Errorf("after Reset: servedCount = %v, want 0", stats["servedCount"])
	}
	// maxPerTurn 应保留
	if mp, ok := stats["maxPerTurn"].(int); !ok || mp != 10 {
		t.Errorf("after Reset: maxPerTurn = %v, want 10", stats["maxPerTurn"])
	}
	// 队列深度应为 0
	if got := s.GetQueueDepth("tenantA"); got != 0 {
		t.Errorf("after Reset: GetQueueDepth(\"tenantA\") = %d, want 0", got)
	}
}

// =============================================================================
// OPT-225: PromptCacheKeyOptimizer (提示缓存键优化器)
// =============================================================================

// TestPCKO_Optimize 验证 Optimize 返回优化后的键（固定长度哈希摘要）。
func TestPCKO_Optimize(t *testing.T) {
	o := NewPromptCacheKeyOptimizer()
	optimized := o.Optimize("some cache key")

	if optimized == "" {
		t.Errorf("Optimize() returned empty string")
	}
	if len(optimized) != 16 {
		t.Errorf("len(Optimize()) = %d, want 16 (FNV-1a 64-bit hex)", len(optimized))
	}
	if optimized == "some cache key" {
		t.Errorf("Optimize() returned the original key unchanged")
	}
}

// TestPCKO_SameKeySameOptimized 验证相同原始键返回相同优化键。
func TestPCKO_SameKeySameOptimized(t *testing.T) {
	o := NewPromptCacheKeyOptimizer()
	first := o.Optimize("my key")
	second := o.Optimize("my key")

	if first != second {
		t.Errorf("Optimize(\"my key\") twice: %q != %q, want equal", first, second)
	}
}

// TestPCKO_GetOriginalKey 验证 GetOriginalKey 反向查找优化键对应的原始键。
func TestPCKO_GetOriginalKey(t *testing.T) {
	o := NewPromptCacheKeyOptimizer()
	original := "the original key"
	optimized := o.Optimize(original)

	found, ok := o.GetOriginalKey(optimized)
	if !ok {
		t.Errorf("GetOriginalKey(%q) ok=false, want true", optimized)
	}
	if found != original {
		t.Errorf("GetOriginalKey(%q) = %q, want %q", optimized, found, original)
	}

	// 不存在的优化键应返回 false
	_, ok = o.GetOriginalKey("nonexistent0000000")
	if ok {
		t.Errorf("GetOriginalKey(\"nonexistent\") ok=true, want false")
	}
}

// TestPCKO_GetKeyReduction 验证 GetKeyReduction 返回键长度缩减率。
func TestPCKO_GetKeyReduction(t *testing.T) {
	o := NewPromptCacheKeyOptimizer()
	// 使用长度为 20 的原始键，优化后为 16 字符
	// 缩减率 = 1 - 16/20 = 0.2
	longKey := "abcdefghijklmnopqrst" // 20 chars
	o.Optimize(longKey)

	reduction := o.GetKeyReduction()
	if reduction <= 0 {
		t.Errorf("GetKeyReduction() = %v, want > 0 (key should be shortened)", reduction)
	}
	// 20 字符 → 16 字符，缩减率 0.2
	expected := 1.0 - float64(16)/float64(len(longKey))
	if reduction != expected {
		t.Errorf("GetKeyReduction() = %v, want %v", reduction, expected)
	}
}

// TestPCKO_Stats 验证 GetStats 中的 optimizedCount 等统计字段。
func TestPCKO_Stats(t *testing.T) {
	o := NewPromptCacheKeyOptimizer()
	o.Optimize("key one")
	o.Optimize("key two")
	o.Optimize("key one") // 命中缓存，不增加计数

	stats := o.GetStats()
	if oc, ok := stats["optimizedCount"].(int); !ok || oc != 2 {
		t.Errorf("stats[optimizedCount] = %v, want 2", stats["optimizedCount"])
	}
	if cc, ok := stats["collisionCount"].(int); !ok || cc != 0 {
		t.Errorf("stats[collisionCount] = %v, want 0", stats["collisionCount"])
	}
	if tl, ok := stats["totalKeyLength"].(int); !ok || tl != 14 {
		t.Errorf("stats[totalKeyLength] = %v, want 14 (7+7)", stats["totalKeyLength"])
	}
}

// TestPCKO_Reset 验证 Reset 清除所有映射与统计。
func TestPCKO_Reset(t *testing.T) {
	o := NewPromptCacheKeyOptimizer()
	o.Optimize("some key")

	o.Reset()

	stats := o.GetStats()
	if oc, ok := stats["optimizedCount"].(int); !ok || oc != 0 {
		t.Errorf("after Reset: optimizedCount = %v, want 0", stats["optimizedCount"])
	}
	if tl, ok := stats["totalKeyLength"].(int); !ok || tl != 0 {
		t.Errorf("after Reset: totalKeyLength = %v, want 0", stats["totalKeyLength"])
	}
	// 缩减率应为 0（无数据）
	if r := o.GetKeyReduction(); r != 0 {
		t.Errorf("after Reset: GetKeyReduction() = %v, want 0", r)
	}
}
