package agent

import (
	"encoding/json"
	"testing"
)

// TestToolResultMemo 测试工具结果记忆化
func TestToolResultMemo(t *testing.T) {
	memo := NewToolResultMemo(10)

	// 第一次调用 — 未命中
	key := MemoKey("read_file", []byte(`{"path":"main.go"}`))
	_, ok := memo.Get(key)
	if ok {
		t.Fatal("first call should not hit cache")
	}

	// 存储结果
	memo.Put("read_file", []byte(`{"path":"main.go"}`), "package main\nfunc main() {}")

	// 第二次调用 — 命中
	entry, ok := memo.Get(key)
	if !ok {
		t.Fatal("second call should hit cache")
	}
	if entry.ToolName != "read_file" {
		t.Fatalf("expected tool name read_file, got %s", entry.ToolName)
	}
	if entry.HitCount != 1 {
		t.Fatalf("expected hit count 1, got %d", entry.HitCount)
	}

	// 占位符
	placeholder := GetCachedPlaceholder(entry)
	if placeholder == "" {
		t.Fatal("placeholder should not be empty")
	}
}

// TestIsMemoizable 测试工具可记忆化判断
func TestIsMemoizable(t *testing.T) {
	if !IsMemoizable("read_file", true) {
		t.Fatal("read_file with readOnly=true should be memoizable")
	}
	if IsMemoizable("read_file", false) {
		t.Fatal("read_file with readOnly=false should not be memoizable")
	}
	if IsMemoizable("edit_file", true) {
		t.Fatal("edit_file should not be memoizable")
	}
	if IsMemoizable("bash", true) {
		t.Fatal("bash should not be memoizable (not in whitelist)")
	}
}

// TestConversationDeduplicator 测试对话历史去重
func TestConversationDeduplicator(t *testing.T) {
	dedup := NewConversationDeduplicator()

	longContent := "这是一段很长的内容，超过了最小去重长度阈值。这只是一段测试内容，用于验证去重功能是否正常工作。" +
		"我们需要确保相同的内容不会重复出现在对话历史中，以节省 token 消耗。"

	// 第一次 — 不应去重
	dedup.Record(longContent, 0)
	shouldDedup, entry := dedup.ShouldDeduplicate(longContent)
	if !shouldDedup {
		t.Fatal("duplicate content should be flagged for dedup")
	}
	if entry == nil {
		t.Fatal("entry should not be nil")
	}
	if entry.MessageIdx != 0 {
		t.Fatalf("expected message idx 0, got %d", entry.MessageIdx)
	}

	// 短内容 — 不应去重
	shortContent := "短"
	dedup.Record(shortContent, 1)
	shouldDedup2, _ := dedup.ShouldDeduplicate(shortContent)
	if shouldDedup2 {
		t.Fatal("short content should not be flagged for dedup")
	}
}

// TestContextBudget 测试自适应上下文预算
func TestContextBudget(t *testing.T) {
	cb := NewContextBudget(128000)

	// 默认级别应该是 Standard
	if cb.GetLevel() != BudgetStandard {
		t.Fatalf("expected standard level, got %s", cb.GetLevel())
	}

	// 调整为最小
	cb.AdjustForScene("simple_qa", 1)
	if cb.GetLevel() != BudgetMinimal {
		t.Fatalf("expected minimal level, got %s", cb.GetLevel())
	}

	// 最小级别的压缩阈值应该更低
	_, _, compact, _, tail := cb.GetThresholds()
	if compact >= 0.70 {
		t.Fatalf("minimal level should have compact threshold < 0.70, got %.2f", compact)
	}
	if tail > 8192 {
		t.Fatalf("minimal level should have tail <= 8192, got %d", tail)
	}

	// 调整为最大
	cb.AdjustForScene("complex_refactoring", 10)
	if cb.GetLevel() != BudgetMaximum {
		t.Fatalf("expected maximum level, got %s", cb.GetLevel())
	}

	// 最大级别的压缩阈值应该更高
	_, _, compact2, _, tail2 := cb.GetThresholds()
	if compact2 < 0.85 {
		t.Fatalf("maximum level should have compact threshold >= 0.85, got %.2f", compact2)
	}
	if tail2 < 16384 {
		t.Fatalf("maximum level should have tail >= 16384, got %d", tail2)
	}
}

// TestProviderCacheStrategy 测试 provider 感知缓存策略
func TestProviderCacheStrategy(t *testing.T) {
	// DeepSeek 策略
	ds := NewProviderCacheStrategy(ProviderDeepSeek)
	profile := ds.GetProfile()
	if profile.MaxBreakpoints != 0 {
		t.Fatal("DeepSeek should have 0 breakpoints (auto cache)")
	}
	if !profile.AutoCache {
		t.Fatal("DeepSeek should have auto cache")
	}
	if !profile.CompactAggressively {
		t.Fatal("DeepSeek should compact aggressively")
	}

	// Anthropic 策略
	anthropic := NewProviderCacheStrategy(ProviderAnthropic)
	profile2 := anthropic.GetProfile()
	if profile2.MaxBreakpoints != 4 {
		t.Fatal("Anthropic should have 4 breakpoints")
	}
	if !profile2.SupportsExplicitBP {
		t.Fatal("Anthropic should support explicit breakpoints")
	}
	if profile2.CompactAggressively {
		t.Fatal("Anthropic should not compact aggressively (high cache benefit)")
	}

	// DetectProviderType
	if DetectProviderType("deepseek-reasoner") != ProviderDeepSeek {
		t.Fatal("should detect DeepSeek")
	}
	if DetectProviderType("claude-sonnet-4") != ProviderAnthropic {
		t.Fatal("should detect Anthropic")
	}
	if DetectProviderType("gpt-4o") != ProviderOpenAI {
		t.Fatal("should detect OpenAI")
	}
	if DetectProviderType("gemini-2.0-flash") != ProviderGemini {
		t.Fatal("should detect Gemini")
	}
}

// TestCacheHealthMonitor 测试缓存健康监控器
func TestCacheHealthMonitor(t *testing.T) {
	monitor := NewCacheHealthMonitor()

	// 记录一些命中
	monitor.RecordRequest(true, 8000, 2000)
	monitor.RecordRequest(true, 9000, 1000)
	monitor.RecordRequest(true, 8500, 1500)

	health := monitor.GetHealth()
	if health.Status != "healthy" {
		t.Fatalf("expected healthy status, got %s (rate: %.1f%%)", health.Status, health.HitRate*100)
	}

	// 记录连续未命中
	monitor.RecordRequest(false, 0, 10000)
	monitor.RecordRequest(false, 0, 10000)
	monitor.RecordRequest(false, 0, 10000)
	monitor.RecordRequest(false, 0, 10000)
	monitor.RecordRequest(false, 0, 10000)

	health2 := monitor.GetHealth()
	if health2.ConsecutiveMisses < 5 {
		t.Fatalf("expected >= 5 consecutive misses, got %d", health2.ConsecutiveMisses)
	}
}

// TestPrefixPinner 测试前缀钉扎
func TestPrefixPinner(t *testing.T) {
	pinner := NewPrefixPinner()

	// 首次钉扎
	change := pinner.Pin("L1_base_prompt", "You are a helpful assistant.")
	if change != nil {
		t.Fatal("first pin should not report change")
	}

	// 相同内容 — 不应报告变化
	change2 := pinner.Pin("L1_base_prompt", "You are a helpful assistant.")
	if change2 != nil {
		t.Fatal("identical content should not report change")
	}

	// 变化内容 — 应报告变化
	change3 := pinner.Pin("L1_base_prompt", "You are a DIFFERENT assistant.")
	if change3 == nil {
		t.Fatal("changed content should report change")
	}
	if change3.Severity != "critical" {
		t.Fatalf("L1 change should be critical, got %s", change3.Severity)
	}

	// 自动恢复 — GetPinnedContent 返回钉扎的版本
	pinned := pinner.GetPinnedContent("L1_base_prompt")
	if pinned != "You are a helpful assistant." {
		t.Fatal("should return pinned content, not changed content")
	}
}

// TestToolCallBatcher 测试工具调用批处理
func TestToolCallBatcher(t *testing.T) {
	batcher := NewToolCallBatcher()

	// 添加只读工具调用
	batcher.Add(BatchedToolCall{
		ID:       "1",
		Name:     "read_file",
		Args:     json.RawMessage(`{"path":"a.go"}`),
		Result:   "package a",
		ReadOnly: true,
	})
	batcher.Add(BatchedToolCall{
		ID:       "2",
		Name:     "read_file",
		Args:     json.RawMessage(`{"path":"b.go"}`),
		Result:   "package b",
		ReadOnly: true,
	})

	// 刷新 — 应合并
	result := batcher.Flush()
	if result == "" {
		t.Fatal("flush should return merged result")
	}
	// 应包含两个工具的结果
	if !containsStr(result, "package a") {
		t.Fatal("merged result should contain first tool result")
	}
	if !containsStr(result, "package b") {
		t.Fatal("merged result should contain second tool result")
	}
	if !containsStr(result, "批量工具结果") {
		t.Fatal("merged result should have batch header")
	}

	// 非只读工具不应被批处理
	added := batcher.Add(BatchedToolCall{
		ID:       "3",
		Name:     "edit_file",
		Args:     json.RawMessage(`{}`),
		Result:   "edited",
		ReadOnly: false,
	})
	if added {
		t.Fatal("non-readOnly tool should not be added to batch")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStrHelper(s, substr))
}

func containsStrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
