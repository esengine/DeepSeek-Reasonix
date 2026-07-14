package agent

import (
	"strings"
	"sync"
)

// ── OPT-93: ToolResultSummarizer (工具结果摘要器) ──
// 对冗长的工具调用结果进行摘要，以降低 token 消耗。
//
// 原理：工具（如 bash、read_file）的输出经常非常长，直接放入
// 上下文会浪费大量 token。ToolResultSummarizer 采用工具感知的
// 摘要策略：
//   - bash 输出：保留首尾各 5 行 + 总行数，并始终保留错误行
//   - grep 输出：通常较短，原样保留全部匹配
//   - 其他工具：截断至 maxTokens*4 字符，并保留被截断部分中的错误行
//   - 所有策略始终保留错误行（含 error/panic/fatal 等关键词）
//
// 效果：长工具输出 token 显著减少，同时保留对模型决策最关键的
// 错误信息与首尾上下文。

// ToolSummarizerStats 工具结果摘要器统计信息
type ToolSummarizerStats struct {
	TotalSummarized int            // 摘要总次数
	TokensSaved     int            // 累计节省 token 数
	ByTool          map[string]int // 按工具名统计的摘要次数
}

// ToolResultSummarizer 工具结果摘要器
// 根据工具类型对冗长输出进行摘要，保留关键信息以减少 token 消耗。
type ToolResultSummarizer struct {
	mu              sync.RWMutex
	totalSummarized int
	tokensSaved     int
	byTool          map[string]int
}

// NewToolResultSummarizer 创建工具结果摘要器
func NewToolResultSummarizer() *ToolResultSummarizer {
	return &ToolResultSummarizer{
		byTool: make(map[string]int),
	}
}

// SummarizeResult 对工具结果进行摘要。
//
// 摘要策略（工具感知）：
//   - "bash"：保留首尾各 5 行并附总行数，始终保留错误行
//   - "grep"：通常较短，原样保留全部匹配
//   - 其他：截断至 maxTokens*4 字符，保留被截断部分中的错误行
//
// maxTokens 为目标 token 上限（按 ~4 字符/token 估算）。
// 方法会更新摘要统计（次数、节省 token、按工具计数）。
func (s *ToolResultSummarizer) SummarizeResult(toolName string, result string, maxTokens int) string {
	originalTokens := len(result) / 4

	var summarized string
	switch toolName {
	case "bash":
		summarized = trsSummarizeBash(result)
	case "grep":
		// grep 结果通常为简短匹配行，原样保留
		summarized = result
	default:
		summarized = trsSummarizeGeneric(result, maxTokens)
	}

	summarizedTokens := len(summarized) / 4
	saved := originalTokens - summarizedTokens
	if saved < 0 {
		saved = 0
	}

	s.mu.Lock()
	s.totalSummarized++
	s.tokensSaved += saved
	s.byTool[toolName]++
	s.mu.Unlock()

	return summarized
}

// ShouldSummarize 判断工具结果是否值得摘要。
// 当结果长度（字节数）超过 threshold*4 时返回 true。
func (s *ToolResultSummarizer) ShouldSummarize(toolName string, result string, threshold int) bool {
	return len(result) > threshold*4
}

// GetStats 返回摘要器统计信息
func (s *ToolResultSummarizer) GetStats() ToolSummarizerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byToolCopy := make(map[string]int, len(s.byTool))
	for k, v := range s.byTool {
		byToolCopy[k] = v
	}

	return ToolSummarizerStats{
		TotalSummarized: s.totalSummarized,
		TokensSaved:     s.tokensSaved,
		ByTool:          byToolCopy,
	}
}

// Reset 重置摘要器，清除所有统计
func (s *ToolResultSummarizer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalSummarized = 0
	s.tokensSaved = 0
	s.byTool = make(map[string]int)
}

// ---------------------------------------------------------------------------
// 内部辅助函数（以 trs 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// trsBashHeadLines / trsBashTailLines 为 bash 摘要保留的首尾行数。
const (
	trsBashHeadLines = 5
	trsBashTailLines = 5
)

// trsIsErrorLine 判断一行是否为错误行（含 error/panic/fatal 等关键词）。
// 复用包内已有的 errorKeywords 与 containsAny。
func trsIsErrorLine(line string) bool {
	return containsAny(line, errorKeywords...)
}

// trsSummarizeBash 对 bash 输出进行摘要：保留首尾各 5 行，始终保留
// 错误行，并在末尾附上总行数。短输出（<=10 行）原样返回。
func trsSummarizeBash(result string) string {
	lines := strings.Split(result, "\n")
	totalLines := len(lines)

	if totalLines <= trsBashHeadLines+trsBashTailLines {
		return result
	}

	kept := make([]bool, totalLines)

	// 保留首部行
	for i := 0; i < trsBashHeadLines && i < totalLines; i++ {
		kept[i] = true
	}
	// 保留尾部行
	for i := totalLines - trsBashTailLines; i < totalLines; i++ {
		if i >= 0 {
			kept[i] = true
		}
	}
	// 始终保留错误行
	for i := 0; i < totalLines; i++ {
		if !kept[i] && trsIsErrorLine(lines[i]) {
			kept[i] = true
		}
	}

	var out []string
	skipped := 0
	for i := 0; i < totalLines; i++ {
		if kept[i] {
			if skipped > 0 {
				out = append(out, "[... "+intToString(skipped)+" lines omitted ...]")
				skipped = 0
			}
			out = append(out, lines[i])
		} else {
			skipped++
		}
	}
	if skipped > 0 {
		out = append(out, "[... "+intToString(skipped)+" lines omitted ...]")
	}

	out = append(out, "[total: "+intToString(totalLines)+" lines]")
	return strings.Join(out, "\n")
}

// trsSummarizeGeneric 对通用工具输出进行截断摘要：截断至 maxTokens*4
// 字符（在最近的换行处截断以避免拆分行），并保留被截断部分中的错误行。
// 若 maxTokens<=0 或结果未超限，则原样返回。
func trsSummarizeGeneric(result string, maxTokens int) string {
	if maxTokens <= 0 {
		return result
	}
	maxChars := maxTokens * 4
	if len(result) <= maxChars {
		return result
	}

	// 在 maxChars 范围内最近的换行处截断，避免拆分行
	cut := maxChars
	if idx := strings.LastIndex(result[:maxChars], "\n"); idx >= 0 {
		cut = idx
	}
	head := result[:cut]

	// 从被截断的尾部提取错误行予以保留
	tail := result[cut:]
	var errorLines []string
	for _, line := range strings.Split(tail, "\n") {
		if trsIsErrorLine(line) {
			errorLines = append(errorLines, line)
		}
	}

	var sb strings.Builder
	sb.WriteString(head)
	sb.WriteString("\n[... truncated ...]")
	if len(errorLines) > 0 {
		sb.WriteString("\n[preserved error lines:]\n")
		sb.WriteString(strings.Join(errorLines, "\n"))
	}
	return sb.String()
}
