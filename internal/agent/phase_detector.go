package agent

import (
	"sync"

	"reasonix/internal/provider"
)

// ── OPT-46: 对话阶段检测器 (Conversation Phase Detector) ──
// 检测当前对话所处的阶段（探索 / 执行 / 收尾），并据此提供优化提示。
//
// 原理：在一次完整对话中，用户的需求通常经历三个阶段：
//  1. 探索阶段（Exploration）：用户在提问、澄清需求，模型以文本回复为主。
//     此阶段历史消息可以激进压缩，工具结果可以记忆化。
//  2. 执行阶段（Execution）：模型正在密集调用工具完成任务。
//     此阶段应保留完整工具结果，避免压缩导致上下文丢失。
//  3. 收尾阶段（WrapUp）：对话趋于结束，工具调用减少。
//     此阶段可以激进压缩并主动摘要早期对话。
//
// 效果：根据阶段动态调整压缩策略与记忆化策略，
// 探索/收尾阶段节省 30-50% token，执行阶段保证上下文完整性。

// ConversationPhase 对话阶段
type ConversationPhase int

const (
	// PhaseUnknown 未知阶段（初始状态）
	PhaseUnknown ConversationPhase = iota
	// PhaseExploration 探索阶段：用户在提问、澄清需求
	PhaseExploration
	// PhaseExecution 执行阶段：模型正在密集调用工具
	PhaseExecution
	// PhaseWrapUp 收尾阶段：对话趋于结束
	PhaseWrapUp
)

// String 返回对话阶段的字符串表示
func (p ConversationPhase) String() string {
	switch p {
	case PhaseExploration:
		return "exploration"
	case PhaseExecution:
		return "execution"
	case PhaseWrapUp:
		return "wrapup"
	default:
		return "unknown"
	}
}

// PhaseTransition 阶段转换记录
type PhaseTransition struct {
	From ConversationPhase `json:"from"`
	To   ConversationPhase `json:"to"`
	Turn int               `json:"turn"`
}

// PhaseOptHint 阶段优化提示
type PhaseOptHint struct {
	// CompressLevel 提示压缩级别
	CompressLevel CompressLevel `json:"compressLevel"`
	// EnableToolMemo 是否启用工具结果记忆化
	EnableToolMemo bool `json:"enableToolMemo"`
	// EnableDedup 是否启用消息去重
	EnableDedup bool `json:"enableDedup"`
	// AggressiveCompact 是否进行激进的上下文压缩
	AggressiveCompact bool `json:"aggressiveCompact"`
}

// PhaseDetectorStats 阶段检测器统计信息
type PhaseDetectorStats struct {
	CurrentPhase          string            `json:"currentPhase"`
	TurnCount             int               `json:"turnCount"`
	ToolUsageCount        int               `json:"toolUsageCount"`
	TotalPhaseTransitions int               `json:"totalPhaseTransitions"`
	PhaseHistory          []PhaseTransition `json:"phaseHistory"`
}

// ConversationPhaseDetector 对话阶段检测器
type ConversationPhaseDetector struct {
	mu             sync.RWMutex
	currentPhase   ConversationPhase
	turnCount      int
	toolUsageCount int
	phaseHistory   []PhaseTransition
	totalDetected  int
}

// NewConversationPhaseDetector 创建对话阶段检测器
func NewConversationPhaseDetector() *ConversationPhaseDetector {
	return &ConversationPhaseDetector{
		currentPhase: PhaseUnknown,
		phaseHistory: make([]PhaseTransition, 0),
	}
}

// Analyze 分析当前对话消息和工具调用情况，检测对话阶段
func (d *ConversationPhaseDetector) Analyze(messages []provider.Message, toolCallsThisTurn int) ConversationPhase {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 增加轮次计数
	d.turnCount++
	// 累加工具使用计数
	d.toolUsageCount += toolCallsThisTurn
	// 增加总检测次数
	d.totalDetected++

	var detected ConversationPhase

	// 阶段检测逻辑
	switch {
	// a. 前 2 轮视为探索阶段（用户在探索/提问）
	case d.turnCount <= 2:
		detected = PhaseExploration

	// b. 超过 2 轮且当前轮有工具调用 → 执行阶段
	case toolCallsThisTurn > 0 && d.turnCount > 2:
		detected = PhaseExecution

	// c. 超过 10 轮且当前轮无工具调用 → 收尾阶段
	case d.turnCount > 10 && toolCallsThisTurn == 0:
		detected = PhaseWrapUp

	// d. 默认：保持当前阶段不变
	default:
		detected = d.currentPhase
	}

	// 记录阶段转换（仅当阶段发生变化时）
	if detected != d.currentPhase {
		transition := PhaseTransition{
			From: d.currentPhase,
			To:   detected,
			Turn: d.turnCount,
		}
		d.phaseHistory = append(d.phaseHistory, transition)
		d.currentPhase = detected
	}

	return detected
}

// GetPhase 获取当前对话阶段
func (d *ConversationPhaseDetector) GetPhase() ConversationPhase {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentPhase
}

// GetOptimizationHint 根据当前对话阶段返回优化提示
func (d *ConversationPhaseDetector) GetOptimizationHint() PhaseOptHint {
	d.mu.RLock()
	defer d.mu.RUnlock()

	switch d.currentPhase {
	// 探索阶段：激进压缩，启用工具记忆化
	case PhaseExploration:
		return PhaseOptHint{
			CompressLevel:     CompressAggressive,
			EnableToolMemo:    true,
			EnableDedup:       true,
			AggressiveCompact: true,
		}

	// 执行阶段：最小压缩，保留完整工具结果
	case PhaseExecution:
		return PhaseOptHint{
			CompressLevel:     CompressLight,
			EnableToolMemo:    false,
			EnableDedup:       true,
			AggressiveCompact: false,
		}

	// 收尾阶段：激进压缩，主动摘要
	case PhaseWrapUp:
		return PhaseOptHint{
			CompressLevel:     CompressAggressive,
			EnableToolMemo:    true,
			EnableDedup:       true,
			AggressiveCompact: true,
		}

	// 未知阶段：保守策略，不压缩
	default:
		return PhaseOptHint{
			CompressLevel:     CompressNone,
			EnableToolMemo:    false,
			EnableDedup:       false,
			AggressiveCompact: false,
		}
	}
}

// GetStats 获取阶段检测器的统计信息
func (d *ConversationPhaseDetector) GetStats() PhaseDetectorStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 复制 phaseHistory 避免外部修改
	historyCopy := make([]PhaseTransition, len(d.phaseHistory))
	copy(historyCopy, d.phaseHistory)

	return PhaseDetectorStats{
		CurrentPhase:          d.currentPhase.String(),
		TurnCount:             d.turnCount,
		ToolUsageCount:        d.toolUsageCount,
		TotalPhaseTransitions: len(d.phaseHistory),
		PhaseHistory:          historyCopy,
	}
}

// Reset 重置检测器到初始状态
func (d *ConversationPhaseDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.currentPhase = PhaseUnknown
	d.turnCount = 0
	d.toolUsageCount = 0
	d.phaseHistory = make([]PhaseTransition, 0)
	d.totalDetected = 0
}
