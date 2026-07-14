package agent

import (
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

// TestCachePrefixEnforcerStability 测试缓存前缀稳定性检测器
func TestCachePrefixEnforcerStability(t *testing.T) {
	enforcer := NewCachePrefixEnforcer()

	tools := []provider.ToolSchema{
		{Name: "bash", Description: "run command", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "read_file", Description: "read file", Parameters: json.RawMessage(`{"type":"object"}`)},
	}

	// 第一次捕获 — 无变化（首次）
	fp1 := enforcer.CaptureFingerprint("system v1", tools)
	change := enforcer.CheckPrefixStability(fp1, 1)
	if change != nil {
		t.Fatal("first capture should not report change")
	}

	// 第二次 — 相同前缀，无变化
	fp2 := enforcer.CaptureFingerprint("system v1", tools)
	change = enforcer.CheckPrefixStability(fp2, 2)
	if change != nil {
		t.Fatal("identical prefix should not report change")
	}

	// 第三次 — system prompt 变化
	fp3 := enforcer.CaptureFingerprint("system v2 CHANGED", tools)
	change = enforcer.CheckPrefixStability(fp3, 3)
	if change == nil {
		t.Fatal("system change should report change")
	}
	if change.Impact != "full_invalidation" {
		t.Fatalf("expected full_invalidation, got %s", change.Impact)
	}

	// 第四次 — tools 变化
	tools2 := append(tools, provider.ToolSchema{Name: "new_tool"})
	fp4 := enforcer.CaptureFingerprint("system v2 CHANGED", tools2)
	change = enforcer.CheckPrefixStability(fp4, 4)
	if change == nil {
		t.Fatal("tools change should report change")
	}
}

// TestCachePrefixEnforcerUsageTracking 测试缓存使用追踪
func TestCachePrefixEnforcerUsageTracking(t *testing.T) {
	enforcer := NewCachePrefixEnforcer()
	tools := []provider.ToolSchema{{Name: "bash"}}
	fp := enforcer.CaptureFingerprint("system", tools)

	// 模拟 3 次 API 请求，每次先检查前缀稳定性，再记录缓存使用

	// 请求 1: 缓存命中
	enforcer.CheckPrefixStability(fp, 1)
	enforcer.RecordCacheUsage(&provider.Usage{
		CacheHitTokens:  5000,
		CacheMissTokens: 1000,
	})

	// 请求 2: 缓存未命中
	enforcer.CheckPrefixStability(fp, 2)
	enforcer.RecordCacheUsage(&provider.Usage{
		CacheHitTokens:  0,
		CacheMissTokens: 6000,
	})

	// 请求 3: 缓存命中
	enforcer.CheckPrefixStability(fp, 3)
	enforcer.RecordCacheUsage(&provider.Usage{
		CacheHitTokens:  8000,
		CacheMissTokens: 500,
	})

	stats := enforcer.GetStats()
	if stats.TotalRequests != 3 {
		t.Fatalf("expected 3 requests, got %d", stats.TotalRequests)
	}
	if stats.CacheHits != 2 {
		t.Fatalf("expected 2 cache hits, got %d", stats.CacheHits)
	}
	if stats.CacheMisses != 1 {
		t.Fatalf("expected 1 cache miss, got %d", stats.CacheMisses)
	}
	// 13000 hit tokens × 9 = 117000 savings
	if stats.TokenSavings != 13000*9 {
		t.Fatalf("expected savings %d, got %d", 13000*9, stats.TokenSavings)
	}
}

// TestCacheHitRateTracker 测试命中率追踪器
func TestCacheHitRateTracker(t *testing.T) {
	tracker := NewCacheHitRateTracker(5)

	// 3 命中, 2 未命中
	tracker.Record(true)
	tracker.Record(true)
	tracker.Record(false)
	tracker.Record(true)
	tracker.Record(false)

	rate := tracker.HitRate()
	if rate != 0.6 {
		t.Fatalf("expected 60%% hit rate, got %.1f%%", rate*100)
	}

	// 窗口滑动 — 添加第 6 条，最早的被移除
	tracker.Record(true)
	rate = tracker.HitRate()
	// 窗口: true, false, true, false, true → 60%
	if rate != 0.6 {
		t.Fatalf("expected 60%% after sliding, got %.1f%%", rate*100)
	}
}

// TestClassifyToolResultStability 测试工具结果稳定性分类
func TestClassifyToolResultStability(t *testing.T) {
	// read_file 是稳定的
	if s := ClassifyToolResultStability("read_file", "file content"); s != StabilityStable {
		t.Fatalf("read_file should be stable, got %d", s)
	}

	// bash 是易变的
	if s := ClassifyToolResultStability("bash", "command output with timestamp"); s != StabilityVolatile {
		t.Fatalf("bash should be volatile, got %d", s)
	}

	// bash 但带 ls -l 输出特征 → 半稳定
	if s := ClassifyToolResultStability("bash", "total 8\ndrwxr-xr-x 2 user user 4096 Jan 1 src"); s != StabilitySemiStable {
		t.Fatalf("bash with ls output should be semi-stable, got %d", s)
	}

	// grep 是半稳定的
	if s := ClassifyToolResultStability("grep", "result.go:42: func main()"); s != StabilitySemiStable {
		t.Fatalf("grep should be semi-stable, got %d", s)
	}

	// web_fetch 是易变的
	if s := ClassifyToolResultStability("web_fetch", "web page content"); s != StabilityVolatile {
		t.Fatalf("web_fetch should be volatile, got %d", s)
	}

	// edit_file 是易变的（包含确认信息）
	if s := ClassifyToolResultStability("edit_file", "edited 3 lines"); s != StabilityVolatile {
		t.Fatalf("edit_file should be volatile, got %d", s)
	}

	// ls 是半稳定的
	if s := ClassifyToolResultStability("ls", "dir1 dir2 file1.go"); s != StabilitySemiStable {
		t.Fatalf("ls should be semi-stable, got %d", s)
	}
}

// TestIsCacheSafeForBreakpoint 测试缓存安全判断
func TestIsCacheSafeForBreakpoint(t *testing.T) {
	if !IsCacheSafeForBreakpoint("read_file", "content") {
		t.Fatal("read_file should be cache-safe")
	}
	if IsCacheSafeForBreakpoint("bash", "output") {
		t.Fatal("bash should not be cache-safe")
	}
}
