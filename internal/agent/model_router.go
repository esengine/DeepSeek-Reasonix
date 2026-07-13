package agent

import (
	"strings"
	"sync"
)

// ── OPT-32: 多模型路由 (Multi-Model Routing) ──
// 根据任务复杂度将请求路由到不同成本的模型。
//
// 原理：简单任务（如格式化、简单问答）不需要最强模型。通过路由：
// 1. 简单任务 → 便宜模型（如 deepseek-chat, $0.27/M）
// 2. 标准任务 → 标准模型（如 deepseek-reasoner, $2.7/M）
// 3. 复杂任务 → 强力模型（如 claude-sonnet, $3/M）
//
// 注意：这不减少单次 token 数量，但通过降低每 token 成本来减少总支出。
// 在"不减少 token 消耗"的理念下，这是通过模型选择来降低成本。
//
// 效果：简单任务成本降低 90%（$3/M → $0.27/M），整体成本降低 40-60%。

// ModelRouter 多模型路由器
type ModelRouter struct {
	mu sync.RWMutex

	// 主模型（复杂任务）
	primaryModel string
	// 经济模型（简单任务）
	economyModel string
	// 标准模型（中等任务）
	standardModel string

	// 路由统计
	routeStats map[string]int

	// 配置
	enabled          bool
	simpleTaskModels map[string]bool // 简单任务类型集合
}

// ModelRouteDecision 路由决策
type ModelRouteDecision struct {
	TargetModel string  `json:"targetModel"`
	Reason      string  `json:"reason"`
	Confidence  float64 `json:"confidence"`
	SavedCost   float64 `json:"savedCost"` // 相比主模型节省的成本
}

// NewModelRouter 创建路由器
func NewModelRouter(primary, economy, standard string) *ModelRouter {
	return &ModelRouter{
		primaryModel:     primary,
		economyModel:     economy,
		standardModel:    standard,
		routeStats:       make(map[string]int),
		enabled:          true,
		simpleTaskModels: make(map[string]bool),
	}
}

// Route 根据输入决定路由目标
func (r *ModelRouter) Route(userInput string, scene string, complexity int) *ModelRouteDecision {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.enabled {
		return &ModelRouteDecision{
			TargetModel: r.primaryModel,
			Reason:      "routing disabled",
			Confidence:  1.0,
		}
	}

	// 复杂任务 → 主模型
	if complexity >= 7 {
		r.routeStats["primary"]++
		return &ModelRouteDecision{
			TargetModel: r.primaryModel,
			Reason:      "high complexity task",
			Confidence:  0.9,
		}
	}

	// 简单任务 → 经济模型
	if complexity <= 3 || isSimpleTask(userInput, scene) {
		r.routeStats["economy"]++
		savedCost := estimateCostSavings(r.primaryModel, r.economyModel)
		return &ModelRouteDecision{
			TargetModel: r.economyModel,
			Reason:      "simple task routed to economy model",
			Confidence:  0.85,
			SavedCost:   savedCost,
		}
	}

	// 中等任务 → 标准模型
	if r.standardModel != "" {
		r.routeStats["standard"]++
		savedCost := estimateCostSavings(r.primaryModel, r.standardModel)
		return &ModelRouteDecision{
			TargetModel: r.standardModel,
			Reason:      "medium complexity task",
			Confidence:  0.75,
			SavedCost:   savedCost,
		}
	}

	// 默认 → 主模型
	r.routeStats["primary"]++
	return &ModelRouteDecision{
		TargetModel: r.primaryModel,
		Reason:      "default routing",
		Confidence:  0.5,
	}
}

// isSimpleTask 判断是否是简单任务
func isSimpleTask(input string, scene string) bool {
	lower := strings.ToLower(input)

	// 简单问答
	if scene == "simple_qa" || scene == "greeting" {
		return true
	}

	// 短输入（< 50 字符）且不含复杂关键词
	if len(lower) < 50 && !containsAny(lower, "refactor", "debug", "architect", "design", "analyze") {
		return true
	}

	// 格式化任务
	if containsAny(lower, "format", "lint", "sort", "rename") {
		return true
	}

	// 翻译任务
	if containsAny(lower, "translate", "翻译") {
		return true
	}

	return false
}

// estimateCostSavings 估算成本节省
func estimateCostSavings(expensiveModel, cheapModel string) float64 {
	// 简化的成本对比（每百万 token 的差异）
	expensiveCosts := map[string]float64{
		"claude-sonnet": 3.0,
		"claude-opus":   15.0,
		"gpt-4o":        2.5,
	}
	cheapCosts := map[string]float64{
		"deepseek-chat":     0.27,
		"deepseek-reasoner": 0.55,
		"gpt-4o-mini":       0.15,
	}

	expCost, ok1 := expensiveCosts[expensiveModel]
	cheapCost, ok2 := cheapCosts[cheapModel]
	if !ok1 || !ok2 {
		return 0
	}

	return expCost - cheapCost
}

// GetStats 获取统计
func (r *ModelRouter) GetStats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RouterStats{
		PrimaryCount:  r.routeStats["primary"],
		EconomyCount:  r.routeStats["economy"],
		StandardCount: r.routeStats["standard"],
		Enabled:       r.enabled,
		PrimaryModel:  r.primaryModel,
		EconomyModel:  r.economyModel,
		StandardModel: r.standardModel,
	}
}

// RouterStats 路由统计
type RouterStats struct {
	PrimaryCount  int    `json:"primaryCount"`
	EconomyCount  int    `json:"economyCount"`
	StandardCount int    `json:"standardCount"`
	Enabled       bool   `json:"enabled"`
	PrimaryModel  string `json:"primaryModel"`
	EconomyModel  string `json:"economyModel"`
	StandardModel string `json:"standardModel"`
}

// SetEnabled 启用/禁用路由
func (r *ModelRouter) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = enabled
}
