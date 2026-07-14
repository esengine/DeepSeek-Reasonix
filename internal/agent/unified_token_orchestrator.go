package agent

import (
	"sort"
	"sync"
)

// ── OPT-100: UnifiedTokenOrchestrator (统一 Token 优化编排器) ──
// 作为第 100 个 OPT 模块（里程碑），统一编排所有其他 OPT 模块，
// 根据当前上下文状况和策略，推荐最优的优化组合以最大化 token 效率。
//
// 原理：
//   - RegisterModule/UnregisterModule 管理已激活的优化模块
//   - Orchestrate 接收当前上下文（token 用量、消息数、轮次、缓存统计），
//     根据策略（conservative/balanced/aggressive）生成推荐操作列表
//   - 策略影响触发优化的阈值：
//     · conservative：阈值高，较少触发优化
//     · balanced（默认）：中等阈值
//     · aggressive：阈值低，最大化节省
//   - 返回推荐操作、估算节省量、优先级和已咨询模块数
//
// 效果：通过统一编排，避免各模块独立工作时的冲突和遗漏，
// 实现全局最优的 token 优化策略。

// OrchestrationContext 编排上下文，描述当前对话的 token 使用状况。
type OrchestrationContext struct {
	PromptTokens    int // 提示 token 数
	CompletionTokens int // 补全 token 数
	CacheHitTokens  int // 缓存命中 token 数
	CacheMissTokens int // 缓存未命中 token 数
	MessageCount    int // 消息数量
	Turn            int // 当前对话轮次
	ContextWindow   int // 上下文窗口大小
}

// OrchestrationResult 编排结果，包含推荐操作和估算信息。
type OrchestrationResult struct {
	RecommendedActions []string // 推荐操作列表
	EstimatedSavings   int      // 估算可节省的 token 数
	Priority           string   // 优先级（high/medium/low）
	ModulesConsulted   int      // 已咨询的模块数
}

// OrchestratorStats 编排器统计信息。
type OrchestratorStats struct {
	Orchestrations    int    // 编排总次数
	TotalTokensSaved  int    // 累计估算节省的 token 数
	ActiveModuleCount int    // 活跃模块数
	Strategy          string // 当前策略
}

// UnifiedTokenOrchestrator 统一 token 优化编排器。
type UnifiedTokenOrchestrator struct {
	mu                 sync.RWMutex
	activeModules      map[string]bool
	orchestrations     int
	totalTokensSaved   int
	strategy           string
	lastOptimization   string
}

// NewUnifiedTokenOrchestrator 创建一个新的 UnifiedTokenOrchestrator 实例。
// 默认策略为 "balanced"。
func NewUnifiedTokenOrchestrator() *UnifiedTokenOrchestrator {
	return &UnifiedTokenOrchestrator{
		activeModules: make(map[string]bool),
		strategy:      "balanced",
	}
}

// RegisterModule 注册一个模块为活跃状态。
func (o *UnifiedTokenOrchestrator) RegisterModule(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.activeModules[name] = true
}

// UnregisterModule 取消注册一个模块。
func (o *UnifiedTokenOrchestrator) UnregisterModule(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	delete(o.activeModules, name)
}

// GetActiveModules 返回已排序的活跃模块名称列表。
func (o *UnifiedTokenOrchestrator) GetActiveModules() []string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	names := make([]string, 0, len(o.activeModules))
	for name, active := range o.activeModules {
		if active {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Orchestrate 根据当前上下文和策略，从活跃模块中生成推荐操作。
// 返回包含推荐操作、估算节省量、优先级和已咨询模块数的结果。
func (o *UnifiedTokenOrchestrator) Orchestrate(ctx OrchestrationContext) OrchestrationResult {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.orchestrations++

	// 复制活跃模块列表（避免在锁内长时间持有）
	activeCount := len(o.activeModules)
	modules := make(map[string]bool, activeCount)
	for k, v := range o.activeModules {
		modules[k] = v
	}
	strategy := o.strategy

	actions := []string{}
	estimatedSavings := 0

	// 根据策略确定触发阈值
	var cacheMissThreshold, promptUtilThreshold float64
	var msgCountThreshold, turnThreshold int

	switch strategy {
	case "conservative":
		cacheMissThreshold = 0.7
		promptUtilThreshold = 0.90
		msgCountThreshold = 30
		turnThreshold = 20
	case "aggressive":
		cacheMissThreshold = 0.3
		promptUtilThreshold = 0.60
		msgCountThreshold = 10
		turnThreshold = 5
	default: // balanced
		cacheMissThreshold = 0.5
		promptUtilThreshold = 0.75
		msgCountThreshold = 20
		turnThreshold = 10
	}

	// 缓存分析
	totalCacheTokens := ctx.CacheHitTokens + ctx.CacheMissTokens
	if totalCacheTokens > 0 {
		missRate := float64(ctx.CacheMissTokens) / float64(totalCacheTokens)
		if missRate > cacheMissThreshold {
			if modules["CacheWarmingV2"] || modules["CacheWarmingScheduler"] {
				actions = append(actions, "Activate cache warming for predicted follow-up queries")
				estimatedSavings += ctx.CacheMissTokens / 4
			}
			if modules["CachePrefixStabilizer"] {
				actions = append(actions, "Stabilize cache prefix to improve hit rate")
				estimatedSavings += ctx.CacheMissTokens / 6
			}
			if modules["CacheEnforcer"] {
				actions = append(actions, "Enforce cache-friendly message ordering")
				estimatedSavings += ctx.CacheMissTokens / 8
			}
		}
	}

	// 上下文窗口分析
	if ctx.ContextWindow > 0 {
		utilization := float64(ctx.PromptTokens) / float64(ctx.ContextWindow)
		if utilization > promptUtilThreshold {
			if modules["SmartContextPruner"] {
				actions = append(actions, "Prune low-importance context messages")
				estimatedSavings += ctx.PromptTokens / 5
			}
			if modules["SlidingContextCompactor"] || modules["ContextCompactor"] {
				actions = append(actions, "Compact conversation history")
				estimatedSavings += ctx.PromptTokens / 4
			}
		}
	}

	// 消息数量分析
	if ctx.MessageCount > msgCountThreshold {
		if modules["DuplicateMessageDeduplicator"] || modules["MessageDeduplicator"] {
			actions = append(actions, "Deduplicate redundant messages")
			estimatedSavings += ctx.MessageCount * 50
		}
	}

	// 对话轮次分析
	if ctx.Turn > turnThreshold {
		if modules["ConversationTokenBudget"] {
			actions = append(actions, "Review conversation token budget allocation")
		}
	}

	// 补全 token 分析
	if ctx.CompletionTokens > 0 && ctx.PromptTokens > 0 {
		total := ctx.PromptTokens + ctx.CompletionTokens
		if float64(ctx.CompletionTokens)/float64(total) > 0.5 {
			if modules["OutputTokenLimiter"] || modules["ResponseOptimizer"] {
				actions = append(actions, "Limit output token generation")
				estimatedSavings += ctx.CompletionTokens / 5
			}
		}
	}

	// 如果没有推荐操作但有活跃模块
	if len(actions) == 0 && activeCount > 0 {
		actions = append(actions, "No optimization needed at current token levels")
	}

	// 确定优先级
	priority := "low"
	if ctx.ContextWindow > 0 {
		utilization := float64(ctx.PromptTokens) / float64(ctx.ContextWindow)
		if utilization > 0.9 {
			priority = "high"
		} else if utilization > promptUtilThreshold {
			priority = "medium"
		}
	}
	if totalCacheTokens > 0 && float64(ctx.CacheMissTokens)/float64(totalCacheTokens) > 0.7 {
		priority = "high"
	}

	o.totalTokensSaved += estimatedSavings

	if len(actions) > 0 {
		o.lastOptimization = actions[0]
	} else {
		o.lastOptimization = "none"
	}

	return OrchestrationResult{
		RecommendedActions: actions,
		EstimatedSavings:   estimatedSavings,
		Priority:           priority,
		ModulesConsulted:   activeCount,
	}
}

// SetStrategy 设置编排策略。
// "conservative"：较少优化，"balanced"（默认）：平衡，"aggressive"：最大化节省。
// 无效的策略值将被忽略。
func (o *UnifiedTokenOrchestrator) SetStrategy(strategy string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	switch strategy {
	case "conservative", "balanced", "aggressive":
		o.strategy = strategy
	}
}

// GetStats 返回编排器的统计信息。
func (o *UnifiedTokenOrchestrator) GetStats() OrchestratorStats {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return OrchestratorStats{
		Orchestrations:    o.orchestrations,
		TotalTokensSaved:  o.totalTokensSaved,
		ActiveModuleCount: len(o.activeModules),
		Strategy:          o.strategy,
	}
}

// Reset 清除所有编排统计和已注册模块，策略重置为 "balanced"。
func (o *UnifiedTokenOrchestrator) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.activeModules = make(map[string]bool)
	o.orchestrations = 0
	o.totalTokensSaved = 0
	o.strategy = "balanced"
	o.lastOptimization = ""
}
