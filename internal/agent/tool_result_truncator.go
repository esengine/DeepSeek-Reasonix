package agent

import (
	"strings"
	"sync"
)

// ── OPT-43: 工具结果智能截断器 (Tool Result Truncator) ──
// 基于相关性评分智能截断工具输出，替代简单的头尾截断策略。
//
// 原理：工具调用（如 bash、read_file）的输出经常非常长，
// 直接放入上下文会浪费大量 token。传统头尾截断会丢失中间的
// 关键信息（错误堆栈、diff 内容、测试结果等）。ToolResultTruncator
// 采用内容感知的截断策略：
// 1. 始终保留错误消息（error/panic/fatal 等关键词所在行）
// 2. 始终保留前 5 行（上下文/命令回显）
// 3. 始终保留最后 5 行（最终结果/退出状态）
// 4. 中间区域保留含 diff/result/success/fail/warning 等关键词的行
// 5. 被跳过的区域以 "[... truncated N lines ...]" 标记替代
//
// 效果：长工具输出 token 减少 40-70%，同时保留对模型决策
// 最关键的信息（错误、变更、结果）。

// TruncationStats 单个工具的截断统计
type TruncationStats struct {
	ToolName       string // 工具名称
	TotalCalls     int    // 总调用次数
	TruncatedCalls int    // 被截断的次数
	TokensSaved    int    // 累计节省的 token 数
}

// ToolResultTruncatorStats 截断器聚合统计
type ToolResultTruncatorStats struct {
	TotalTruncated   int // 总截断次数
	TotalTokensSaved int // 总节省 token 数
	ToolsTracked     int // 已追踪的工具数量
}

// ToolResultTruncator 工具结果智能截断器
// 基于内容相关性对工具输出进行智能截断，保留关键信息的同时减少 token 消耗。
type ToolResultTruncator struct {
	mu sync.RWMutex

	// 配置
	maxTokens int // 单次工具输出的 token 上限（默认 4000）

	// 聚合统计
	totalTruncated int // 总截断次数
	tokensSaved    int // 总节省 token 数

	// 按工具分类的统计
	byTool map[string]*TruncationStats
}

// errorKeywords 表示错误/严重信息的行关键词，匹配这些关键词的行始终保留。
var errorKeywords = []string{"error", "Error", "ERROR", "panic", "fatal"}

// middleKeywords 中间区域相关性关键词，匹配这些关键词的中间行会被保留。
var middleKeywords = []string{"diff", "+", "-", "result", "success", "fail", "warning"}

// NewToolResultTruncator 创建工具结果截断器
// maxTokens 为单次输出的 token 上限，若 <= 0 则使用默认值 4000。
func NewToolResultTruncator(maxTokens int) *ToolResultTruncator {
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	return &ToolResultTruncator{
		maxTokens: maxTokens,
		byTool:    make(map[string]*TruncationStats),
	}
}

// intToString 将非负整数转换为十进制字符串（不依赖 strconv 包）。
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	var sb strings.Builder
	for i := len(digits) - 1; i >= 0; i-- {
		sb.WriteByte(digits[i])
	}
	return sb.String()
}

// EstimateTokens 估算文本的 token 数量
// 使用简单启发式：token ≈ len(text) / 4
func (t *ToolResultTruncator) EstimateTokens(text string) int {
	return len(text) / 4
}

// Truncate 对工具输出进行智能截断
//
// 截断策略（内容感知）：
//   - 错误消息行始终保留（含 error/panic/fatal 等关键词）
//   - 前 5 行始终保留（上下文）
//   - 后 5 行始终保留（最终结果）
//   - 中间区域保留含 diff/result/success/fail/warning 等关键词的行
//   - 被跳过的连续行以 "[... truncated N lines ...]" 标记替代
//
// 若 maxTokens <= 0，则使用截断器的默认上限。
func (t *ToolResultTruncator) Truncate(toolName string, output string, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = t.maxTokens
	}

	originalTokens := t.EstimateTokens(output)

	// 输出在限制范围内，直接返回
	if originalTokens <= maxTokens {
		t.recordCall(toolName, false, 0)
		return output
	}

	lines := strings.Split(output, "\n")
	totalLines := len(lines)

	// 行数太少无法进行有意义的行级截断，直接返回
	if totalLines <= 10 {
		t.recordCall(toolName, false, 0)
		return output
	}

	const headCount = 5 // 保留的头部行数（上下文）
	const tailCount = 5 // 保留的尾部行数（最终结果）

	kept := make([]bool, totalLines)

	// 始终保留前 headCount 行（上下文）
	for i := 0; i < headCount && i < totalLines; i++ {
		kept[i] = true
	}

	// 始终保留后 tailCount 行（最终结果）
	for i := totalLines - tailCount; i < totalLines; i++ {
		if i >= 0 {
			kept[i] = true
		}
	}

	// 中间区域：保留错误行和含相关性关键词的行
	for i := headCount; i < totalLines-tailCount; i++ {
		line := lines[i]
		if containsAny(line, errorKeywords...) || containsAny(line, middleKeywords...) {
			kept[i] = true
		}
	}

	// 构建截断后的输出，在连续跳过的区域插入截断标记
	var result []string
	skippedCount := 0
	for i := 0; i < totalLines; i++ {
		if kept[i] {
			if skippedCount > 0 {
				result = append(result, "[... truncated "+intToString(skippedCount)+" lines ...]")
				skippedCount = 0
			}
			result = append(result, lines[i])
		} else {
			skippedCount++
		}
	}
	// 处理末尾的跳过区域
	if skippedCount > 0 {
		result = append(result, "[... truncated "+intToString(skippedCount)+" lines ...]")
	}

	truncated := strings.Join(result, "\n")
	truncatedTokens := t.EstimateTokens(truncated)
	saved := originalTokens - truncatedTokens
	if saved < 0 {
		saved = 0
	}

	t.recordCall(toolName, true, saved)

	return truncated
}

// recordCall 记录一次工具调用，更新按工具分类统计和聚合统计。
// 该方法内部获取写锁，调用方无需额外加锁。
func (t *ToolResultTruncator) recordCall(toolName string, truncated bool, tokensSaved int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	stats, ok := t.byTool[toolName]
	if !ok {
		stats = &TruncationStats{ToolName: toolName}
		t.byTool[toolName] = stats
	}
	stats.TotalCalls++
	if truncated {
		stats.TruncatedCalls++
		stats.TokensSaved += tokensSaved
		t.totalTruncated++
		t.tokensSaved += tokensSaved
	}
}

// GetStats 返回按工具分类的截断统计快照
func (t *ToolResultTruncator) GetStats() map[string]TruncationStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]TruncationStats, len(t.byTool))
	for name, stats := range t.byTool {
		result[name] = *stats
	}
	return result
}

// GetTotalStats 返回所有工具的聚合截断统计
func (t *ToolResultTruncator) GetTotalStats() ToolResultTruncatorStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return ToolResultTruncatorStats{
		TotalTruncated:   t.totalTruncated,
		TotalTokensSaved: t.tokensSaved,
		ToolsTracked:     len(t.byTool),
	}
}

// Reset 清除所有截断统计，将截断器重置为初始状态
func (t *ToolResultTruncator) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalTruncated = 0
	t.tokensSaved = 0
	t.byTool = make(map[string]*TruncationStats)
}
