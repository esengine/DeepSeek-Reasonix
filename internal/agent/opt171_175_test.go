package agent

import (
	"strings"
	"testing"
)

// ============================================================================
// OPT-171: TokenAwareBuffer — Token感知缓冲区
// ============================================================================

// TestTokenAwareBuffer_WriteReturnsTrueWhenFull 验证写入达到容量时返回 true 且 IsFull 为 true。
func TestTokenAwareBuffer_WriteReturnsTrueWhenFull(t *testing.T) {
	// capacity 以 token 计，tabEstimateTokens = len/4，故 40 字符 => 10 token
	buf := NewTokenAwareBuffer(10)
	content := strings.Repeat("a", 40) // 40 字符 => 10 token，恰好达到容量
	full := buf.Write(content)
	if !full {
		t.Errorf("expected Write to return true when capacity reached, got false")
	}
	if !buf.IsFull() {
		t.Errorf("expected IsFull to return true when at capacity, got false")
	}
}

// TestTokenAwareBuffer_WriteReturnsFalseWhenNotFull 验证未满时 Write 返回 false 且 IsFull 为 false。
func TestTokenAwareBuffer_WriteReturnsFalseWhenNotFull(t *testing.T) {
	buf := NewTokenAwareBuffer(100) // 容量 100 token
	content := "hello"              // 5 字符 => 1 token，远未达到容量
	full := buf.Write(content)
	if full {
		t.Errorf("expected Write to return false when not full, got true")
	}
	if buf.IsFull() {
		t.Errorf("expected IsFull to return false when not full, got true")
	}
}

// TestTokenAwareBuffer_FlushReturnsContentAndClears 验证 Flush 返回拼接内容并清空缓冲区。
func TestTokenAwareBuffer_FlushReturnsContentAndClears(t *testing.T) {
	buf := NewTokenAwareBuffer(100)
	buf.Write("hello")
	buf.Write("world")
	result := buf.Flush()
	if result != "helloworld" {
		t.Errorf("expected flushed content 'helloworld', got %q", result)
	}
	if buf.GetCurrentTokens() != 0 {
		t.Errorf("expected currentTokens 0 after flush, got %d", buf.GetCurrentTokens())
	}
	if buf.IsFull() {
		t.Errorf("expected IsFull false after flush, got true")
	}
}

// TestTokenAwareBuffer_GetCurrentTokens 验证 GetCurrentTokens 返回当前缓冲的 token 数。
func TestTokenAwareBuffer_GetCurrentTokens(t *testing.T) {
	buf := NewTokenAwareBuffer(100)
	content := "abcdefgh" // 8 字符 => 2 token
	buf.Write(content)
	if buf.GetCurrentTokens() != 2 {
		t.Errorf("expected currentTokens 2, got %d", buf.GetCurrentTokens())
	}
}

// TestTokenAwareBuffer_StatsVerifyCapacity 验证 GetStats 返回正确的 capacity。
func TestTokenAwareBuffer_StatsVerifyCapacity(t *testing.T) {
	buf := NewTokenAwareBuffer(50)
	stats := buf.GetStats()
	if stats["capacity"].(int) != 50 {
		t.Errorf("expected capacity 50, got %v", stats["capacity"])
	}
}

// TestTokenAwareBuffer_Reset 验证 Reset 清空状态但保留 capacity 配置。
func TestTokenAwareBuffer_Reset(t *testing.T) {
	buf := NewTokenAwareBuffer(100)
	buf.Write("some content here")
	buf.Flush()
	buf.Reset()
	if buf.GetCurrentTokens() != 0 {
		t.Errorf("expected currentTokens 0 after reset, got %d", buf.GetCurrentTokens())
	}
	if buf.IsFull() {
		t.Errorf("expected IsFull false after reset, got true")
	}
	stats := buf.GetStats()
	if stats["flushCount"].(int) != 0 {
		t.Errorf("expected flushCount 0 after reset, got %v", stats["flushCount"])
	}
	if stats["totalBuffered"].(int) != 0 {
		t.Errorf("expected totalBuffered 0 after reset, got %v", stats["totalBuffered"])
	}
	// capacity 应被保留
	if stats["capacity"].(int) != 100 {
		t.Errorf("expected capacity preserved as 100 after reset, got %v", stats["capacity"])
	}
}

// ============================================================================
// OPT-172: CacheVersionManager — 缓存版本管理器
// ============================================================================

// TestCacheVersionManager_BeginTransactionAndCommit 验证事务提交后版本递增。
func TestCacheVersionManager_BeginTransactionAndCommit(t *testing.T) {
	cvm := NewCacheVersionManager()
	key := "cache_key"
	v0 := cvm.BeginTransaction(key)
	if v0 != 0 {
		t.Errorf("expected initial version 0, got %d", v0)
	}
	v1 := cvm.Commit(key)
	if v1 != 1 {
		t.Errorf("expected version 1 after first commit, got %d", v1)
	}
	v2 := cvm.Commit(key)
	if v2 != 2 {
		t.Errorf("expected version 2 after second commit, got %d", v2)
	}
}

// TestCacheVersionManager_RollbackRestoresVersion 验证 Rollback 恢复（递减）版本号。
func TestCacheVersionManager_RollbackRestoresVersion(t *testing.T) {
	cvm := NewCacheVersionManager()
	key := "key1"
	cvm.Commit(key) // version 1
	cvm.Commit(key) // version 2
	cvm.Rollback(key)
	if got := cvm.GetVersion(key); got != 1 {
		t.Errorf("expected version 1 after rollback, got %d", got)
	}
}

// TestCacheVersionManager_GetVersion 验证 GetVersion 返回指定 key 的版本（不存在时为 0）。
func TestCacheVersionManager_GetVersion(t *testing.T) {
	cvm := NewCacheVersionManager()
	if got := cvm.GetVersion("nonexistent"); got != 0 {
		t.Errorf("expected version 0 for nonexistent key, got %d", got)
	}
	cvm.Commit("k")
	if got := cvm.GetVersion("k"); got != 1 {
		t.Errorf("expected version 1 after commit, got %d", got)
	}
}

// TestCacheVersionManager_StatsCommitsAndRollbacks 验证 GetStats 中 commits 与 rollbacks 计数。
func TestCacheVersionManager_StatsCommitsAndRollbacks(t *testing.T) {
	cvm := NewCacheVersionManager()
	cvm.Commit("a")
	cvm.Commit("b")
	cvm.Rollback("a")
	stats := cvm.GetStats()
	if stats["commits"].(int) != 2 {
		t.Errorf("expected commits 2, got %v", stats["commits"])
	}
	if stats["rollbacks"].(int) != 1 {
		t.Errorf("expected rollbacks 1, got %v", stats["rollbacks"])
	}
	// globalVersion 为 int64 类型，需用 .(int64) 断言避免 int/int64 类型不匹配
	if stats["globalVersion"].(int64) != 2 {
		t.Errorf("expected globalVersion 2, got %v", stats["globalVersion"])
	}
	if stats["trackedKeys"].(int) != 2 {
		t.Errorf("expected trackedKeys 2, got %v", stats["trackedKeys"])
	}
}

// TestCacheVersionManager_Reset 验证 Reset 将版本管理器恢复到初始状态。
func TestCacheVersionManager_Reset(t *testing.T) {
	cvm := NewCacheVersionManager()
	cvm.Commit("k")
	cvm.Rollback("k")
	cvm.Reset()
	stats := cvm.GetStats()
	if stats["commits"].(int) != 0 {
		t.Errorf("expected commits 0 after reset, got %v", stats["commits"])
	}
	if stats["rollbacks"].(int) != 0 {
		t.Errorf("expected rollbacks 0 after reset, got %v", stats["rollbacks"])
	}
	// globalVersion 为 int64 类型，需用 .(int64) 断言
	if stats["globalVersion"].(int64) != 0 {
		t.Errorf("expected globalVersion 0 after reset, got %v", stats["globalVersion"])
	}
	if got := cvm.GetVersion("k"); got != 0 {
		t.Errorf("expected version 0 after reset, got %d", got)
	}
}

// ============================================================================
// OPT-173: ContextOverflowHandler — 上下文溢出处理器
// ============================================================================

// TestContextOverflowHandler_HandleOverflowTrimsMessages 验证 HandleOverflow 修剪消息使结果变短。
func TestContextOverflowHandler_HandleOverflowTrimsMessages(t *testing.T) {
	// maxTokens=100，estimatedTokens=600，excess=500 => trim_oldest
	handler := NewContextOverflowHandler(100)
	msg := strings.Repeat("x", 400)                    // 每条 400 字符 => 100 token
	messages := []string{msg, msg, msg, msg, msg, msg} // 6 条 => 600 token
	result, strategy := handler.HandleOverflow(messages, 600)
	if strategy != "trim_oldest" {
		t.Errorf("expected strategy 'trim_oldest', got %q", strategy)
	}
	if len(result) >= len(messages) {
		t.Errorf("expected messages to be trimmed, result length %d not less than %d", len(result), len(messages))
	}
	if len(result) == 0 {
		t.Errorf("expected at least one message to remain after trim, got 0")
	}
}

// TestContextOverflowHandler_SelectStrategy 验证 SelectStrategy 根据超出量选择策略。
func TestContextOverflowHandler_SelectStrategy(t *testing.T) {
	handler := NewContextOverflowHandler(1000)
	if got := handler.SelectStrategy(50); got != "truncate" {
		t.Errorf("excess 50: expected 'truncate', got %q", got)
	}
	if got := handler.SelectStrategy(100); got != "summarize" {
		t.Errorf("excess 100: expected 'summarize', got %q", got)
	}
	if got := handler.SelectStrategy(499); got != "summarize" {
		t.Errorf("excess 499: expected 'summarize', got %q", got)
	}
	if got := handler.SelectStrategy(500); got != "trim_oldest" {
		t.Errorf("excess 500: expected 'trim_oldest', got %q", got)
	}
	if got := handler.SelectStrategy(1000); got != "trim_oldest" {
		t.Errorf("excess 1000: expected 'trim_oldest', got %q", got)
	}
}

// TestContextOverflowHandler_GetOverflowCount 验证 GetOverflowCount 返回已处理的溢出次数。
func TestContextOverflowHandler_GetOverflowCount(t *testing.T) {
	handler := NewContextOverflowHandler(50)
	messages := []string{strings.Repeat("a", 400), strings.Repeat("b", 400)}
	handler.HandleOverflow(messages, 600) // excess 550 => trim_oldest
	handler.HandleOverflow(messages, 600)
	if got := handler.GetOverflowCount(); got != 2 {
		t.Errorf("expected overflowCount 2, got %d", got)
	}
}

// TestContextOverflowHandler_StatsMaxTokens 验证 GetStats 返回正确的 maxTokens 与 strategyCount。
func TestContextOverflowHandler_StatsMaxTokens(t *testing.T) {
	handler := NewContextOverflowHandler(500)
	stats := handler.GetStats()
	if stats["maxTokens"].(int) != 500 {
		t.Errorf("expected maxTokens 500, got %v", stats["maxTokens"])
	}
	if stats["strategyCount"].(int) != 3 {
		t.Errorf("expected strategyCount 3, got %v", stats["strategyCount"])
	}
	if stats["overflowCount"].(int) != 0 {
		t.Errorf("expected overflowCount 0 initially, got %v", stats["overflowCount"])
	}
}

// TestContextOverflowHandler_Reset 验证 Reset 清空计数但保留 maxTokens 配置。
func TestContextOverflowHandler_Reset(t *testing.T) {
	handler := NewContextOverflowHandler(50)
	handler.HandleOverflow([]string{strings.Repeat("a", 400)}, 600)
	handler.Reset()
	if got := handler.GetOverflowCount(); got != 0 {
		t.Errorf("expected overflowCount 0 after reset, got %d", got)
	}
	stats := handler.GetStats()
	if stats["overflowCount"].(int) != 0 {
		t.Errorf("expected stats overflowCount 0 after reset, got %v", stats["overflowCount"])
	}
	if stats["totalTrimmedTokens"].(int) != 0 {
		t.Errorf("expected totalTrimmedTokens 0 after reset, got %v", stats["totalTrimmedTokens"])
	}
	// maxTokens 应被保留
	if stats["maxTokens"].(int) != 50 {
		t.Errorf("expected maxTokens preserved as 50 after reset, got %v", stats["maxTokens"])
	}
}

// TestContextOverflowHandler_NoOverflowWithinLimit 验证未超限时返回原消息且策略为空。
func TestContextOverflowHandler_NoOverflowWithinLimit(t *testing.T) {
	handler := NewContextOverflowHandler(1000)
	messages := []string{"hello", "world"}
	result, strategy := handler.HandleOverflow(messages, 500) // 500 <= 1000，无溢出
	if len(result) != len(messages) {
		t.Errorf("expected unchanged message count %d, got %d", len(messages), len(result))
	}
	if strategy != "" {
		t.Errorf("expected empty strategy when no overflow, got %q", strategy)
	}
	if got := handler.GetOverflowCount(); got != 0 {
		t.Errorf("expected overflowCount 0 when within limit, got %d", got)
	}
}

// ============================================================================
// OPT-174: TokenAwareCompressorV2 — Token感知压缩器V2
// ============================================================================

// TestTokenAwareCompressorV2_CompressWhitespace 验证空白压缩将连续空白合并为单个空格。
func TestTokenAwareCompressorV2_CompressWhitespace(t *testing.T) {
	c := NewTokenAwareCompressorV2()
	text := "hello     world\t\t\nfoo"
	compressed := c.CompressWithStrategy(text, "whitespace")
	expected := "hello world foo"
	if compressed != expected {
		t.Errorf("expected %q, got %q", expected, compressed)
	}
}

// TestTokenAwareCompressorV2_CompressRedundancy 验证重复词移除（连续重复词只保留一个）。
func TestTokenAwareCompressorV2_CompressRedundancy(t *testing.T) {
	c := NewTokenAwareCompressorV2()
	text := "hello hello world world test"
	compressed := c.CompressWithStrategy(text, "redundancy")
	expected := "hello world test"
	if compressed != expected {
		t.Errorf("expected %q, got %q", expected, compressed)
	}
}

// TestTokenAwareCompressorV2_CompressWithStrategy 验证单策略（缩写替换）。
func TestTokenAwareCompressorV2_CompressWithStrategy(t *testing.T) {
	c := NewTokenAwareCompressorV2()
	text := "for example this is a test by the way"
	compressed := c.CompressWithStrategy(text, "abbreviation")
	if !strings.Contains(compressed, "e.g.") {
		t.Errorf("expected abbreviation 'e.g.' in result, got %q", compressed)
	}
	if !strings.Contains(compressed, "BTW") {
		t.Errorf("expected abbreviation 'BTW' in result, got %q", compressed)
	}
}

// TestTokenAwareCompressorV2_CompressAllStrategies 验证 Compress 应用全部策略后文本变短。
func TestTokenAwareCompressorV2_CompressAllStrategies(t *testing.T) {
	c := NewTokenAwareCompressorV2()
	text := "hello     hello   world   for example test test"
	compressed := c.Compress(text)
	if len(compressed) >= len(text) {
		t.Errorf("expected compressed text shorter than original, got len %d >= %d", len(compressed), len(text))
	}
	if !strings.Contains(compressed, "hello") {
		t.Errorf("expected 'hello' to remain in compressed text, got %q", compressed)
	}
}

// TestTokenAwareCompressorV2_GetCompressionRatio 验证 GetCompressionRatio 返回压缩比率。
func TestTokenAwareCompressorV2_GetCompressionRatio(t *testing.T) {
	c := NewTokenAwareCompressorV2()
	original := "hello     world" // 14 字符 => 3 token
	compressed := "hello world"   // 11 字符 => 2 token
	ratio := c.GetCompressionRatio(original, compressed)
	expected := float64(len(compressed)/4) / float64(len(original)/4)
	if ratio != expected {
		t.Errorf("expected ratio %f, got %f", expected, ratio)
	}
	if ratio >= 1.0 {
		t.Errorf("expected ratio < 1.0 for compressed text, got %f", ratio)
	}
	if ratio <= 0.0 {
		t.Errorf("expected ratio > 0.0, got %f", ratio)
	}
}

// TestTokenAwareCompressorV2_StatsCompressCount 验证 GetStats 中 compressCount 计数。
func TestTokenAwareCompressorV2_StatsCompressCount(t *testing.T) {
	c := NewTokenAwareCompressorV2()
	c.Compress("hello     world")
	c.Compress("test test test")
	stats := c.GetStats()
	if stats["compressCount"].(int) != 2 {
		t.Errorf("expected compressCount 2, got %v", stats["compressCount"])
	}
	if stats["strategyCount"].(int) != 3 {
		t.Errorf("expected strategyCount 3, got %v", stats["strategyCount"])
	}
}

// TestTokenAwareCompressorV2_Reset 验证 Reset 清空统计但保留策略配置。
func TestTokenAwareCompressorV2_Reset(t *testing.T) {
	c := NewTokenAwareCompressorV2()
	c.Compress("hello     world")
	c.Reset()
	stats := c.GetStats()
	if stats["compressCount"].(int) != 0 {
		t.Errorf("expected compressCount 0 after reset, got %v", stats["compressCount"])
	}
	if stats["totalTokensSaved"].(int) != 0 {
		t.Errorf("expected totalTokensSaved 0 after reset, got %v", stats["totalTokensSaved"])
	}
	// avgCompressionRatio 为 float64 类型，用 .(float64) 断言
	if stats["avgCompressionRatio"].(float64) != 0.0 {
		t.Errorf("expected avgCompressionRatio 0.0 after reset, got %v", stats["avgCompressionRatio"])
	}
	// strategies 应被保留
	if stats["strategyCount"].(int) != 3 {
		t.Errorf("expected strategyCount preserved as 3 after reset, got %v", stats["strategyCount"])
	}
}

// ============================================================================
// OPT-175: PromptTokenCalculator — 提示Token计算器
// ============================================================================

// TestPromptTokenCalculator_CalculateRecordsAndReturns 验证 Calculate 记录并返回 token 数。
func TestPromptTokenCalculator_CalculateRecordsAndReturns(t *testing.T) {
	calc := NewPromptTokenCalculator()
	content := "abcdefgh" // 8 字符 => 2 token
	count := calc.Calculate("p1", content)
	if count != 2 {
		t.Errorf("expected token count 2, got %d", count)
	}
	stats := calc.GetStats()
	if stats["totalCalculated"].(int) != 1 {
		t.Errorf("expected totalCalculated 1, got %v", stats["totalCalculated"])
	}
	if stats["totalTokens"].(int) != 2 {
		t.Errorf("expected totalTokens 2, got %v", stats["totalTokens"])
	}
}

// TestPromptTokenCalculator_EstimateNoRecord 验证 Estimate 仅估算不记录。
func TestPromptTokenCalculator_EstimateNoRecord(t *testing.T) {
	calc := NewPromptTokenCalculator()
	content := "abcdefgh" // 2 token
	est := calc.Estimate(content)
	if est != 2 {
		t.Errorf("expected estimate 2, got %d", est)
	}
	// 验证未被记录
	stats := calc.GetStats()
	if stats["totalCalculated"].(int) != 0 {
		t.Errorf("expected totalCalculated 0 after Estimate, got %v", stats["totalCalculated"])
	}
	if stats["totalTokens"].(int) != 0 {
		t.Errorf("expected totalTokens 0 after Estimate, got %v", stats["totalTokens"])
	}
}

// TestPromptTokenCalculator_GetTotalTokens 验证 GetTotalTokens 返回累计 token 总数。
func TestPromptTokenCalculator_GetTotalTokens(t *testing.T) {
	calc := NewPromptTokenCalculator()
	calc.Calculate("p1", "abcdefgh")         // 8 字符 => 2 token
	calc.Calculate("p2", "abcdefghijklmnop") // 16 字符 => 4 token
	if got := calc.GetTotalTokens(); got != 6 {
		t.Errorf("expected totalTokens 6, got %d", got)
	}
}

// TestPromptTokenCalculator_GetAvgTokens 验证 GetAvgTokens 返回平均 token 数。
func TestPromptTokenCalculator_GetAvgTokens(t *testing.T) {
	calc := NewPromptTokenCalculator()
	calc.Calculate("p1", "abcdefgh")         // 2 token
	calc.Calculate("p2", "abcdefghijklmnop") // 4 token
	if avg := calc.GetAvgTokens(); avg != 3.0 {
		t.Errorf("expected avgTokens 3.0, got %f", avg)
	}
	// 空计算器应返回 0
	empty := NewPromptTokenCalculator()
	if avg := empty.GetAvgTokens(); avg != 0.0 {
		t.Errorf("expected avgTokens 0.0 for empty calculator, got %f", avg)
	}
}

// TestPromptTokenCalculator_StatsTotalCalculated 验证 GetStats 中 totalCalculated 及 min/max 统计。
func TestPromptTokenCalculator_StatsTotalCalculated(t *testing.T) {
	calc := NewPromptTokenCalculator()
	calc.Calculate("p1", "aaaa")             // 4 字符 => 1 token
	calc.Calculate("p2", "aaaaaaaa")         // 8 字符 => 2 token
	calc.Calculate("p3", "aaaaaaaaaaaaaaaa") // 16 字符 => 4 token
	stats := calc.GetStats()
	if stats["totalCalculated"].(int) != 3 {
		t.Errorf("expected totalCalculated 3, got %v", stats["totalCalculated"])
	}
	if stats["totalTokens"].(int) != 7 {
		t.Errorf("expected totalTokens 7, got %v", stats["totalTokens"])
	}
	if stats["maxTokensSeen"].(int) != 4 {
		t.Errorf("expected maxTokensSeen 4, got %v", stats["maxTokensSeen"])
	}
	if stats["minTokensSeen"].(int) != 1 {
		t.Errorf("expected minTokensSeen 1, got %v", stats["minTokensSeen"])
	}
}

// TestPromptTokenCalculator_Reset 验证 Reset 将计算器恢复到初始状态。
func TestPromptTokenCalculator_Reset(t *testing.T) {
	calc := NewPromptTokenCalculator()
	calc.Calculate("p1", "abcdefgh")
	calc.Reset()
	stats := calc.GetStats()
	if stats["totalCalculated"].(int) != 0 {
		t.Errorf("expected totalCalculated 0 after reset, got %v", stats["totalCalculated"])
	}
	if stats["totalTokens"].(int) != 0 {
		t.Errorf("expected totalTokens 0 after reset, got %v", stats["totalTokens"])
	}
	if stats["maxTokensSeen"].(int) != 0 {
		t.Errorf("expected maxTokensSeen 0 after reset, got %v", stats["maxTokensSeen"])
	}
	if stats["minTokensSeen"].(int) != 0 {
		t.Errorf("expected minTokensSeen 0 after reset, got %v", stats["minTokensSeen"])
	}
	if got := calc.GetTotalTokens(); got != 0 {
		t.Errorf("expected GetTotalTokens 0 after reset, got %d", got)
	}
	if avg := calc.GetAvgTokens(); avg != 0.0 {
		t.Errorf("expected GetAvgTokens 0.0 after reset, got %f", avg)
	}
}
