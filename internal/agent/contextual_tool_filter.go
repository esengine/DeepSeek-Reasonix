package agent

import (
	"strings"
	"sync"
)

// ── OPT-50: ContextualToolFilter (上下文工具过滤) ──
// 根据用户查询的上下文动态过滤可用工具列表，减少不必要的工具描述 token，
// 从而降低每次 API 请求的 token 消耗。
//
// 原理：LLM 在每轮对话中需要接收所有可用工具的描述（schema），当工具数量
// 较多时（如 20+ 个），工具描述可能消耗数千 token。通过上下文检测：
// 1. 分析用户查询，判断当前任务类型（文件编辑、网页搜索、代码执行等）
// 2. 仅保留与当前上下文相关的工具 + 基础文件操作工具
// 3. 每个被过滤掉的工具约节省 200 token 的描述开销
//
// 效果：对于 20 个工具的场景，若上下文仅需 6 个工具，则每轮节省约 2800 token。

// ToolFilterStats 工具过滤统计信息
type ToolFilterStats struct {
	TotalFiltered    int            // 累计被过滤掉的工具数量
	TokensSaved      int            // 累计节省的 token 数量
	ContextsDetected map[string]int // 检测到的上下文类型及次数
}

// ContextualToolFilter 上下文工具过滤器
type ContextualToolFilter struct {
	mu              sync.RWMutex
	totalFiltered   int               // 累计被过滤掉的工具数量
	tokensSaved     int               // 累计节省的 token 数量
	contextKeywords map[string][]string // 上下文类型到相关工具名称的映射
	activeFilters   map[string]bool   // 当前活跃的上下文过滤器
}

// NewContextualToolFilter 创建一个新的上下文工具过滤器，包含默认的上下文-工具映射。
func NewContextualToolFilter() *ContextualToolFilter {
	return &ContextualToolFilter{
		contextKeywords: map[string][]string{
			"file_editing":    {"edit_file", "write_file", "read_file", "multi_edit", "grep", "glob"},
			"web_research":    {"web_search", "web_fetch"},
			"code_execution":  {"bash", "task"},
			"planning":        {"todo_write", "task"},
			"mcp_interaction": {"mcp", "connect_tool_source"},
		},
		activeFilters: make(map[string]bool),
	}
}

// DetectContext 分析用户查询，返回检测到的上下文类型。
// 如果无法识别上下文，返回空字符串。
func (c *ContextualToolFilter) DetectContext(query string) string {
	q := strings.ToLower(query)

	// 上下文检测关键词映射（按优先级排列）
	contexts := []struct {
		name     string
		keywords []string
	}{
		{"file_editing", []string{"file", "edit", "write", "code"}},
		{"web_research", []string{"search", "web", "internet", "look up"}},
		{"code_execution", []string{"run", "execute", "command", "bash"}},
		{"planning", []string{"plan", "todo", "organize", "track"}},
		{"mcp_interaction", []string{"mcp", "server", "connect"}},
	}

	for _, ctx := range contexts {
		for _, kw := range ctx.keywords {
			if strings.Contains(q, kw) {
				c.mu.Lock()
				c.activeFilters[ctx.name] = true
				c.mu.Unlock()
				return ctx.name
			}
		}
	}

	return ""
}

// FilterTools 根据检测到的上下文过滤工具列表。
// 如果 context 为空，则原样返回所有工具。
// 始终包含基础文件操作工具：read_file、grep、glob。
func (c *ContextualToolFilter) FilterTools(allTools []string, context string) []string {
	if context == "" {
		return allTools
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	relevantTools, ok := c.contextKeywords[context]
	if !ok {
		return allTools
	}

	// 构建允许的工具集合：上下文相关工具 + 始终包含的基础工具
	allowed := make(map[string]bool)
	for _, t := range relevantTools {
		allowed[t] = true
	}
	for _, t := range []string{"read_file", "grep", "glob"} {
		allowed[t] = true
	}

	// 过滤工具列表
	var filtered []string
	for _, tool := range allTools {
		if allowed[tool] {
			filtered = append(filtered, tool)
		}
	}

	// 更新统计信息
	filteredOut := len(allTools) - len(filtered)
	c.totalFiltered += filteredOut
	c.tokensSaved += filteredOut * 200
	c.activeFilters[context] = true

	return filtered
}

// GetStats 返回工具过滤的统计信息。
func (c *ContextualToolFilter) GetStats() ToolFilterStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	contextsDetected := make(map[string]int)
	for ctx, active := range c.activeFilters {
		if active {
			contextsDetected[ctx] = 1
		}
	}

	return ToolFilterStats{
		TotalFiltered:    c.totalFiltered,
		TokensSaved:      c.tokensSaved,
		ContextsDetected: contextsDetected,
	}
}

// Reset 重置所有统计信息和活跃过滤器。
func (c *ContextualToolFilter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalFiltered = 0
	c.tokensSaved = 0
	c.activeFilters = make(map[string]bool)
}
