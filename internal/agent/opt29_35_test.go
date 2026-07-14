package agent

import (
	"testing"
)

// TestPromptCompressor 测试提示压缩引擎
func TestPromptCompressor(t *testing.T) {
	compressor := NewPromptCompressor(CompressMedium)

	original := "This  is   a   test\n\n\n\nwith   extra   whitespace\n\n  and  indentation  "
	compressed := compressor.Compress(original)

	if len(compressed) >= len(original) {
		t.Fatalf("compressed should be shorter: %d vs %d", len(compressed), len(original))
	}
	// 不应包含连续多个空格
	if containsStr(compressed, "  ") {
		t.Fatal("compressed should not have consecutive spaces")
	}
}

// TestPromptCompressorAggressive 测试激进压缩
func TestPromptCompressorAggressive(t *testing.T) {
	compressor := NewPromptCompressor(CompressAggressive)

	original := "This is **bold** and *italic* text with <!-- comment --> and.... multiple... punctuation!!!"
	compressed := compressor.Compress(original)

	// 应移除注释
	if containsStr(compressed, "<!--") {
		t.Fatal("should remove HTML comments")
	}
	// 应移除加粗标记
	if containsStr(compressed, "**") {
		t.Fatal("should remove bold markers")
	}
}

// TestToolDescriptionRotator 测试工具描述轮换
func TestToolDescriptionRotator(t *testing.T) {
	rotator := NewToolDescriptionRotator()
	rotator.Register("bash", "Execute a bash command with full description and examples and notes")

	// 首次应返回完整描述
	desc := rotator.GetDescription("bash")
	if !containsStr(desc, "full description") {
		t.Fatal("first use should return full description")
	}

	// 记录使用
	rotator.RecordUsage("bash")

	// 第二次应返回精简描述
	desc2 := rotator.GetDescription("bash")
	if containsStr(desc2, "full description") {
		t.Fatal("after first use should return compact description")
	}
	if len(desc2) >= len(desc) {
		t.Fatalf("compact desc should be shorter: %d vs %d", len(desc2), len(desc))
	}
}

// TestToolDescriptionRotatorErrorRecovery 测试错误恢复
func TestToolDescriptionRotatorErrorRecovery(t *testing.T) {
	rotator := NewToolDescriptionRotator()
	rotator.Register("bash", "full description with details")
	rotator.RecordUsage("bash") // 轮换为精简

	// 两次错误后恢复完整描述
	rotator.RecordError("bash")
	rotator.RecordError("bash")

	desc := rotator.GetDescription("bash")
	if !containsStr(desc, "full description") {
		t.Fatal("should restore full description after errors")
	}
}

// TestSummaryCache 测试摘要缓存
func TestSummaryCache(t *testing.T) {
	cache := NewSummaryCache(10)

	messages := []string{"message1", "message2", "message3"}

	// 第一次 — 未命中
	_, hit := cache.Get(messages)
	if hit {
		t.Fatal("first get should miss")
	}

	// 存储摘要
	cache.Put(messages, "summary of messages")

	// 第二次 — 命中
	entry, hit2 := cache.Get(messages)
	if !hit2 {
		t.Fatal("second get should hit")
	}
	if entry.Summary != "summary of messages" {
		t.Fatalf("expected summary text, got %s", entry.Summary)
	}
}

// TestModelRouter 测试多模型路由
func TestModelRouter(t *testing.T) {
	router := NewModelRouter("claude-sonnet", "deepseek-chat", "deepseek-reasoner")

	// 简单任务 → 经济模型
	decision := router.Route("hello", "greeting", 1)
	if decision.TargetModel != "deepseek-chat" {
		t.Fatalf("simple task should route to economy model, got %s", decision.TargetModel)
	}
	if decision.SavedCost <= 0 {
		t.Fatal("should estimate cost savings")
	}

	// 复杂任务 → 主模型
	decision2 := router.Route("refactor the entire authentication system", "coding", 8)
	if decision2.TargetModel != "claude-sonnet" {
		t.Fatalf("complex task should route to primary model, got %s", decision2.TargetModel)
	}

	// 中等任务 → 标准模型（需要足够长且含复杂关键词的输入）
	decision3 := router.Route("fix this bug in the authentication module that causes session timeout issues during heavy load", "coding", 5)
	if decision3.TargetModel != "deepseek-reasoner" {
		t.Fatalf("medium task should route to standard model, got %s", decision3.TargetModel)
	}
}

// TestImageOptimizer 测试图片优化器
func TestImageOptimizer(t *testing.T) {
	opt := NewImageOptimizer()

	// 小 PNG 图不需要优化
	result := opt.OptimizeImage(256, 256, "png")
	if result.Action != "unchanged" {
		t.Fatalf("small image should not need optimization, got %s", result.Action)
	}

	// 大图需要优化
	result2 := opt.OptimizeImage(2048, 2048, "png")
	if result2.Action == "unchanged" {
		t.Fatal("large image should need optimization")
	}
	if result2.SavedTokens <= 0 {
		t.Fatal("should save tokens for large image")
	}

	// 验证 token 估算
	originalTokens := EstimateImageTokens(2048, 2048)
	if originalTokens <= 0 {
		t.Fatal("token estimate should be positive")
	}
}

func containsStrOpt29(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
