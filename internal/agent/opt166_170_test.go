package agent

import "testing"

// ── OPT-166: TokenAwareAggregator ──

func TestTokenAwareAggregator_AddShouldFlush(t *testing.T) {
	agg := NewTokenAwareAggregator(3, 100000)
	// 前两项不应触发刷新
	if agg.Add(AggregationItem{ID: "1", Content: "hello", EstimatedTokens: 10}) {
		t.Errorf("batch 未满时 Add 应返回 false")
	}
	if agg.Add(AggregationItem{ID: "2", Content: "world", EstimatedTokens: 10}) {
		t.Errorf("batch 未满时 Add 应返回 false")
	}
	// 第三项达到 maxBatchSize，应触发刷新
	if !agg.Add(AggregationItem{ID: "3", Content: "test", EstimatedTokens: 10}) {
		t.Errorf("达到 maxBatchSize 时 Add 应返回 true")
	}
	// ShouldFlush 也应返回 true
	if !agg.ShouldFlush() {
		t.Errorf("达到 maxBatchSize 时 ShouldFlush 应返回 true")
	}
}

func TestTokenAwareAggregator_Flush(t *testing.T) {
	agg := NewTokenAwareAggregator(10, 100000)
	agg.Add(AggregationItem{ID: "1", Content: "aaa", EstimatedTokens: 10})
	agg.Add(AggregationItem{ID: "2", Content: "bbb", EstimatedTokens: 10})
	agg.Add(AggregationItem{ID: "3", Content: "ccc", EstimatedTokens: 10})

	batch := agg.Flush()
	if len(batch) != 3 {
		t.Errorf("Flush 应返回 3 项, 得到 %d", len(batch))
	}
	if batch[0].ID != "1" || batch[1].ID != "2" || batch[2].ID != "3" {
		t.Errorf("Flush 返回的顺序不正确")
	}
	// Flush 后 pending 应清空
	if len(agg.Peek()) != 0 {
		t.Errorf("Flush 后 pending 应为空")
	}
}

func TestTokenAwareAggregator_Peek(t *testing.T) {
	agg := NewTokenAwareAggregator(10, 100000)
	agg.Add(AggregationItem{ID: "1", Content: "aaa", EstimatedTokens: 10})
	agg.Add(AggregationItem{ID: "2", Content: "bbb", EstimatedTokens: 10})

	peeked := agg.Peek()
	if len(peeked) != 2 {
		t.Errorf("Peek 应返回 2 项, 得到 %d", len(peeked))
	}
	// Peek 不应清空 pending
	if len(agg.Peek()) != 2 {
		t.Errorf("Peek 不应清空 pending 列表")
	}
}

func TestTokenAwareAggregator_TotalAggregated(t *testing.T) {
	agg := NewTokenAwareAggregator(2, 100000)
	agg.Add(AggregationItem{ID: "1", Content: "aaa", EstimatedTokens: 10})
	agg.Add(AggregationItem{ID: "2", Content: "bbb", EstimatedTokens: 10})
	agg.Flush() // 第一批 2 项
	agg.Add(AggregationItem{ID: "3", Content: "ccc", EstimatedTokens: 10})
	agg.Add(AggregationItem{ID: "4", Content: "ddd", EstimatedTokens: 10})
	agg.Flush() // 第二批 2 项

	stats := agg.GetStats()
	if stats["totalAggregated"].(int) != 4 {
		t.Errorf("totalAggregated 应为 4, 得到 %v", stats["totalAggregated"])
	}
	if stats["flushedBatches"].(int) != 2 {
		t.Errorf("flushedBatches 应为 2, 得到 %v", stats["flushedBatches"])
	}
}

func TestTokenAwareAggregator_Stats(t *testing.T) {
	agg := NewTokenAwareAggregator(5, 1000)
	agg.Add(AggregationItem{ID: "1", Content: "aaa", EstimatedTokens: 10})
	agg.Add(AggregationItem{ID: "2", Content: "bbb", EstimatedTokens: 20})

	stats := agg.GetStats()
	if stats["maxBatchSize"].(int) != 5 {
		t.Errorf("maxBatchSize 应为 5, 得到 %v", stats["maxBatchSize"])
	}
	if stats["maxBatchTokens"].(int) != 1000 {
		t.Errorf("maxBatchTokens 应为 1000, 得到 %v", stats["maxBatchTokens"])
	}
	if stats["pendingCount"].(int) != 2 {
		t.Errorf("pendingCount 应为 2, 得到 %v", stats["pendingCount"])
	}
	if stats["flushedBatches"].(int) != 0 {
		t.Errorf("flushedBatches 应为 0, 得到 %v", stats["flushedBatches"])
	}
	if stats["totalAggregated"].(int) != 2 {
		t.Errorf("totalAggregated 应为 2, 得到 %v", stats["totalAggregated"])
	}
}

func TestTokenAwareAggregator_Reset(t *testing.T) {
	agg := NewTokenAwareAggregator(5, 1000)
	agg.Add(AggregationItem{ID: "1", Content: "aaa", EstimatedTokens: 10})
	agg.Add(AggregationItem{ID: "2", Content: "bbb", EstimatedTokens: 20})
	agg.Flush()

	agg.Reset()
	stats := agg.GetStats()
	if stats["pendingCount"].(int) != 0 {
		t.Errorf("Reset 后 pendingCount 应为 0, 得到 %v", stats["pendingCount"])
	}
	if stats["flushedBatches"].(int) != 0 {
		t.Errorf("Reset 后 flushedBatches 应为 0, 得到 %v", stats["flushedBatches"])
	}
	if stats["totalAggregated"].(int) != 0 {
		t.Errorf("Reset 后 totalAggregated 应为 0, 得到 %v", stats["totalAggregated"])
	}
}

// ── OPT-167: CacheInvalidationTracker ──
// 注意: TestCacheInvalidationTracker_Record 和 TestCacheInvalidationTracker_GetTopReasons
// 已在 opt86_90_test.go 中定义，此处使用不同名称避免冲突。

func TestCacheInvalidationTracker_RecordCount(t *testing.T) {
	tracker := NewCacheInvalidationTracker()
	tracker.Record("key1", "prefix_changed")
	tracker.Record("key2", "prefix_changed")
	tracker.Record("key3", "ttl_expired")

	if tracker.GetInvalidationCount("prefix_changed") != 2 {
		t.Errorf("prefix_changed 失效次数应为 2, 得到 %d", tracker.GetInvalidationCount("prefix_changed"))
	}
	if tracker.GetInvalidationCount("ttl_expired") != 1 {
		t.Errorf("ttl_expired 失效次数应为 1, 得到 %d", tracker.GetInvalidationCount("ttl_expired"))
	}
	if tracker.GetInvalidationCount("nonexistent") != 0 {
		t.Errorf("不存在的原因失效次数应为 0, 得到 %d", tracker.GetInvalidationCount("nonexistent"))
	}
}

func TestCacheInvalidationTracker_TopReasonsOrder(t *testing.T) {
	tracker := NewCacheInvalidationTracker()
	tracker.Record("k1", "reason_a")
	tracker.Record("k2", "reason_b")
	tracker.Record("k3", "reason_b")
	tracker.Record("k4", "reason_b")
	tracker.Record("k5", "reason_c")
	tracker.Record("k6", "reason_c")

	top := tracker.GetTopReasons(2)
	if len(top) != 2 {
		t.Errorf("GetTopReasons(2) 应返回 2 个原因, 得到 %d", len(top))
	}
	if top[0] != "reason_b" {
		t.Errorf("最高频原因应为 reason_b (3次), 得到 %s", top[0])
	}
	if top[1] != "reason_c" {
		t.Errorf("第二高频原因应为 reason_c (2次), 得到 %s", top[1])
	}
}

func TestCacheInvalidationTracker_Stats(t *testing.T) {
	tracker := NewCacheInvalidationTracker()
	tracker.Record("key1", "reason_a")
	tracker.Record("key2", "reason_b")
	tracker.Record("key3", "reason_a")

	stats := tracker.GetStats()
	if stats["totalInvalidations"].(int) != 3 {
		t.Errorf("totalInvalidations 应为 3, 得到 %v", stats["totalInvalidations"])
	}
	if stats["reasonCount"].(int) != 2 {
		t.Errorf("reasonCount 应为 2, 得到 %v", stats["reasonCount"])
	}
	if stats["lastInvalidatedKey"].(string) != "key3" {
		t.Errorf("lastInvalidatedKey 应为 key3, 得到 %v", stats["lastInvalidatedKey"])
	}
	if stats["lastReason"].(string) != "reason_a" {
		t.Errorf("lastReason 应为 reason_a, 得到 %v", stats["lastReason"])
	}
}

func TestCacheInvalidationTracker_MultipleReasons(t *testing.T) {
	tracker := NewCacheInvalidationTracker()
	tracker.Record("k1", "reason_a")
	tracker.Record("k2", "reason_b")
	tracker.Record("k3", "reason_c")
	tracker.Record("k4", "reason_a")
	tracker.Record("k5", "reason_a")

	if tracker.GetInvalidationCount("reason_a") != 3 {
		t.Errorf("reason_a 失效次数应为 3, 得到 %d", tracker.GetInvalidationCount("reason_a"))
	}
	if tracker.GetInvalidationCount("reason_b") != 1 {
		t.Errorf("reason_b 失效次数应为 1, 得到 %d", tracker.GetInvalidationCount("reason_b"))
	}
	if tracker.GetInvalidationCount("reason_c") != 1 {
		t.Errorf("reason_c 失效次数应为 1, 得到 %d", tracker.GetInvalidationCount("reason_c"))
	}
	// n 超过可用原因数时应返回全部
	top := tracker.GetTopReasons(10)
	if len(top) != 3 {
		t.Errorf("GetTopReasons(10) 应返回 3 个原因, 得到 %d", len(top))
	}
	if top[0] != "reason_a" {
		t.Errorf("最高频原因应为 reason_a, 得到 %s", top[0])
	}
}

func TestCacheInvalidationTracker_Reset(t *testing.T) {
	tracker := NewCacheInvalidationTracker()
	tracker.Record("key1", "reason_a")
	tracker.Record("key2", "reason_b")

	tracker.Reset()
	stats := tracker.GetStats()
	if stats["totalInvalidations"].(int) != 0 {
		t.Errorf("Reset 后 totalInvalidations 应为 0, 得到 %v", stats["totalInvalidations"])
	}
	if stats["reasonCount"].(int) != 0 {
		t.Errorf("Reset 后 reasonCount 应为 0, 得到 %v", stats["reasonCount"])
	}
	if tracker.GetInvalidationCount("reason_a") != 0 {
		t.Errorf("Reset 后 reason_a 失效次数应为 0")
	}
}

// ── OPT-168: ContextMergeOptimizer ──

func TestContextMergeOptimizer_AddFragmentAndMerge(t *testing.T) {
	opt := NewContextMergeOptimizer(100)
	opt.AddFragment("f1", "hello", 5)
	opt.AddFragment("f2", "world", 5)

	merged := opt.Merge()
	if merged != "helloworld" {
		t.Errorf("合并结果应为 'helloworld', 得到 '%s'", merged)
	}
	if opt.GetMergeCount() != 1 {
		t.Errorf("mergeCount 应为 1, 得到 %d", opt.GetMergeCount())
	}
}

func TestContextMergeOptimizer_OverlapDetection(t *testing.T) {
	opt := NewContextMergeOptimizer(100)
	// 第一个片段后缀与第二个片段前缀有 10 字符重叠
	opt.AddFragment("f1", "abcdefghij", 10)
	opt.AddFragment("f2", "abcdefghijXYZ", 13)

	merged := opt.Merge()
	// 重叠部分应被移除: "abcdefghij" + "XYZ"
	if merged != "abcdefghijXYZ" {
		t.Errorf("重叠移除后合并结果应为 'abcdefghijXYZ', 得到 '%s'", merged)
	}
	stats := opt.GetStats()
	// tokensSaved = overlap / 4 = 10 / 4 = 2
	if stats["tokensSaved"].(int) != 2 {
		t.Errorf("tokensSaved 应为 2, 得到 %v", stats["tokensSaved"])
	}
}

func TestContextMergeOptimizer_GetMergeCount(t *testing.T) {
	opt := NewContextMergeOptimizer(100)
	opt.AddFragment("f1", "aaa", 3)

	opt.Merge()
	opt.Merge()

	if opt.GetMergeCount() != 2 {
		t.Errorf("mergeCount 应为 2, 得到 %d", opt.GetMergeCount())
	}
}

func TestContextMergeOptimizer_Stats(t *testing.T) {
	opt := NewContextMergeOptimizer(50)
	opt.AddFragment("f1", "hello", 5)
	opt.AddFragment("f2", "world", 5)
	opt.Merge()

	stats := opt.GetStats()
	if stats["fragmentCount"].(int) != 2 {
		t.Errorf("fragmentCount 应为 2, 得到 %v", stats["fragmentCount"])
	}
	if stats["mergeCount"].(int) != 1 {
		t.Errorf("mergeCount 应为 1, 得到 %v", stats["mergeCount"])
	}
	if stats["overlapThreshold"].(int) != 50 {
		t.Errorf("overlapThreshold 应为 50, 得到 %v", stats["overlapThreshold"])
	}
}

func TestContextMergeOptimizer_Reset(t *testing.T) {
	opt := NewContextMergeOptimizer(100)
	opt.AddFragment("f1", "abcdefghij", 10)
	opt.AddFragment("f2", "abcdefghijXYZ", 13)
	opt.Merge()

	opt.Reset()
	stats := opt.GetStats()
	if stats["fragmentCount"].(int) != 0 {
		t.Errorf("Reset 后 fragmentCount 应为 0, 得到 %v", stats["fragmentCount"])
	}
	if stats["mergeCount"].(int) != 0 {
		t.Errorf("Reset 后 mergeCount 应为 0, 得到 %v", stats["mergeCount"])
	}
	if stats["tokensSaved"].(int) != 0 {
		t.Errorf("Reset 后 tokensSaved 应为 0, 得到 %v", stats["tokensSaved"])
	}
}

// ── OPT-169: TokenAwareRouter ──

func TestTokenAwareRouter_RegisterAndRoute(t *testing.T) {
	router := NewTokenAwareRouter()
	// ep1: weight = 1000/20 = 50
	router.RegisterEndpoint("ep1", 1000, 20)
	// ep2: weight = 2000/10 = 200 (最高)
	router.RegisterEndpoint("ep2", 2000, 10)
	// ep3: weight = 500/5 = 100
	router.RegisterEndpoint("ep3", 500, 5)

	name, ok := router.Route(500)
	if !ok {
		t.Errorf("Route 应成功, 得到 false")
	}
	if name != "ep2" {
		t.Errorf("Route 应返回权重最高的 ep2, 得到 %s", name)
	}
}

func TestTokenAwareRouter_InsufficientTokens(t *testing.T) {
	router := NewTokenAwareRouter()
	// ep1: 只有 100 tokens
	router.RegisterEndpoint("ep1", 100, 10)

	name, ok := router.Route(200)
	if ok {
		t.Errorf("token 不足时 Route 应返回 false, 得到 ok=true, name=%s", name)
	}
	if name != "" {
		t.Errorf("token 不足时 Route 应返回空名称, 得到 %s", name)
	}
}

func TestTokenAwareRouter_UpdateEndpointTokens(t *testing.T) {
	router := NewTokenAwareRouter()
	// ep1: weight = 1000/10 = 100
	router.RegisterEndpoint("ep1", 1000, 10)
	// ep2: weight = 500/10 = 50
	router.RegisterEndpoint("ep2", 500, 10)

	// 初始 ep1 权重更高
	name, _ := router.Route(100)
	if name != "ep1" {
		t.Errorf("首次 Route 应返回 ep1, 得到 %s", name)
	}

	// 更新 ep2 的 token 数使其权重变为最高
	router.UpdateEndpointTokens("ep2", 10000) // weight = 10000/10 = 1000
	name, _ = router.Route(100)
	if name != "ep2" {
		t.Errorf("UpdateEndpointTokens 后 Route 应返回 ep2, 得到 %s", name)
	}
}

func TestTokenAwareRouter_RouteDecreasesTokens(t *testing.T) {
	router := NewTokenAwareRouter()
	router.RegisterEndpoint("ep1", 1000, 10)

	// 消耗 300 tokens, 剩余 700
	router.Route(300)

	// 再消耗 700, 剩余 0
	name, ok := router.Route(700)
	if !ok {
		t.Errorf("Route(700) 在剩余 700 tokens 时应成功, 得到 false")
	}
	if name != "ep1" {
		t.Errorf("Route 应返回 ep1, 得到 %s", name)
	}

	// token 已耗尽, 路由应失败
	_, ok = router.Route(100)
	if ok {
		t.Errorf("token 耗尽后 Route 应返回 false, 得到 true")
	}
}

func TestTokenAwareRouter_Stats(t *testing.T) {
	router := NewTokenAwareRouter()
	router.RegisterEndpoint("ep1", 1000, 10)
	router.RegisterEndpoint("ep2", 2000, 20)

	router.Route(500)
	router.Route(300)

	stats := router.GetStats()
	if stats["endpointCount"].(int) != 2 {
		t.Errorf("endpointCount 应为 2, 得到 %v", stats["endpointCount"])
	}
	if stats["routeCount"].(int) != 2 {
		t.Errorf("routeCount 应为 2, 得到 %v", stats["routeCount"])
	}
	if stats["totalTokensRouted"].(int) != 800 {
		t.Errorf("totalTokensRouted 应为 800, 得到 %v", stats["totalTokensRouted"])
	}
}

func TestTokenAwareRouter_Reset(t *testing.T) {
	router := NewTokenAwareRouter()
	router.RegisterEndpoint("ep1", 1000, 10)
	router.Route(500)

	router.Reset()
	stats := router.GetStats()
	if stats["endpointCount"].(int) != 0 {
		t.Errorf("Reset 后 endpointCount 应为 0, 得到 %v", stats["endpointCount"])
	}
	if stats["routeCount"].(int) != 0 {
		t.Errorf("Reset 后 routeCount 应为 0, 得到 %v", stats["routeCount"])
	}
	if stats["totalTokensRouted"].(int) != 0 {
		t.Errorf("Reset 后 totalTokensRouted 应为 0, 得到 %v", stats["totalTokensRouted"])
	}
}

// ── OPT-170: PromptCacheWarmer ──

func TestPromptCacheWarmer_EnqueueDequeue(t *testing.T) {
	warmer := NewPromptCacheWarmer(10)
	warmer.Enqueue("h1", "content1", 1)
	warmer.Enqueue("h2", "content2", 5)
	warmer.Enqueue("h3", "content3", 3)

	// Dequeue 应按优先级降序返回
	task, ok := warmer.Dequeue()
	if !ok {
		t.Errorf("Dequeue 应成功, 得到 false")
	}
	if task.Priority != 5 {
		t.Errorf("首次 Dequeue 应返回优先级 5, 得到 %d", task.Priority)
	}
	if task.PromptHash != "h2" {
		t.Errorf("首次 Dequeue 应返回 h2, 得到 %s", task.PromptHash)
	}

	task, _ = warmer.Dequeue()
	if task.Priority != 3 {
		t.Errorf("第二次 Dequeue 应返回优先级 3, 得到 %d", task.Priority)
	}

	task, _ = warmer.Dequeue()
	if task.Priority != 1 {
		t.Errorf("第三次 Dequeue 应返回优先级 1, 得到 %d", task.Priority)
	}

	// 队列已空
	_, ok = warmer.Dequeue()
	if ok {
		t.Errorf("空队列 Dequeue 应返回 false")
	}
}

func TestPromptCacheWarmer_MaxQueueSize(t *testing.T) {
	warmer := NewPromptCacheWarmer(2)
	if !warmer.Enqueue("h1", "content1", 1) {
		t.Errorf("队列未满时 Enqueue 应返回 true")
	}
	if !warmer.Enqueue("h2", "content2", 2) {
		t.Errorf("队列未满时 Enqueue 应返回 true")
	}
	if warmer.Enqueue("h3", "content3", 3) {
		t.Errorf("队列已满时 Enqueue 应返回 false")
	}
}

func TestPromptCacheWarmer_MarkWarmedIsWarmed(t *testing.T) {
	warmer := NewPromptCacheWarmer(10)
	warmer.MarkWarmed("h1", true)
	warmer.MarkWarmed("h2", false)

	if !warmer.IsWarmed("h1") {
		t.Errorf("IsWarmed(h1) 应返回 true (标记成功)")
	}
	if warmer.IsWarmed("h2") {
		t.Errorf("IsWarmed(h2) 应返回 false (标记失败)")
	}
	if warmer.IsWarmed("h3") {
		t.Errorf("IsWarmed(h3) 应返回 false (未标记)")
	}
}

func TestPromptCacheWarmer_Stats(t *testing.T) {
	warmer := NewPromptCacheWarmer(10)
	warmer.Enqueue("h1", "content1", 1)
	warmer.Enqueue("h2", "content2", 2)
	warmer.MarkWarmed("h3", true)
	warmer.MarkWarmed("h4", false)

	stats := warmer.GetStats()
	if stats["queueSize"].(int) != 2 {
		t.Errorf("queueSize 应为 2, 得到 %v", stats["queueSize"])
	}
	if stats["maxQueueSize"].(int) != 10 {
		t.Errorf("maxQueueSize 应为 10, 得到 %v", stats["maxQueueSize"])
	}
	if stats["warmedCount"].(int) != 2 {
		t.Errorf("warmedCount 应为 2, 得到 %v", stats["warmedCount"])
	}
	if stats["totalWarmed"].(int) != 1 {
		t.Errorf("totalWarmed 应为 1, 得到 %v", stats["totalWarmed"])
	}
	if stats["totalFailed"].(int) != 1 {
		t.Errorf("totalFailed 应为 1, 得到 %v", stats["totalFailed"])
	}
}

func TestPromptCacheWarmer_Reset(t *testing.T) {
	warmer := NewPromptCacheWarmer(10)
	warmer.Enqueue("h1", "content1", 1)
	warmer.MarkWarmed("h2", true)

	warmer.Reset()
	stats := warmer.GetStats()
	if stats["queueSize"].(int) != 0 {
		t.Errorf("Reset 后 queueSize 应为 0, 得到 %v", stats["queueSize"])
	}
	if stats["warmedCount"].(int) != 0 {
		t.Errorf("Reset 后 warmedCount 应为 0, 得到 %v", stats["warmedCount"])
	}
	if stats["totalWarmed"].(int) != 0 {
		t.Errorf("Reset 后 totalWarmed 应为 0, 得到 %v", stats["totalWarmed"])
	}
	if stats["totalFailed"].(int) != 0 {
		t.Errorf("Reset 后 totalFailed 应为 0, 得到 %v", stats["totalFailed"])
	}
}

func TestPromptCacheWarmer_DequeueEmpty(t *testing.T) {
	warmer := NewPromptCacheWarmer(10)
	task, ok := warmer.Dequeue()
	if ok {
		t.Errorf("空队列 Dequeue 应返回 false, 得到 true")
	}
	if task.PromptHash != "" {
		t.Errorf("空队列 Dequeue 应返回空任务, 得到 hash=%s", task.PromptHash)
	}
}
