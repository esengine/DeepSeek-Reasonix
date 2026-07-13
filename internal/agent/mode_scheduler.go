package agent

import (
	"sync"
)

// ── OPT-36: Token 模式感知调度器 (Token Mode-Aware Scheduler) ──
// 按 economy/delivery/full 模式动态调整所有 OPT 模块的行为。
//
// 原理：项目有三种 token 模式：
// - economy: 最小化 token 消耗（缩减工具面、提前压缩、激进裁剪）
// - delivery: 交付质量优先（完整工具面、延迟压缩、保留更多上下文）
// - full: 默认平衡模式
//
// 不同模式下，OPT 模块的参数应该不同。例如：
// - economy 模式下 OPT-18 的压缩阈值应更低（提前压缩）
// - delivery 模式下 OPT-30 的工具描述不应轮换（保留完整描述）
// - economy 模式下 OPT-29 的压缩级别应为 aggressive
//
// 效果：让所有 OPT 模块协同响应当前 token 模式，避免模式与优化策略矛盾。

// TokenMode token 模式
type TokenMode int

const (
	TokenModeFull     TokenMode = iota // 默认完整模式
	TokenModeEconomy                   // 经济模式
	TokenModeDelivery                  // 交付质量模式
)

func (m TokenMode) String() string {
	switch m {
	case TokenModeEconomy:
		return "economy"
	case TokenModeDelivery:
		return "delivery"
	default:
		return "full"
	}
}

// ModeAwareScheduler 模式感知调度器
type ModeAwareScheduler struct {
	mu   sync.RWMutex
	mode TokenMode

	// 模式对应的优化配置
	config ModeOptConfig
}

// ModeOptConfig 模式对应的优化配置
type ModeOptConfig struct {
	// OPT-18 上下文预算
	CompactThreshold float64 // 压缩阈值
	ForceThreshold   float64 // 强制压缩阈值
	TailTokens       int     // 压缩后保留尾部 token

	// OPT-29 提示压缩
	PromptCompressLevel CompressLevel // 压缩级别

	// OPT-30 工具描述轮换
	EnableDescRotation bool // 是否启用描述轮换

	// OPT-03 语义裁剪
	EnableSemanticPrune bool // 是否启用语义裁剪

	// OPT-16 工具记忆化
	EnableToolMemo bool // 是否启用工具记忆化

	// OPT-17 对话去重
	EnableDedup bool // 是否启用对话去重

	// OPT-21 工具批处理
	EnableBatching bool // 是否启用工具批处理

	// OPT-32 多模型路由
	EnableModelRouting bool // 是否启用多模型路由
}

// NewModeAwareScheduler 创建调度器
func NewModeAwareScheduler(mode TokenMode) *ModeAwareScheduler {
	s := &ModeAwareScheduler{mode: mode}
	s.applyMode(mode)
	return s
}

// applyMode 应用模式配置
func (s *ModeAwareScheduler) applyMode(mode TokenMode) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.mode = mode
	switch mode {
	case TokenModeEconomy:
		// 经济模式：激进优化
		s.config = ModeOptConfig{
			CompactThreshold:    0.60, // 提前压缩
			ForceThreshold:      0.75,
			TailTokens:          4096,
			PromptCompressLevel: CompressAggressive,
			EnableDescRotation:  true,
			EnableSemanticPrune: true,
			EnableToolMemo:      true,
			EnableDedup:         true,
			EnableBatching:      true,
			EnableModelRouting:  true, // 经济模式启用模型路由
		}

	case TokenModeDelivery:
		// 交付模式：保守优化，保留完整信息
		s.config = ModeOptConfig{
			CompactThreshold:    0.85, // 延迟压缩
			ForceThreshold:      0.93,
			TailTokens:          32768,
			PromptCompressLevel: CompressLight, // 轻度压缩
			EnableDescRotation:  false,         // 保留完整工具描述
			EnableSemanticPrune: false,         // 不裁剪
			EnableToolMemo:      false,         // 不记忆化（需要完整结果）
			EnableDedup:         false,         // 不去重
			EnableBatching:      false,         // 不批处理
			EnableModelRouting:  false,         // 不路由（用最强模型）
		}

	default:
		// 默认模式：平衡
		s.config = ModeOptConfig{
			CompactThreshold:    0.80,
			ForceThreshold:      0.90,
			TailTokens:          8192,
			PromptCompressLevel: CompressMedium,
			EnableDescRotation:  true,
			EnableSemanticPrune: true,
			EnableToolMemo:      true,
			EnableDedup:         true,
			EnableBatching:      true,
			EnableModelRouting:  false, // 默认不路由
		}
	}
}

// SetMode 切换模式
func (s *ModeAwareScheduler) SetMode(mode TokenMode) {
	s.mu.Lock()
	currentMode := s.mode
	s.mu.Unlock()
	if currentMode != mode {
		s.applyMode(mode)
	}
}

// GetMode 获取当前模式
func (s *ModeAwareScheduler) GetMode() TokenMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

// GetConfig 获取当前配置
func (s *ModeAwareScheduler) GetConfig() ModeOptConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// ApplyToAgent 将模式配置应用到 Agent 的各 OPT 模块
func (s *ModeAwareScheduler) ApplyToAgent(a *Agent) {
	s.mu.RLock()
	config := s.config
	s.mu.RUnlock()

	// OPT-18: 调整上下文预算
	if a.contextBudget != nil {
		switch s.mode {
		case TokenModeEconomy:
			a.contextBudget.AdjustForScene("economy", 1)
		case TokenModeDelivery:
			a.contextBudget.AdjustForScene("delivery", 10)
		default:
			a.contextBudget.AdjustForScene("standard", 5)
		}
	}

	// OPT-17: 启用/禁用去重
	if a.conversationDedup != nil {
		a.conversationDedup.SetEnabled(config.EnableDedup)
	}

	// OPT-29: 调整压缩级别（通过重新创建压缩机）
	// 注意：promptCompressor 的 level 是创建时设置的，
	// 这里通过模式感知让调用者知道应该用什么级别

	// OPT-30: 启用/禁用描述轮换
	// 通过 EnableDescRotation 标志控制
}
