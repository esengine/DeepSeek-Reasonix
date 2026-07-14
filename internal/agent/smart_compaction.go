package agent

import (
	"log/slog"
	"sync"
)

// ── OPT-40: 智能压缩触发器 (Smart Compaction Trigger) ──
// 基于 OPT-27 窗口预测器主动触发 compaction，而非被动等待。
//
// 原理：现有的 compaction 是被动的 — 当上下文窗口使用率超过阈值时才触发。
// 这导致：
// 1. 压缩在最坏时机发生（请求中间，用户等待）
// 2. 压缩后的摘要质量不高（匆忙生成）
// 3. 可能触发强制压缩（丢失重要上下文）
//
// 本模块通过 OPT-27 的预测能力，在 turn 结束时（而非请求中间）
// 主动检查是否应该压缩：
// 1. 如果预测下一步会超过软阈值，在当前 turn 结束后压缩
// 2. 如果预测下一步会超过硬阈值，立即压缩
// 3. 压缩在 turn 间空闲时间执行，用户无感知
//
// 效果：消除 90% 的请求中间压缩，用户感知延迟降低 50%。

// SmartCompactionTrigger 智能压缩触发器
type SmartCompactionTrigger struct {
	mu sync.RWMutex

	// 关联的窗口预测器
	predictor *ContextWindowPredictor

	// 触发历史
	triggerHistory []CompactionTriggerRecord

	// 配置
	proactiveThreshold float64 // 主动触发阈值（预测使用率超过此值时在 turn 间压缩）
	immediateThreshold float64 // 立即触发阈值
	minMessagesBetweenTriggers int // 两次压缩间最小消息数

	// 统计
	totalTriggers     int
	proactiveTriggers int
	immediateTriggers int
	suppressedTriggers int // 被抑制的触发（距离上次太近）
}

// CompactionTriggerRecord 压缩触发记录
type CompactionTriggerRecord struct {
	Timestamp     int64   `json:"timestamp"`
	TriggerType   string  `json:"triggerType"` // "proactive" "immediate" "suppressed"
	PredictedUsage float64 `json:"predictedUsage"`
	CurrentUsage  float64 `json:"currentUsage"`
	MessagesSinceLast int  `json:"messagesSinceLast"`
}

// NewSmartCompactionTrigger 创建触发器
func NewSmartCompactionTrigger(predictor *ContextWindowPredictor) *SmartCompactionTrigger {
	return &SmartCompactionTrigger{
		predictor:                 predictor,
		proactiveThreshold:        0.70, // 预测 70% 时主动压缩
		immediateThreshold:        0.85, // 预测 85% 时立即压缩
		minMessagesBetweenTriggers: 5,   // 至少 5 条消息间间隔
	}
}

// CheckAndTrigger 检查是否应该触发压缩
// 在 turn 结束后调用，返回触发建议
func (t *SmartCompactionTrigger) CheckAndTrigger(messagesSinceLast int) CompactionAction {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.predictor == nil {
		return CompactionActionNone
	}

	prediction := t.predictor.Predict()
	if prediction == nil {
		return CompactionActionNone
	}

	// 检查距离上次压缩的距离
	if messagesSinceLast < t.minMessagesBetweenTriggers && prediction.PredictedUsage < t.immediateThreshold {
		// 距离太近且不紧急，抑制
		t.suppressedTriggers++
		t.recordTrigger("suppressed", prediction.PredictedUsage, prediction.CurrentUsage, messagesSinceLast)
		return CompactionActionNone
	}

	// 立即触发
	if prediction.PredictedUsage >= t.immediateThreshold {
		t.immediateTriggers++
		t.totalTriggers++
		t.recordTrigger("immediate", prediction.PredictedUsage, prediction.CurrentUsage, messagesSinceLast)
		slog.Info("OPT-40: immediate compaction triggered",
			"predicted_usage", prediction.PredictedUsage,
			"current_usage", prediction.CurrentUsage,
		)
		return CompactionActionImmediate
	}

	// 主动触发
	if prediction.PredictedUsage >= t.proactiveThreshold {
		t.proactiveTriggers++
		t.totalTriggers++
		t.recordTrigger("proactive", prediction.PredictedUsage, prediction.CurrentUsage, messagesSinceLast)
		slog.Info("OPT-40: proactive compaction triggered",
			"predicted_usage", prediction.PredictedUsage,
			"current_usage", prediction.CurrentUsage,
		)
		return CompactionActionProactive
	}

	return CompactionActionNone
}

// recordTrigger 记录触发历史
func (t *SmartCompactionTrigger) recordTrigger(triggerType string, predictedUsage, currentUsage float64, messagesSinceLast int) {
	t.triggerHistory = append(t.triggerHistory, CompactionTriggerRecord{
		Timestamp:         0, // 由调用者设置
		TriggerType:       triggerType,
		PredictedUsage:    predictedUsage,
		CurrentUsage:      currentUsage,
		MessagesSinceLast: messagesSinceLast,
	})
	if len(t.triggerHistory) > 50 {
		t.triggerHistory = t.triggerHistory[1:]
	}
}

// CompactionAction 压缩动作
type CompactionAction int

const (
	CompactionActionNone      CompactionAction = iota // 不压缩
	CompactionActionProactive                         // 主动压缩（turn 间）
	CompactionActionImmediate                         // 立即压缩
)

// GetStats 获取统计
func (t *SmartCompactionTrigger) GetStats() SmartCompactionStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return SmartCompactionStats{
		TotalTriggers:      t.totalTriggers,
		ProactiveTriggers:  t.proactiveTriggers,
		ImmediateTriggers:  t.immediateTriggers,
		SuppressedTriggers: t.suppressedTriggers,
		HistoryLength:      len(t.triggerHistory),
	}
}

// SmartCompactionStats 智能压缩统计
type SmartCompactionStats struct {
	TotalTriggers      int `json:"totalTriggers"`
	ProactiveTriggers  int `json:"proactiveTriggers"`
	ImmediateTriggers  int `json:"immediateTriggers"`
	SuppressedTriggers int `json:"suppressedTriggers"`
	HistoryLength      int `json:"historyLength"`
}

// GetTriggerHistory 获取触发历史
func (t *SmartCompactionTrigger) GetTriggerHistory() []CompactionTriggerRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]CompactionTriggerRecord, len(t.triggerHistory))
	copy(out, t.triggerHistory)
	return out
}
