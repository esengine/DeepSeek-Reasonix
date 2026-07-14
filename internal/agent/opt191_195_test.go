package agent

import (
	"testing"
)

// ==============================================================
// OPT-191: TokenAwareQuotaManager / Token感知配额管理器
// ==============================================================

// TestTokenAwareQuotaManager_SetQuotaAndConsume 验证 SetQuota + Consume 正确消耗配额
func TestTokenAwareQuotaManager_SetQuotaAndConsume(t *testing.T) {
	m := NewTokenAwareQuotaManager()
	m.SetQuota("tenantA", 100)
	if !m.Consume("tenantA", 30) {
		t.Errorf("Consume(30) 在配额100内应成功，期望 true，实际 false")
	}
	if got := m.GetRemaining("tenantA"); got != 70 {
		t.Errorf("消耗30后剩余配额 = %d，期望 70", got)
	}
}

// TestTokenAwareQuotaManager_ConsumeExceedQuota 验证超出配额时 Consume 返回 false
func TestTokenAwareQuotaManager_ConsumeExceedQuota(t *testing.T) {
	m := NewTokenAwareQuotaManager()
	m.SetQuota("tenantA", 50)
	if m.Consume("tenantA", 60) {
		t.Errorf("Consume(60) 超出配额50，应返回 false，实际 true")
	}
	// 超出后剩余配额应保持不变
	if got := m.GetRemaining("tenantA"); got != 50 {
		t.Errorf("超出配额后剩余应不变 = %d，期望 50", got)
	}
}

// TestTokenAwareQuotaManager_GetRemaining 验证 GetRemaining 返回正确剩余配额
func TestTokenAwareQuotaManager_GetRemaining(t *testing.T) {
	m := NewTokenAwareQuotaManager()
	m.SetQuota("tenantA", 200)
	m.Consume("tenantA", 80)
	if got := m.GetRemaining("tenantA"); got != 120 {
		t.Errorf("GetRemaining = %d，期望 120", got)
	}
	// 未设置配额的租户返回 0
	if got := m.GetRemaining("unknown"); got != 0 {
		t.Errorf("未设置配额的租户剩余 = %d，期望 0", got)
	}
}

// TestTokenAwareQuotaManager_Refill 验证 Refill 补充配额
func TestTokenAwareQuotaManager_Refill(t *testing.T) {
	m := NewTokenAwareQuotaManager()
	m.SetQuota("tenantA", 100)
	m.Consume("tenantA", 50)
	if got := m.GetRemaining("tenantA"); got != 50 {
		t.Errorf("消耗50后剩余 = %d，期望 50", got)
	}
	m.Refill("tenantA", 30)
	if got := m.GetRemaining("tenantA"); got != 80 {
		t.Errorf("Refill(30)后剩余 = %d，期望 80", got)
	}
}

// TestTokenAwareQuotaManager_StatsTenantCount 验证 Stats 中 tenantCount 正确
func TestTokenAwareQuotaManager_StatsTenantCount(t *testing.T) {
	m := NewTokenAwareQuotaManager()
	m.SetQuota("tenantA", 100)
	m.SetQuota("tenantB", 200)
	m.SetQuota("tenantC", 300)
	stats := m.GetStats()
	tenantCount, ok := stats["tenantCount"].(int)
	if !ok {
		t.Errorf("stats[\"tenantCount\"] 类型断言失败")
	}
	if tenantCount != 3 {
		t.Errorf("tenantCount = %d，期望 3", tenantCount)
	}
	totalAllocated, ok := stats["totalAllocated"].(int)
	if !ok {
		t.Errorf("stats[\"totalAllocated\"] 类型断言失败")
	}
	if totalAllocated != 600 {
		t.Errorf("totalAllocated = %d，期望 600", totalAllocated)
	}
}

// TestTokenAwareQuotaManager_Reset 验证 Reset 清空所有数据
func TestTokenAwareQuotaManager_Reset(t *testing.T) {
	m := NewTokenAwareQuotaManager()
	m.SetQuota("tenantA", 100)
	m.Consume("tenantA", 30)
	m.Reset()
	stats := m.GetStats()
	tenantCount, _ := stats["tenantCount"].(int)
	if tenantCount != 0 {
		t.Errorf("Reset后 tenantCount = %d，期望 0", tenantCount)
	}
	if got := m.GetRemaining("tenantA"); got != 0 {
		t.Errorf("Reset后剩余配额 = %d，期望 0", got)
	}
}

// ==============================================================
// OPT-192: CacheFreshnessGuarantor / 缓存新鲜度保证器
// ==============================================================

// TestCacheFreshnessGuarantor_PutAndIsFresh 验证 Put 后在有效期内 IsFresh 返回 true
func TestCacheFreshnessGuarantor_PutAndIsFresh(t *testing.T) {
	g := NewCacheFreshnessGuarantor(100)
	g.Put("key1")
	// Put 后 expiry = 1 + 100 = 101，currentTime=50 时仍新鲜
	if !g.IsFresh("key1", 50) {
		t.Errorf("IsFresh(key1, 50) 在有效期内应返回 true，实际 false")
	}
}

// TestCacheFreshnessGuarantor_IsFreshExpired 验证过期后 IsFresh 返回 false
func TestCacheFreshnessGuarantor_IsFreshExpired(t *testing.T) {
	g := NewCacheFreshnessGuarantor(100)
	g.Put("key1")
	// expiry = 101，currentTime=101 时已过期（expiry > currentTime 为 false）
	if g.IsFresh("key1", 101) {
		t.Errorf("IsFresh(key1, 101) 过期后应返回 false，实际 true")
	}
	// 更远的未来时间也应过期
	if g.IsFresh("key1", 200) {
		t.Errorf("IsFresh(key1, 200) 过期后应返回 false，实际 true")
	}
}

// TestCacheFreshnessGuarantor_RemoveStale 验证 RemoveStale 移除过期项
func TestCacheFreshnessGuarantor_RemoveStale(t *testing.T) {
	g := NewCacheFreshnessGuarantor(100)
	g.Put("a") // expiry = 101
	g.Put("b") // expiry = 102
	removed := g.RemoveStale(101)
	if removed != 1 {
		t.Errorf("RemoveStale(101) 移除数量 = %d，期望 1", removed)
	}
	// "a" 已被移除（expiry=101 <= 101），"b" 仍存在（expiry=102 > 101）
	if g.IsFresh("a", 50) {
		t.Errorf("RemoveStale 后 a 应已被移除，IsFresh 应返回 false")
	}
	if !g.IsFresh("b", 50) {
		t.Errorf("RemoveStale 后 b 应仍新鲜，IsFresh 应返回 true")
	}
}

// TestCacheFreshnessGuarantor_StatsEntryCount 验证 Stats 中 entryCount 正确
func TestCacheFreshnessGuarantor_StatsEntryCount(t *testing.T) {
	g := NewCacheFreshnessGuarantor(100)
	g.Put("a")
	g.Put("b")
	g.Put("c")
	stats := g.GetStats()
	entryCount, ok := stats["entryCount"].(int)
	if !ok {
		t.Errorf("stats[\"entryCount\"] 类型断言失败")
	}
	if entryCount != 3 {
		t.Errorf("entryCount = %d，期望 3", entryCount)
	}
	maxAge, ok := stats["maxAge"].(int)
	if !ok {
		t.Errorf("stats[\"maxAge\"] 类型断言失败")
	}
	if maxAge != 100 {
		t.Errorf("maxAge = %d，期望 100", maxAge)
	}
}

// TestCacheFreshnessGuarantor_Reset 验证 Reset 清空所有缓存项和统计
func TestCacheFreshnessGuarantor_Reset(t *testing.T) {
	g := NewCacheFreshnessGuarantor(100)
	g.Put("a")
	g.Put("b")
	g.Reset()
	stats := g.GetStats()
	entryCount, _ := stats["entryCount"].(int)
	if entryCount != 0 {
		t.Errorf("Reset后 entryCount = %d，期望 0", entryCount)
	}
	if g.IsFresh("a", 0) {
		t.Errorf("Reset后 a 应不存在，IsFresh 应返回 false")
	}
}

// ==============================================================
// OPT-193: ContextSimilarityDetector / 上下文相似度检测器
// ==============================================================

// TestContextSimilarityDetector_CompareSameText 验证相同文本相似度为 1.0
func TestContextSimilarityDetector_CompareSameText(t *testing.T) {
	d := NewContextSimilarityDetector(0.5)
	sim := d.Compare("hello world", "hello world")
	if sim != 1.0 {
		t.Errorf("相同文本相似度 = %v，期望 1.0", sim)
	}
}

// TestContextSimilarityDetector_CompareDisjoint 验证不相交文本相似度为 0.0
func TestContextSimilarityDetector_CompareDisjoint(t *testing.T) {
	d := NewContextSimilarityDetector(0.5)
	sim := d.Compare("hello world", "foo bar")
	if sim != 0.0 {
		t.Errorf("不相交文本相似度 = %v，期望 0.0", sim)
	}
}

// TestContextSimilarityDetector_IsDuplicateAboveThreshold 验证超过阈值时 IsDuplicate 返回 true
func TestContextSimilarityDetector_IsDuplicateAboveThreshold(t *testing.T) {
	d := NewContextSimilarityDetector(0.5)
	// "hello world foo" vs "hello world bar"：交集2，并集4，相似度0.5 >= 阈值0.5
	if !d.IsDuplicate("hello world foo", "hello world bar") {
		t.Errorf("IsDuplicate 相似度0.5 >= 阈值0.5 应返回 true，实际 false")
	}
}

// TestContextSimilarityDetector_FindDuplicates 验证 FindDuplicates 返回重复对
func TestContextSimilarityDetector_FindDuplicates(t *testing.T) {
	d := NewContextSimilarityDetector(0.5)
	items := []string{"hello world", "hello world", "foo bar baz"}
	pairs := d.FindDuplicates(items)
	if len(pairs) != 1 {
		t.Errorf("FindDuplicates 返回重复对数量 = %d，期望 1", len(pairs))
	}
	if len(pairs) > 0 {
		if pairs[0][0] != 0 || pairs[0][1] != 1 {
			t.Errorf("重复对索引 = [%d, %d]，期望 [0, 1]", pairs[0][0], pairs[0][1])
		}
	}
}

// TestContextSimilarityDetector_StatsComparisons 验证 Stats 中 comparisons 正确
func TestContextSimilarityDetector_StatsComparisons(t *testing.T) {
	d := NewContextSimilarityDetector(0.5)
	d.Compare("a b", "c d")
	d.Compare("a b", "c d")
	stats := d.GetStats()
	comparisons, ok := stats["comparisons"].(int)
	if !ok {
		t.Errorf("stats[\"comparisons\"] 类型断言失败")
	}
	if comparisons != 2 {
		t.Errorf("comparisons = %d，期望 2", comparisons)
	}
	lastSim, ok := stats["lastSimilarity"].(float64)
	if !ok {
		t.Errorf("stats[\"lastSimilarity\"] 类型断言失败")
	}
	if lastSim != 0.0 {
		t.Errorf("lastSimilarity = %v，期望 0.0（a b 与 c d 不相交）", lastSim)
	}
}

// TestContextSimilarityDetector_Reset 验证 Reset 清空所有统计
func TestContextSimilarityDetector_Reset(t *testing.T) {
	d := NewContextSimilarityDetector(0.5)
	d.Compare("hello world", "hello world")
	d.IsDuplicate("hello world", "hello world")
	d.Reset()
	stats := d.GetStats()
	comparisons, _ := stats["comparisons"].(int)
	if comparisons != 0 {
		t.Errorf("Reset后 comparisons = %d，期望 0", comparisons)
	}
	duplicatesFound, _ := stats["duplicatesFound"].(int)
	if duplicatesFound != 0 {
		t.Errorf("Reset后 duplicatesFound = %d，期望 0", duplicatesFound)
	}
}

// ==============================================================
// OPT-194: TokenAwareRateLimiter / Token感知速率限制器
// ==============================================================

// TestTokenAwareRateLimiter_TryAcquireWithinBudget 验证预算内 TryAcquire 返回 true
func TestTokenAwareRateLimiter_TryAcquireWithinBudget(t *testing.T) {
	r := NewTokenAwareRateLimiter(100, 10)
	if !r.TryAcquire(30, 1) {
		t.Errorf("TryAcquire(30, 1) 在预算100内应返回 true，实际 false")
	}
	if got := r.GetCurrentUsage(); got != 30 {
		t.Errorf("TryAcquire后当前使用量 = %d，期望 30", got)
	}
}

// TestTokenAwareRateLimiter_TryAcquireExceedBudget 验证超出预算 TryAcquire 返回 false
func TestTokenAwareRateLimiter_TryAcquireExceedBudget(t *testing.T) {
	r := NewTokenAwareRateLimiter(100, 10)
	r.TryAcquire(60, 1) // 60 <= 100，成功
	if r.TryAcquire(50, 1) {
		t.Errorf("TryAcquire(50, 1) 累计110超出预算100应返回 false，实际 true")
	}
	// 被拒绝后使用量应不变
	if got := r.GetCurrentUsage(); got != 60 {
		t.Errorf("被拒绝后当前使用量 = %d，期望 60", got)
	}
}

// TestTokenAwareRateLimiter_WindowReset 验证窗口切换时重置使用量
func TestTokenAwareRateLimiter_WindowReset(t *testing.T) {
	r := NewTokenAwareRateLimiter(100, 10)
	r.TryAcquire(80, 1) // 窗口1使用80
	// 窗口切换到2，使用量应重置为0
	if !r.TryAcquire(60, 2) {
		t.Errorf("窗口切换后 TryAcquire(60, 2) 应在预算内返回 true，实际 false")
	}
	if got := r.GetCurrentUsage(); got != 60 {
		t.Errorf("窗口切换后当前使用量 = %d，期望 60", got)
	}
}

// TestTokenAwareRateLimiter_GetUtilization 验证 GetUtilization 返回正确利用率
func TestTokenAwareRateLimiter_GetUtilization(t *testing.T) {
	r := NewTokenAwareRateLimiter(100, 10)
	r.TryAcquire(50, 1)
	util := r.GetUtilization()
	if util != 0.5 {
		t.Errorf("GetUtilization = %v，期望 0.5", util)
	}
}

// TestTokenAwareRateLimiter_StatsAllowedDenied 验证 Stats 中 allowedCount 和 deniedCount
func TestTokenAwareRateLimiter_StatsAllowedDenied(t *testing.T) {
	r := NewTokenAwareRateLimiter(100, 10)
	r.TryAcquire(40, 1) // allowed，累计40
	r.TryAcquire(40, 1) // allowed，累计80
	r.TryAcquire(30, 1) // denied，80+30=110 > 100
	stats := r.GetStats()
	allowed, ok := stats["allowedCount"].(int)
	if !ok {
		t.Errorf("stats[\"allowedCount\"] 类型断言失败")
	}
	if allowed != 2 {
		t.Errorf("allowedCount = %d，期望 2", allowed)
	}
	denied, ok := stats["deniedCount"].(int)
	if !ok {
		t.Errorf("stats[\"deniedCount\"] 类型断言失败")
	}
	if denied != 1 {
		t.Errorf("deniedCount = %d，期望 1", denied)
	}
}

// TestTokenAwareRateLimiter_Reset 验证 Reset 清空所有统计和使用量
func TestTokenAwareRateLimiter_Reset(t *testing.T) {
	r := NewTokenAwareRateLimiter(100, 10)
	r.TryAcquire(50, 1) // allowed
	r.TryAcquire(60, 1) // denied
	r.Reset()
	stats := r.GetStats()
	allowed, _ := stats["allowedCount"].(int)
	if allowed != 0 {
		t.Errorf("Reset后 allowedCount = %d，期望 0", allowed)
	}
	denied, _ := stats["deniedCount"].(int)
	if denied != 0 {
		t.Errorf("Reset后 deniedCount = %d，期望 0", denied)
	}
	if got := r.GetCurrentUsage(); got != 0 {
		t.Errorf("Reset后当前使用量 = %d，期望 0", got)
	}
}

// ==============================================================
// OPT-195: PromptEvictionPolicy / 提示驱逐策略器
// ==============================================================

// TestPromptEvictionPolicy_LRUEviction 验证 LRU 策略驱逐最久未访问的条目
func TestPromptEvictionPolicy_LRUEviction(t *testing.T) {
	p := NewPromptEvictionPolicy(10, "lru")
	p.Add("a", 100, 1)
	p.Add("b", 100, 1)
	p.Add("c", 100, 1)
	p.Access("a", 10)
	p.Access("b", 20)
	p.Access("c", 15)
	// LRU：LastAccess 最小的是 a（10），应被驱逐
	evicted := p.Evict()
	if evicted != "a" {
		t.Errorf("LRU驱逐 key = %q，期望 \"a\"", evicted)
	}
}

// TestPromptEvictionPolicy_PriorityEviction 验证 priority 策略驱逐最低优先级条目
func TestPromptEvictionPolicy_PriorityEviction(t *testing.T) {
	p := NewPromptEvictionPolicy(10, "priority")
	p.Add("a", 100, 5)
	p.Add("b", 100, 1)
	p.Add("c", 100, 10)
	// priority：Priority 最低的是 b（1），应被驱逐
	evicted := p.Evict()
	if evicted != "b" {
		t.Errorf("priority驱逐 key = %q，期望 \"b\"", evicted)
	}
}

// TestPromptEvictionPolicy_AccessUpdatesTime 验证 Access 更新访问时间从而改变驱逐顺序
func TestPromptEvictionPolicy_AccessUpdatesTime(t *testing.T) {
	p := NewPromptEvictionPolicy(10, "lru")
	p.Add("a", 100, 1)
	p.Add("b", 100, 1)
	p.Access("a", 10)
	p.Access("b", 20)
	// 此时 a=10, b=20，LRU 应驱逐 a
	// 但先通过 Access 更新 a 的访问时间到 30
	p.Access("a", 30)
	// 现在 a=30, b=20，LRU 应驱逐 b（访问时间更早）
	evicted := p.Evict()
	if evicted != "b" {
		t.Errorf("更新访问时间后 LRU 驱逐 key = %q，期望 \"b\"（b=20 比 a=30 更早）", evicted)
	}
}

// TestPromptEvictionPolicy_StatsEntryCount 验证 Stats 中 entryCount 正确
func TestPromptEvictionPolicy_StatsEntryCount(t *testing.T) {
	p := NewPromptEvictionPolicy(10, "lru")
	p.Add("a", 100, 1)
	p.Add("b", 100, 1)
	p.Add("c", 100, 1)
	stats := p.GetStats()
	entryCount, ok := stats["entryCount"].(int)
	if !ok {
		t.Errorf("stats[\"entryCount\"] 类型断言失败")
	}
	if entryCount != 3 {
		t.Errorf("entryCount = %d，期望 3", entryCount)
	}
	policy, ok := stats["policy"].(string)
	if !ok {
		t.Errorf("stats[\"policy\"] 类型断言失败")
	}
	if policy != "lru" {
		t.Errorf("policy = %q，期望 \"lru\"", policy)
	}
}

// TestPromptEvictionPolicy_Reset 验证 Reset 清空所有条目和统计
func TestPromptEvictionPolicy_Reset(t *testing.T) {
	p := NewPromptEvictionPolicy(10, "lru")
	p.Add("a", 100, 1)
	p.Add("b", 100, 1)
	p.Evict()
	p.Reset()
	stats := p.GetStats()
	entryCount, _ := stats["entryCount"].(int)
	if entryCount != 0 {
		t.Errorf("Reset后 entryCount = %d，期望 0", entryCount)
	}
	evictedCount, _ := stats["evictedCount"].(int)
	if evictedCount != 0 {
		t.Errorf("Reset后 evictedCount = %d，期望 0", evictedCount)
	}
}
