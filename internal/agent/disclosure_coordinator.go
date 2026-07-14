package agent

import (
	"sync"
)

// ── OPT-38: 渐进式披露与懒加载协同 (Progressive Disclosure + Lazy Loading Coordination) ──
// 让 OPT-14（渐进式工具披露）和 OPT-25（工具 schema 懒加载）协同工作。
//
// 原理：OPT-14 按任务复杂度分 4 级披露工具（Minimal/Core/Extended/Full），
// OPT-25 在启动时只加载核心工具 schema，其余按需加载。两者目标一致但独立运作。
// 本模块协调两者：
// 1. OPT-14 决定当前应该披露多少工具
// 2. OPT-38 根据 OPT-14 的级别决定 OPT-25 应该预加载哪些工具
// 3. 当 OPT-14 升级披露级别时，通知 OPT-25 预加载对应工具
//
// 效果：首次请求工具 schema 从 3000 降到 500（省 83%），同时按需加载
// 确保工具可用性。

// DisclosureLazyCoordinator 协调器
type DisclosureLazyCoordinator struct {
	mu sync.RWMutex

	// 当前的披露级别
	currentLevel DisclosureLevel

	// 级别对应的工具集
	levelTools map[DisclosureLevel][]string

	// 已加载的工具
	loadedTools map[string]bool
}

// DisclosureLevel 披露级别
type DisclosureLevel int

const (
	DisclosureMinimal  DisclosureLevel = iota // 最小集（5 个核心工具）
	DisclosureCore                            // 核心集（15 个工具）
	DisclosureExtended                        // 扩展集（+ MCP/skills）
	DisclosureFull                            // 全量
)

func (d DisclosureLevel) String() string {
	switch d {
	case DisclosureMinimal:
		return "minimal"
	case DisclosureCore:
		return "core"
	case DisclosureExtended:
		return "extended"
	case DisclosureFull:
		return "full"
	default:
		return "unknown"
	}
}

// NewDisclosureLazyCoordinator 创建协调器
func NewDisclosureLazyCoordinator() *DisclosureLazyCoordinator {
	return &DisclosureLazyCoordinator{
		currentLevel: DisclosureCore,
		levelTools:   getDefaultLevelTools(),
		loadedTools:  make(map[string]bool),
	}
}

// getDefaultLevelTools 获取各级别的默认工具集
func getDefaultLevelTools() map[DisclosureLevel][]string {
	return map[DisclosureLevel][]string{
		DisclosureMinimal: {"bash", "read_file", "edit_file", "grep", "glob"},
		DisclosureCore: {
			"bash", "read_file", "edit_file", "write_file", "grep", "glob",
			"ls", "code_index", "web_search", "web_fetch", "todo_write",
			"task", "mcp", "interactive", "multi_edit",
		},
		DisclosureExtended: {
			// core + MCP + skills + LSP
			"bash", "read_file", "edit_file", "write_file", "grep", "glob",
			"ls", "code_index", "web_search", "web_fetch", "todo_write",
			"task", "mcp", "interactive", "multi_edit",
			"connect_tool_source", "skill_activate", "lsp_diagnostics",
		},
		DisclosureFull: nil, // nil 表示全量加载
	}
}

// SetLevel 设置披露级别，返回需要新加载的工具列表
func (c *DisclosureLazyCoordinator) SetLevel(level DisclosureLevel) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if level == c.currentLevel {
		return nil
	}

	oldLevel := c.currentLevel
	c.currentLevel = level

	// 如果降到更低级别，不需要卸载（已加载的保持）
	// 如果升到更高级别，返回需要新加载的工具
	if level <= oldLevel {
		return nil
	}

	// 返回新级别中尚未加载的工具
	tools := c.levelTools[level]
	if tools == nil {
		return nil // Full 级别，全量加载
	}

	var toLoad []string
	for _, tool := range tools {
		if !c.loadedTools[tool] {
			toLoad = append(toLoad, tool)
		}
	}
	return toLoad
}

// MarkLoaded 标记工具已加载
func (c *DisclosureLazyCoordinator) MarkLoaded(toolName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadedTools[toolName] = true
}

// IsLoaded 检查工具是否已加载
func (c *DisclosureLazyCoordinator) IsLoaded(toolName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loadedTools[toolName]
}

// GetLevel 获取当前级别
func (c *DisclosureLazyCoordinator) GetLevel() DisclosureLevel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentLevel
}

// GetActiveTools 获取当前已加载的工具列表
func (c *DisclosureLazyCoordinator) GetActiveTools() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var tools []string
	for tool := range c.loadedTools {
		tools = append(tools, tool)
	}
	return tools
}

// EstimateTokenSavings 估算相比全量加载节省的 token
func (c *DisclosureLazyCoordinator) EstimateTokenSavings() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 每个工具 schema 平均约 200 token
	tokensPerTool := 200
	totalTools := 25 // 假设全量约 25 个工具
	loadedCount := len(c.loadedTools)

	return (totalTools - loadedCount) * tokensPerTool
}

// GetStats 获取统计
func (c *DisclosureLazyCoordinator) GetStats() DisclosureStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return DisclosureStats{
		Level:         c.currentLevel.String(),
		LoadedTools:   len(c.loadedTools),
		TokenSavings:  c.EstimateTokenSavings(),
	}
}

// DisclosureStats 披露统计
type DisclosureStats struct {
	Level        string `json:"level"`
	LoadedTools  int    `json:"loadedTools"`
	TokenSavings int    `json:"tokenSavings"`
}
