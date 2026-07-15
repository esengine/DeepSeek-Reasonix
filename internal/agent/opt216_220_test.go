package agent

import (
	"strconv"
	"strings"
	"testing"
)

// ═══════════════════════════════════════════════════════════════════════════
// OPT-216: TokenAwareShardManager 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestTASM_Assign_ReturnsValidShardID 验证 Assign 返回有效的分片 ID（0 <= id < shardCount）。
func TestTASM_Assign_ReturnsValidShardID(t *testing.T) {
	shardCount := 8
	mgr := NewTokenAwareShardManager(shardCount)

	for i := 0; i < 100; i++ {
		key := "key-" + strconv.Itoa(i)
		shardID := mgr.Assign(key)
		if shardID < 0 || shardID >= shardCount {
			t.Errorf("Assign(%q) = %d, 期望在 [0, %d) 范围内", key, shardID, shardCount)
		}
	}
}

// TestTASM_Assign_SameKeySameShard 验证相同 key 总是分配到同一分片。
func TestTASM_Assign_SameKeySameShard(t *testing.T) {
	mgr := NewTokenAwareShardManager(16)
	key := "consistent-key"

	first := mgr.Assign(key)
	for i := 0; i < 20; i++ {
		shardID := mgr.Assign(key)
		if shardID != first {
			t.Errorf("相同 key %q 第 %d 次分配到分片 %d, 期望始终为 %d", key, i+1, shardID, first)
		}
	}
}

// TestTASM_GetShard_ReturnsKeys 验证 GetShard 返回分片中的所有 key。
func TestTASM_GetShard_ReturnsKeys(t *testing.T) {
	mgr := NewTokenAwareShardManager(1) // 单分片，所有 key 都在分片 0
	keys := []string{"alpha", "beta", "gamma", "delta"}
	for _, k := range keys {
		mgr.Assign(k)
	}

	got := mgr.GetShard(0)
	if len(got) != len(keys) {
		t.Errorf("GetShard(0) 返回 %d 个 key, 期望 %d", len(got), len(keys))
	}

	// 验证所有 key 都在返回结果中
	gotMap := make(map[string]bool)
	for _, k := range got {
		gotMap[k] = true
	}
	for _, k := range keys {
		if !gotMap[k] {
			t.Errorf("GetShard(0) 未包含 key %q", k)
		}
	}

	// 不存在的分片应返回 nil
	if mgr.GetShard(999) != nil {
		t.Errorf("GetShard(999) 对不存在的分片未返回 nil")
	}
}

// TestTASM_Migrate 验证 Migrate 将 key 从一个分片迁移到另一个分片。
func TestTASM_Migrate(t *testing.T) {
	mgr := NewTokenAwareShardManager(4)
	mgr.Assign("migratable-key")

	// 找到 key 所在的分片
	fromShard := -1
	for i := 0; i < 4; i++ {
		for _, k := range mgr.GetShard(i) {
			if k == "migratable-key" {
				fromShard = i
			}
		}
	}
	if fromShard < 0 {
		t.Fatalf("未找到 key 所在分片")
	}

	toShard := (fromShard + 1) % 4

	ok := mgr.Migrate("migratable-key", fromShard, toShard)
	if !ok {
		t.Errorf("Migrate(%q, %d, %d) 返回 false, 期望 true", "migratable-key", fromShard, toShard)
	}

	// 验证 key 已从源分片移除
	for _, k := range mgr.GetShard(fromShard) {
		if k == "migratable-key" {
			t.Errorf("迁移后源分片 %d 仍包含 key %q", fromShard, "migratable-key")
		}
	}

	// 验证 key 已添加到目标分片
	found := false
	for _, k := range mgr.GetShard(toShard) {
		if k == "migratable-key" {
			found = true
		}
	}
	if !found {
		t.Errorf("迁移后目标分片 %d 未包含 key %q", toShard, "migratable-key")
	}

	// 迁移不存在的 key 应返回 false
	if mgr.Migrate("nonexistent", fromShard, toShard) {
		t.Errorf("Migrate 不存在的 key 返回 true, 期望 false")
	}
}

// TestTASM_Stats_TotalSharded 验证 Stats 中的 totalSharded 计数正确。
func TestTASM_Stats_TotalSharded(t *testing.T) {
	mgr := NewTokenAwareShardManager(4)

	count := 50
	for i := 0; i < count; i++ {
		mgr.Assign("stat-key-" + strconv.Itoa(i))
	}

	stats := mgr.GetStats()
	totalSharded, ok := stats["totalSharded"].(int)
	if !ok {
		t.Fatalf("stats[\"totalSharded\"] 类型断言失败")
	}
	if totalSharded != count {
		t.Errorf("stats[\"totalSharded\"] = %d, 期望 %d", totalSharded, count)
	}

	shardCount, ok := stats["shardCount"].(int)
	if !ok {
		t.Fatalf("stats[\"shardCount\"] 类型断言失败")
	}
	if shardCount != 4 {
		t.Errorf("stats[\"shardCount\"] = %d, 期望 4", shardCount)
	}
}

// TestTASM_Reset 验证 Reset 清空所有分片数据与计数。
func TestTASM_Reset(t *testing.T) {
	mgr := NewTokenAwareShardManager(4)
	mgr.Assign("key1")
	mgr.Assign("key2")

	// 找到 key1 所在分片并迁移，以产生 migrations 计数
	fromShard := -1
	for i := 0; i < 4; i++ {
		for _, k := range mgr.GetShard(i) {
			if k == "key1" {
				fromShard = i
			}
		}
	}
	if fromShard >= 0 {
		toShard := (fromShard + 1) % 4
		mgr.Migrate("key1", fromShard, toShard)
	}

	mgr.Reset()

	stats := mgr.GetStats()
	totalSharded, _ := stats["totalSharded"].(int)
	if totalSharded != 0 {
		t.Errorf("Reset 后 totalSharded = %d, 期望 0", totalSharded)
	}
	migrations, _ := stats["migrations"].(int)
	if migrations != 0 {
		t.Errorf("Reset 后 migrations = %d, 期望 0", migrations)
	}

	// 验证所有分片为空
	for i := 0; i < 4; i++ {
		if len(mgr.GetShard(i)) != 0 {
			t.Errorf("Reset 后分片 %d 不为空", i)
		}
	}

	// shardCount 应保留
	shardCount, _ := stats["shardCount"].(int)
	if shardCount != 4 {
		t.Errorf("Reset 后 shardCount = %d, 期望 4（应保留配置）", shardCount)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-217: CacheInvalidationScheduler 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestCISched_ScheduleExecute_Expired 验证 Schedule + Execute 对到期任务执行失效。
func TestCISched_ScheduleExecute_Expired(t *testing.T) {
	sched := NewCacheInvalidationScheduler(100)
	sched.Schedule("key1", 100)
	sched.Schedule("key2", 200)
	sched.Schedule("key3", 300)

	// currentTime=250, key1(100) 和 key2(200) 到期
	expired := sched.Execute(250)
	if len(expired) != 2 {
		t.Errorf("Execute(250) 返回 %d 个到期 key, 期望 2", len(expired))
	}

	expiredMap := make(map[string]bool)
	for _, k := range expired {
		expiredMap[k] = true
	}
	if !expiredMap["key1"] {
		t.Errorf("Execute(250) 未返回到期 key key1")
	}
	if !expiredMap["key2"] {
		t.Errorf("Execute(250) 未返回到期 key key2")
	}
}

// TestCISched_Execute_NotExpired 验证未到期的任务不被执行。
func TestCISched_Execute_NotExpired(t *testing.T) {
	sched := NewCacheInvalidationScheduler(100)
	sched.Schedule("future-key", 1000)

	expired := sched.Execute(500)
	if len(expired) != 0 {
		t.Errorf("Execute(500) 返回 %d 个到期 key, 期望 0（key 计划失效时间为 1000）", len(expired))
	}

	// 验证任务仍在 pending 中
	if sched.GetPendingCount() != 1 {
		t.Errorf("未到期任务执行后 GetPendingCount() = %d, 期望 1", sched.GetPendingCount())
	}
}

// TestCISched_Cancel 验证 Cancel 取消待执行任务。
func TestCISched_Cancel(t *testing.T) {
	sched := NewCacheInvalidationScheduler(100)
	sched.Schedule("cancel-me", 500)

	ok := sched.Cancel("cancel-me")
	if !ok {
		t.Errorf("Cancel(\"cancel-me\") 返回 false, 期望 true")
	}

	if sched.GetPendingCount() != 0 {
		t.Errorf("Cancel 后 GetPendingCount() = %d, 期望 0", sched.GetPendingCount())
	}

	// 再次取消已不存在的 key 应返回 false
	ok = sched.Cancel("cancel-me")
	if ok {
		t.Errorf("对已取消的 key 再次 Cancel 返回 true, 期望 false")
	}
}

// TestCISched_GetPendingCount 验证 GetPendingCount 返回正确的待执行任务数量。
func TestCISched_GetPendingCount(t *testing.T) {
	sched := NewCacheInvalidationScheduler(100)
	sched.Schedule("k1", 100)
	sched.Schedule("k2", 200)
	sched.Schedule("k3", 300)

	if sched.GetPendingCount() != 3 {
		t.Errorf("GetPendingCount() = %d, 期望 3", sched.GetPendingCount())
	}

	sched.Execute(150) // 执行 k1
	if sched.GetPendingCount() != 2 {
		t.Errorf("Execute 后 GetPendingCount() = %d, 期望 2", sched.GetPendingCount())
	}
}

// TestCISched_Stats_ExecutedCount 验证 Stats 中的 executedCount 正确。
func TestCISched_Stats_ExecutedCount(t *testing.T) {
	sched := NewCacheInvalidationScheduler(100)
	sched.Schedule("a", 100)
	sched.Schedule("b", 200)
	sched.Schedule("c", 300)

	sched.Execute(250) // a 和 b 到期

	stats := sched.GetStats()
	executedCount, ok := stats["executedCount"].(int)
	if !ok {
		t.Fatalf("stats[\"executedCount\"] 类型断言失败")
	}
	if executedCount != 2 {
		t.Errorf("stats[\"executedCount\"] = %d, 期望 2", executedCount)
	}

	pendingCount, ok := stats["pendingCount"].(int)
	if !ok {
		t.Fatalf("stats[\"pendingCount\"] 类型断言失败")
	}
	if pendingCount != 1 {
		t.Errorf("stats[\"pendingCount\"] = %d, 期望 1", pendingCount)
	}
}

// TestCISched_Reset 验证 Reset 清空所有任务与计数。
func TestCISched_Reset(t *testing.T) {
	sched := NewCacheInvalidationScheduler(100)
	sched.Schedule("a", 100)
	sched.Schedule("b", 200)
	sched.Execute(150) // 执行 a
	sched.Cancel("b")  // 取消 b

	sched.Reset()

	if sched.GetPendingCount() != 0 {
		t.Errorf("Reset 后 GetPendingCount() = %d, 期望 0", sched.GetPendingCount())
	}

	stats := sched.GetStats()
	executedCount, _ := stats["executedCount"].(int)
	if executedCount != 0 {
		t.Errorf("Reset 后 executedCount = %d, 期望 0", executedCount)
	}
	cancelledCount, _ := stats["cancelledCount"].(int)
	if cancelledCount != 0 {
		t.Errorf("Reset 后 cancelledCount = %d, 期望 0", cancelledCount)
	}

	// maxDelay 应保留
	maxDelay, _ := stats["maxDelay"].(int)
	if maxDelay != 100 {
		t.Errorf("Reset 后 maxDelay = %d, 期望 100（应保留配置）", maxDelay)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-218: ContextDensityAnalyzer 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestCDA_Analyze_DensityRange 验证 Analyze 返回 0~1 之间的密度值。
func TestCDA_Analyze_DensityRange(t *testing.T) {
	analyzer := NewContextDensityAnalyzer()

	contents := []string{
		"hello world",
		"the quick brown fox jumps over the lazy dog",
		"aaa aaa aaa aaa",
		"unique words only here",
		"", // 空内容
	}

	for _, content := range contents {
		d := analyzer.Analyze(content)
		if d < 0 || d > 1 {
			t.Errorf("Analyze(%q) = %v, 期望在 [0, 1] 范围内", content, d)
		}
	}
}

// TestCDA_Analyze_SameContentSameDensity 验证相同内容返回相同密度。
func TestCDA_Analyze_SameContentSameDensity(t *testing.T) {
	analyzer := NewContextDensityAnalyzer()
	content := "the quick brown fox jumps"

	d1 := analyzer.Analyze(content)
	d2 := analyzer.Analyze(content)

	if d1 != d2 {
		t.Errorf("相同内容两次 Analyze 结果不同: %v vs %v", d1, d2)
	}
}

// TestCDA_GetAvgDensity 验证 GetAvgDensity 返回正确的平均密度。
func TestCDA_GetAvgDensity(t *testing.T) {
	analyzer := NewContextDensityAnalyzer()

	d1 := analyzer.Analyze("hello world") // 2 unique / 2 total = 1.0
	d2 := analyzer.Analyze("aaa aaa aaa") // 1 unique / 3 total
	expected := (d1 + d2) / 2

	avg := analyzer.GetAvgDensity()
	if avg != expected {
		t.Errorf("GetAvgDensity() = %v, 期望 %v", avg, expected)
	}

	// 未分析过时平均密度应为 0
	fresh := NewContextDensityAnalyzer()
	if fresh.GetAvgDensity() != 0 {
		t.Errorf("新分析器 GetAvgDensity() = %v, 期望 0", fresh.GetAvgDensity())
	}
}

// TestCDA_GetMaxMinDensity 验证 GetMaxDensity 和 GetMinDensity。
func TestCDA_GetMaxMinDensity(t *testing.T) {
	analyzer := NewContextDensityAnalyzer()

	d1 := analyzer.Analyze("aaa aaa aaa") // 低密度
	d2 := analyzer.Analyze("hello world") // 高密度

	maxD := analyzer.GetMaxDensity()
	minD := analyzer.GetMinDensity()

	if maxD < d1 || maxD < d2 {
		t.Errorf("GetMaxDensity() = %v, 应不小于已分析的密度 %v 和 %v", maxD, d1, d2)
	}
	if minD > d1 || minD > d2 {
		t.Errorf("GetMinDensity() = %v, 应不大于已分析的密度 %v 和 %v", minD, d1, d2)
	}
}

// TestCDA_Stats_Analyses 验证 Stats 中的 analyses 计数正确。
func TestCDA_Stats_Analyses(t *testing.T) {
	analyzer := NewContextDensityAnalyzer()

	count := 10
	for i := 0; i < count; i++ {
		analyzer.Analyze("some content " + strconv.Itoa(i))
	}

	stats := analyzer.GetStats()
	analyses, ok := stats["analyses"].(int)
	if !ok {
		t.Fatalf("stats[\"analyses\"] 类型断言失败")
	}
	if analyses != count {
		t.Errorf("stats[\"analyses\"] = %d, 期望 %d", analyses, count)
	}

	avgDensity, ok := stats["avgDensity"].(float64)
	if !ok {
		t.Fatalf("stats[\"avgDensity\"] 类型断言失败")
	}
	if avgDensity < 0 || avgDensity > 1 {
		t.Errorf("stats[\"avgDensity\"] = %v, 期望在 [0, 1] 范围内", avgDensity)
	}
}

// TestCDA_Reset 验证 Reset 清空所有统计。
func TestCDA_Reset(t *testing.T) {
	analyzer := NewContextDensityAnalyzer()
	analyzer.Analyze("hello world")
	analyzer.Analyze("aaa aaa aaa")

	analyzer.Reset()

	stats := analyzer.GetStats()
	analyses, _ := stats["analyses"].(int)
	if analyses != 0 {
		t.Errorf("Reset 后 analyses = %d, 期望 0", analyses)
	}

	maxDensity, _ := stats["maxDensity"].(float64)
	if maxDensity != 0 {
		t.Errorf("Reset 后 maxDensity = %v, 期望 0", maxDensity)
	}

	minDensity, _ := stats["minDensity"].(float64)
	if minDensity != 1.0 {
		t.Errorf("Reset 后 minDensity = %v, 期望 1.0", minDensity)
	}

	// 平均密度也应为 0
	if analyzer.GetAvgDensity() != 0 {
		t.Errorf("Reset 后 GetAvgDensity() = %v, 期望 0", analyzer.GetAvgDensity())
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-219: TokenAwareRetryStrategy 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestTARS_ShouldRetry_WithinMaxRetries 验证在 maxRetries 内且 tokenCost 合理时返回 true。
func TestTARS_ShouldRetry_WithinMaxRetries(t *testing.T) {
	strat := NewTokenAwareRetryStrategy(3, 100)

	for attempt := 0; attempt < 3; attempt++ {
		if !strat.ShouldRetry(attempt, 500) {
			t.Errorf("ShouldRetry(%d, 500) = false, 期望 true（attempt < maxRetries）", attempt)
		}
	}
}

// TestTARS_ShouldRetry_ExceedsMaxRetries 验证超过 maxRetries 时返回 false。
func TestTARS_ShouldRetry_ExceedsMaxRetries(t *testing.T) {
	strat := NewTokenAwareRetryStrategy(3, 100)

	if strat.ShouldRetry(3, 500) {
		t.Errorf("ShouldRetry(3, 500) = true, 期望 false（attempt >= maxRetries）")
	}
	if strat.ShouldRetry(10, 500) {
		t.Errorf("ShouldRetry(10, 500) = true, 期望 false（attempt >= maxRetries）")
	}

	// tokenCost 不合理也返回 false
	if strat.ShouldRetry(0, 0) {
		t.Errorf("ShouldRetry(0, 0) = true, 期望 false（tokenCost <= 0）")
	}
	if strat.ShouldRetry(0, 200000) {
		t.Errorf("ShouldRetry(0, 200000) = true, 期望 false（tokenCost 超过上限）")
	}
}

// TestTARS_GetBackoff_Exponential 验证 GetBackoff 返回指数退避时间。
func TestTARS_GetBackoff_Exponential(t *testing.T) {
	strat := NewTokenAwareRetryStrategy(5, 100)

	expected := []int{100, 200, 400, 800, 1600}
	for i, exp := range expected {
		got := strat.GetBackoff(i)
		if got != exp {
			t.Errorf("GetBackoff(%d) = %d, 期望 %d", i, got, exp)
		}
	}

	// attempt=0 时退避应等于 base
	if strat.GetBackoff(0) != 100 {
		t.Errorf("GetBackoff(0) = %d, 期望 100（等于 base）", strat.GetBackoff(0))
	}
}

// TestTARS_RecordRetry 验证 RecordRetry 正确记录重试。
func TestTARS_RecordRetry(t *testing.T) {
	strat := NewTokenAwareRetryStrategy(5, 100)

	strat.RecordRetry(200)
	strat.RecordRetry(300)

	stats := strat.GetStats()
	retryCount, ok := stats["retryCount"].(int)
	if !ok {
		t.Fatalf("stats[\"retryCount\"] 类型断言失败")
	}
	if retryCount != 2 {
		t.Errorf("RecordRetry 后 retryCount = %d, 期望 2", retryCount)
	}

	totalRetriedTokens, ok := stats["totalRetriedTokens"].(int)
	if !ok {
		t.Fatalf("stats[\"totalRetriedTokens\"] 类型断言失败")
	}
	if totalRetriedTokens != 500 {
		t.Errorf("totalRetriedTokens = %d, 期望 500", totalRetriedTokens)
	}

	lastBackoff, ok := stats["lastBackoff"].(int)
	if !ok {
		t.Fatalf("stats[\"lastBackoff\"] 类型断言失败")
	}
	// lastBackoff = base * 2^retryCount = 100 * 2^2 = 400
	if lastBackoff != 400 {
		t.Errorf("lastBackoff = %d, 期望 400", lastBackoff)
	}
}

// TestTARS_Stats_RetryCount 验证 Stats 返回正确的统计信息。
func TestTARS_Stats_RetryCount(t *testing.T) {
	strat := NewTokenAwareRetryStrategy(5, 50)

	// 初始状态
	stats := strat.GetStats()
	retryCount, _ := stats["retryCount"].(int)
	if retryCount != 0 {
		t.Errorf("初始 retryCount = %d, 期望 0", retryCount)
	}

	maxRetries, _ := stats["maxRetries"].(int)
	if maxRetries != 5 {
		t.Errorf("maxRetries = %d, 期望 5", maxRetries)
	}

	strat.RecordRetry(100)
	strat.RecordRetry(100)

	stats = strat.GetStats()
	retryCount, _ = stats["retryCount"].(int)
	if retryCount != 2 {
		t.Errorf("两次 RecordRetry 后 retryCount = %d, 期望 2", retryCount)
	}
}

// TestTARS_Reset 验证 Reset 清空重试计数与退避记录。
func TestTARS_Reset(t *testing.T) {
	strat := NewTokenAwareRetryStrategy(5, 100)
	strat.RecordRetry(200)
	strat.RecordRetry(300)

	strat.Reset()

	stats := strat.GetStats()
	retryCount, _ := stats["retryCount"].(int)
	if retryCount != 0 {
		t.Errorf("Reset 后 retryCount = %d, 期望 0", retryCount)
	}
	totalRetriedTokens, _ := stats["totalRetriedTokens"].(int)
	if totalRetriedTokens != 0 {
		t.Errorf("Reset 后 totalRetriedTokens = %d, 期望 0", totalRetriedTokens)
	}
	lastBackoff, _ := stats["lastBackoff"].(int)
	if lastBackoff != 0 {
		t.Errorf("Reset 后 lastBackoff = %d, 期望 0", lastBackoff)
	}

	// maxRetries 应保留
	maxRetries, _ := stats["maxRetries"].(int)
	if maxRetries != 5 {
		t.Errorf("Reset 后 maxRetries = %d, 期望 5（应保留配置）", maxRetries)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// OPT-220: PromptCacheOptimizationAdvisor 测试
// ═══════════════════════════════════════════════════════════════════════════

// TestPCOA_Analyze_ReturnsAdvice 验证 Analyze 返回非空建议字符串。
func TestPCOA_Analyze_ReturnsAdvice(t *testing.T) {
	advisor := NewPromptCacheOptimizationAdvisor()

	// 低命中率场景
	advice := advisor.Analyze(0.1, 0.8, 600)
	if advice == "" {
		t.Errorf("Analyze(0.1, 0.8, 600) 返回空字符串, 期望非空建议")
	}
	if !strings.Contains(advice, "命中率") {
		t.Errorf("Analyze(0.1, 0.8, 600) 建议 %q 未包含关键词 \"命中率\"", advice)
	}

	// 健康状态也应有建议
	healthyAdvice := advisor.Analyze(0.9, 0.1, 100)
	if healthyAdvice == "" {
		t.Errorf("Analyze(0.9, 0.1, 100) 返回空字符串, 期望非空建议")
	}
}

// TestPCOA_GetAdvice 验证 GetAdvice 获取指定类别的建议。
func TestPCOA_GetAdvice(t *testing.T) {
	advisor := NewPromptCacheOptimizationAdvisor()
	advisor.Analyze(0.1, 0.3, 100) // 触发 hitRate 类别

	advice := advisor.GetAdvice("hitRate")
	if advice == "" {
		t.Errorf("GetAdvice(\"hitRate\") 返回空字符串, 期望非空建议")
	}

	// 不存在的类别应返回空字符串
	missing := advisor.GetAdvice("nonexistent")
	if missing != "" {
		t.Errorf("GetAdvice(\"nonexistent\") = %q, 期望空字符串", missing)
	}
}

// TestPCOA_MarkImplemented 验证 MarkImplemented 标记建议已实施。
func TestPCOA_MarkImplemented(t *testing.T) {
	advisor := NewPromptCacheOptimizationAdvisor()
	advisor.Analyze(0.1, 0.3, 100) // 触发 hitRate 类别

	advisor.MarkImplemented("hitRate")

	stats := advisor.GetStats()
	implementedCount, ok := stats["implementedCount"].(int)
	if !ok {
		t.Fatalf("stats[\"implementedCount\"] 类型断言失败")
	}
	if implementedCount != 1 {
		t.Errorf("MarkImplemented 后 implementedCount = %d, 期望 1", implementedCount)
	}

	// 标记不存在的类别不应递增
	advisor.MarkImplemented("nonexistent")
	stats = advisor.GetStats()
	implementedCount, _ = stats["implementedCount"].(int)
	if implementedCount != 1 {
		t.Errorf("标记不存在的类别后 implementedCount = %d, 期望仍为 1", implementedCount)
	}
}

// TestPCOA_Stats_AdviceCount 验证 Stats 中的 adviceCount 正确。
func TestPCOA_Stats_AdviceCount(t *testing.T) {
	advisor := NewPromptCacheOptimizationAdvisor()

	// 触发 4 个不同类别
	advisor.Analyze(0.1, 0.3, 100) // hitRate 类别
	advisor.Analyze(0.5, 0.8, 100) // missRate 类别
	advisor.Analyze(0.8, 0.2, 800) // latency 类别
	advisor.Analyze(0.9, 0.1, 100) // healthy 类别

	stats := advisor.GetStats()
	adviceCount, ok := stats["adviceCount"].(int)
	if !ok {
		t.Fatalf("stats[\"adviceCount\"] 类型断言失败")
	}
	if adviceCount != 4 {
		t.Errorf("stats[\"adviceCount\"] = %d, 期望 4", adviceCount)
	}

	// 重复触发同一类别不应递增 adviceCount
	advisor.Analyze(0.05, 0.3, 100) // 仍为 hitRate 类别
	stats = advisor.GetStats()
	adviceCount, _ = stats["adviceCount"].(int)
	if adviceCount != 4 {
		t.Errorf("重复类别后 adviceCount = %d, 期望仍为 4", adviceCount)
	}
}

// TestPCOA_Reset 验证 Reset 清空所有建议与统计。
func TestPCOA_Reset(t *testing.T) {
	advisor := NewPromptCacheOptimizationAdvisor()
	advisor.Analyze(0.1, 0.3, 100)
	advisor.MarkImplemented("hitRate")

	advisor.Reset()

	stats := advisor.GetStats()
	adviceCount, _ := stats["adviceCount"].(int)
	if adviceCount != 0 {
		t.Errorf("Reset 后 adviceCount = %d, 期望 0", adviceCount)
	}
	implementedCount, _ := stats["implementedCount"].(int)
	if implementedCount != 0 {
		t.Errorf("Reset 后 implementedCount = %d, 期望 0", implementedCount)
	}
	totalImpact, _ := stats["totalImpact"].(float64)
	if totalImpact != 0 {
		t.Errorf("Reset 后 totalImpact = %v, 期望 0", totalImpact)
	}

	// 验证建议已清空
	if advisor.GetAdvice("hitRate") != "" {
		t.Errorf("Reset 后 GetAdvice(\"hitRate\") 仍返回非空字符串")
	}
}
