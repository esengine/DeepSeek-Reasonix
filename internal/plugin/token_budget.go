package plugin

import (
	"sync"
)

// ── OPT-34: 插件 Token 预算 (Plugin Token Budget) ──
// 限制每个插件贡献到 system prompt 的 token 数量。
//
// 原理：插件可以向 system prompt 注入工具定义、提示片段、
// MCP 服务器配置等。如果不限制，一个贪心的插件可能注入
// 数千 token，挤压其他插件的可用空间。
//
// 通过预算分配：
// 1. 每个插件有默认 token 预算（如 500）
// 2. 超出预算的插件内容被截断
// 3. 用户可配置个别插件的预算
// 4. 总预算不超过 system prompt 的 20%
//
// 效果：防止单个插件 token 爆炸，保持 prompt 整体精简。

// PluginTokenBudget 插件 token 预算管理器
type PluginTokenBudget struct {
	mu sync.RWMutex

	// 默认预算（每插件）
	defaultBudget int

	// 每插件的预算覆盖
	pluginBudgets map[string]int

	// 每插件的实际消耗
	pluginConsumption map[string]int

	// 总预算上限（所有插件合计）
	totalBudget int

	// 统计
	totalTruncated int
	totalSaved     int
}

// NewPluginTokenBudget 创建预算管理器
func NewPluginTokenBudget(defaultBudget, totalBudget int) *PluginTokenBudget {
	if defaultBudget <= 0 {
		defaultBudget = 500
	}
	if totalBudget <= 0 {
		totalBudget = 3000 // 所有插件合计不超过 3000 token
	}
	return &PluginTokenBudget{
		defaultBudget:    defaultBudget,
		pluginBudgets:    make(map[string]int),
		pluginConsumption: make(map[string]int),
		totalBudget:      totalBudget,
	}
}

// SetPluginBudget 设置特定插件的预算
func (b *PluginTokenBudget) SetPluginBudget(pluginName string, budget int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pluginBudgets[pluginName] = budget
}

// GetPluginBudget 获取插件的预算
func (b *PluginTokenBudget) GetPluginBudget(pluginName string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if budget, ok := b.pluginBudgets[pluginName]; ok {
		return budget
	}
	return b.defaultBudget
}

// EnforceBudget 强制执行预算，返回截断后的内容和实际消耗
func (b *PluginTokenBudget) EnforceBudget(pluginName string, content string) (string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	budget := b.defaultBudget
	if custom, ok := b.pluginBudgets[pluginName]; ok {
		budget = custom
	}

	// 检查总预算剩余
	totalUsed := 0
	for _, consumed := range b.pluginConsumption {
		totalUsed += consumed
	}
	remaining := b.totalBudget - totalUsed
	if budget > remaining {
		budget = remaining
	}

	if budget <= 0 {
		b.totalTruncated++
		b.totalSaved += len(content) / 4
		b.pluginConsumption[pluginName] = 0
		return "", 0
	}

	// 估算 token（粗略 4 字符/token）
	estimatedTokens := len(content) / 4

	if estimatedTokens <= budget {
		// 在预算内
		b.pluginConsumption[pluginName] = estimatedTokens
		return content, estimatedTokens
	}

	// 超出预算，截断内容
	maxChars := budget * 4
	if maxChars > len(content) {
		maxChars = len(content)
	}
	truncated := content[:maxChars] + "\n[plugin content truncated to fit token budget]"

	saved := estimatedTokens - budget
	b.totalTruncated++
	b.totalSaved += saved
	b.pluginConsumption[pluginName] = budget

	return truncated, budget
}

// GetConsumption 获取插件消耗
func (b *PluginTokenBudget) GetConsumption(pluginName string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pluginConsumption[pluginName]
}

// GetTotalConsumption 获取所有插件总消耗
func (b *PluginTokenBudget) GetTotalConsumption() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	total := 0
	for _, c := range b.pluginConsumption {
		total += c
	}
	return total
}

// GetStats 获取统计
func (b *PluginTokenBudget) GetStats() PluginBudgetStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	totalUsed := 0
	for _, c := range b.pluginConsumption {
		totalUsed += c
	}

	return PluginBudgetStats{
		DefaultBudget:   b.defaultBudget,
		TotalBudget:     b.totalBudget,
		TotalUsed:       totalUsed,
		TotalTruncated:  b.totalTruncated,
		TotalSaved:      b.totalSaved,
		PluginCount:     len(b.pluginConsumption),
	}
}

// PluginBudgetStats 插件预算统计
type PluginBudgetStats struct {
	DefaultBudget  int `json:"defaultBudget"`
	TotalBudget    int `json:"totalBudget"`
	TotalUsed      int `json:"totalUsed"`
	TotalTruncated int `json:"totalTruncated"`
	TotalSaved     int `json:"totalSaved"`
	PluginCount    int `json:"pluginCount"`
}

// Reset 重置消耗记录
func (b *PluginTokenBudget) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pluginConsumption = make(map[string]int)
	b.totalTruncated = 0
	b.totalSaved = 0
}
