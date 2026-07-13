package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ── OPT-14: 渐进式工具披露 (Progressive Tool Disclosure) ──
// 在经济模式下，根据任务复杂度动态调整工具表面 (tool surface)。
//
// 原理：工具 schema 在 prompt 前缀中占用大量 token（15 个核心工具约 3000 token，
// 全部工具可达 15000+ token）。通过渐进式披露：
// - 初始只暴露最小核心集（5 个工具 ~1000 token）
// - 模型通过 need_tool 请求更多工具
// - 已使用的工具在会话内保持激活
// - 根据场景自动推荐可能需要的工具
//
// 效果：首次请求工具 schema token 从 3000 降到 1000（省 67%），
// 后续请求因前缀更短，缓存命中率更高。

// ToolDisclosureLevel 工具披露级别
type ToolDisclosureLevel int

const (
	// DisclosureMinimal — 最小集（5 个最常用工具）
	DisclosureMinimal ToolDisclosureLevel = iota
	// DisclosureCore — 核心集（15 个内置工具，当前 economy 模式默认）
	DisclosureCore
	// DisclosureExtended — 扩展集（核心 + 已按需激活的工具）
	DisclosureExtended
	// DisclosureFull — 全量（所有可用工具）
	DisclosureFull
)

func (l ToolDisclosureLevel) String() string {
	switch l {
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

// minimalToolSet 最小工具集（5 个最高频工具，约 1000 token）
var minimalToolSet = []string{
	"bash",
	"read_file",
	"edit_file",
	"grep",
	"glob",
}

// ProgressiveToolManager 渐进式工具管理器
type ProgressiveToolManager struct {
	mu sync.RWMutex

	// 当前披露级别
	level ToolDisclosureLevel

	// 已激活的工具（会话内累积，不会移除）
	activatedTools map[string]bool

	// 工具使用计数（用于自动推荐）
	toolUsage map[string]int

	// 上次工具激活时间（用于冷却控制）
	lastActivate time.Time

	// 冷却时间（避免频繁切换导致缓存失效）
	cooldown time.Duration

	// 场景到工具的推荐映射
	sceneRecommendations map[string][]string
}

// NewProgressiveToolManager 创建渐进式工具管理器
func NewProgressiveToolManager() *ProgressiveToolManager {
	return &ProgressiveToolManager{
		level:                DisclosureCore, // 默认从核心集开始
		activatedTools:       make(map[string]bool),
		toolUsage:            make(map[string]int),
		cooldown:             30 * time.Second,
		sceneRecommendations: initSceneRecommendations(),
	}
}

// initSceneRecommendations 场景到工具的推荐映射
func initSceneRecommendations() map[string][]string {
	return map[string][]string{
		// 编码场景：需要文件操作和代码搜索
		"coding": {"write_file", "multi_edit", "code_index", "ls"},
		// 调试场景：需要执行和输出检查
		"debugging": {"bash_output", "kill_shell", "wait"},
		// 重构场景：需要批量编辑和代码索引
		"refactoring": {"multi_edit", "move_file", "code_index"},
		// 搜索场景：需要 glob 和 grep
		"searching": {"ls", "code_index"},
		// 规划场景：需要 todo 和步骤管理
		"planning": {"todo_write", "complete_step"},
	}
}

// GetActiveTools 返回当前应激活的工具列表
func (m *ProgressiveToolManager) GetActiveTools() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch m.level {
	case DisclosureFull:
		// 全量模式：返回 nil 表示不过滤
		return nil

	case DisclosureMinimal:
		// 最小模式：只返回最小集 + 已激活的工具
		result := make([]string, 0, len(minimalToolSet)+len(m.activatedTools))
		seen := make(map[string]bool)
		for _, t := range minimalToolSet {
			result = append(result, t)
			seen[t] = true
		}
		for t := range m.activatedTools {
			if !seen[t] {
				result = append(result, t)
				seen[t] = true
			}
		}
		return result

	case DisclosureCore, DisclosureExtended:
		// 核心/扩展模式：返回核心集 + 已激活的工具
		result := make([]string, 0, len(tokenEconomyCoreBuiltins)+len(m.activatedTools))
		seen := make(map[string]bool)
		for _, t := range tokenEconomyCoreBuiltins {
			result = append(result, t)
			seen[t] = true
		}
		for t := range m.activatedTools {
			if !seen[t] {
				result = append(result, t)
				seen[t] = true
			}
		}
		return result

	default:
		return tokenEconomyCoreBuiltins
	}
}

// ActivateTool 激活一个工具（会话内永久保持）
func (m *ProgressiveToolManager) ActivateTool(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}

	// 检查冷却时间
	if time.Since(m.lastActivate) < m.cooldown {
		// 冷却中，但仍允许激活（只是记录警告）
		slog.Debug("OPT-14: tool activation during cooldown",
			"tool", name, "cooldown_remaining", m.cooldown-time.Since(m.lastActivate))
	}

	// 如果已经在活跃集中，不需要操作
	if m.activatedTools[name] {
		return false
	}

	m.activatedTools[name] = true
	m.lastActivate = time.Now()

	// 如果级别是最小，升级到扩展
	if m.level == DisclosureMinimal {
		m.level = DisclosureExtended
	}

	slog.Info("OPT-14: tool activated",
		"tool", name,
		"level", m.level.String(),
		"total_activated", len(m.activatedTools),
	)
	return true
}

// RecordToolUse 记录工具使用（用于自动推荐）
func (m *ProgressiveToolManager) RecordToolUse(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolUsage[name]++
}

// RecommendToolsForScene 根据场景推荐工具
func (m *ProgressiveToolManager) RecommendToolsForScene(scene string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	recommendations, ok := m.sceneRecommendations[scene]
	if !ok {
		return nil
	}

	// 过滤掉已激活的工具
	var newTools []string
	for _, t := range recommendations {
		if !m.activatedTools[t] {
			newTools = append(newTools, t)
		}
	}
	return newTools
}

// SetLevel 设置披露级别
func (m *ProgressiveToolManager) SetLevel(level ToolDisclosureLevel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.level != level {
		old := m.level
		m.level = level
		slog.Info("OPT-14: disclosure level changed",
			"from", old.String(), "to", level.String())
	}
}

// GetLevel 获取当前披露级别
func (m *ProgressiveToolManager) GetLevel() ToolDisclosureLevel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.level
}

// GetStats 获取统计信息
func (m *ProgressiveToolManager) GetStats() ProgressiveToolStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ProgressiveToolStats{
		Level:           m.level.String(),
		ActiveToolCount: len(m.GetActiveToolsUnsafe()),
		ActivatedCount:  len(m.activatedTools),
		TotalUsage:      len(m.toolUsage),
	}
}

// GetActiveToolsUnsafe 不加锁版本
func (m *ProgressiveToolManager) GetActiveToolsUnsafe() []string {
	switch m.level {
	case DisclosureFull:
		return nil
	case DisclosureMinimal:
		result := make([]string, 0, len(minimalToolSet)+len(m.activatedTools))
		seen := make(map[string]bool)
		for _, t := range minimalToolSet {
			result = append(result, t)
			seen[t] = true
		}
		for t := range m.activatedTools {
			if !seen[t] {
				result = append(result, t)
				seen[t] = true
			}
		}
		return result
	default:
		result := make([]string, 0, len(tokenEconomyCoreBuiltins)+len(m.activatedTools))
		seen := make(map[string]bool)
		for _, t := range tokenEconomyCoreBuiltins {
			result = append(result, t)
			seen[t] = true
		}
		for t := range m.activatedTools {
			if !seen[t] {
				result = append(result, t)
				seen[t] = true
			}
		}
		return result
	}
}

// ProgressiveToolStats 渐进式工具统计
type ProgressiveToolStats struct {
	Level           string `json:"level"`
	ActiveToolCount int    `json:"activeToolCount"`
	ActivatedCount  int    `json:"activatedCount"`
	TotalUsage      int    `json:"totalUsage"`
}

// needToolTool 是一个元工具，让模型请求激活更多工具
type needToolTool struct {
	manager *ProgressiveToolManager
}

func (t *needToolTool) Name() string { return "need_tool" }

func (t *needToolTool) Description() string {
	return `Progressive tool disclosure: request activation of a tool that is not currently available. 
Pass the tool name you need. The tool will become available on the next model request.
Common tools you might need: write_file, multi_edit, ls, code_index, todo_write, complete_step, 
bash_output, kill_shell, wait, move_file.`
}

func (*needToolTool) ReadOnly() bool { return true }

func (*needToolTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"tool": {
				"type": "string",
				"description": "Name of the tool to activate"
			},
			"reason": {
				"type": "string",
				"description": "Brief reason why this tool is needed (for analytics)"
			}
		},
		"required": ["tool"]
	}`)
}

func (t *needToolTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Tool   string `json:"tool"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	tool := strings.TrimSpace(p.Tool)
	if tool == "" {
		return "", fmt.Errorf("tool name is required")
	}

	activated := t.manager.ActivateTool(tool)
	if activated {
		return fmt.Sprintf("Tool %q activated. It will be available on your next request.", tool), nil
	}
	return fmt.Sprintf("Tool %q is already available.", tool), nil
}

// NeedToolSchema 返回 need_tool 工具的 schema（用于注册）
func NeedToolSchema(manager *ProgressiveToolManager) *needToolTool {
	return &needToolTool{manager: manager}
}

// EstimateToolSchemaTokens 估算工具 schema 的 token 数
// 用于决定是否需要降低工具表面
func EstimateToolSchemaTokens(toolCount int) int {
	// 经验值：每个工具 schema 约 200 token（压缩后）
	return toolCount * 200
}

// ShouldReduceToolSurface 判断是否应该降低工具表面
// 当工具 schema token 数超过阈值时返回 true
func ShouldReduceToolSurface(toolCount int, maxTokens int) bool {
	estimated := EstimateToolSchemaTokens(toolCount)
	return estimated > maxTokens
}
