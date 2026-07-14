package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ── P0-2: WAL 恢复半完成副作用补偿追踪 ──
// 问题：WAL 恢复时，已发送的 API 请求或已执行的工具调用无法撤销。
// 方案：在执行副作用操作前记录补偿动作，恢复时按逆序执行补偿。
//
// 补偿模型基于 Saga 模式：每个副作用操作 paired 一个补偿操作。
// 恢复时从最后一个已执行副作用开始，逆序执行补偿。

// SideEffectType 副作用类型
type SideEffectType string

const (
	SideEffectToolCall   SideEffectType = "tool_call"   // 工具调用（如 bash 命令）
	SideEffectFileWrite  SideEffectType = "file_write"  // 文件写入
	SideEffectFileDelete SideEffectType = "file_delete" // 文件删除
	SideEffectAPICall    SideEffectType = "api_call"    // 外部 API 调用
	SideEffectNetworkReq SideEffectType = "network_req" // 网络请求
)

// SideEffect 记录一个已执行的副作用操作
type SideEffect struct {
	ID           string         // 唯一标识
	Type         SideEffectType // 副作用类型
	Timestamp    time.Time      // 执行时间
	Description  string         // 人类可读描述
	Compensation string         // 补偿动作描述（用于日志和手动恢复）
	Reversible   bool           // 是否可自动补偿
	// 补偿执行函数（如果可逆）
	Compensate func() error `json:"-"`
	// 已补偿标记
	compensated bool
}

// SideEffectTracker 追踪会话中的副作用操作
type SideEffectTracker struct {
	mu       sync.Mutex
	effects  []*SideEffect
	maxDepth int // 最大追踪深度，避免内存膨胀
}

// NewSideEffectTracker 创建副作用追踪器
func NewSideEffectTracker(maxDepth int) *SideEffectTracker {
	if maxDepth <= 0 {
		maxDepth = 100
	}
	return &SideEffectTracker{
		maxDepth: maxDepth,
	}
}

// Record 记录一个已执行的副作用操作
func (t *SideEffectTracker) Record(effect *SideEffect) {
	if effect == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.effects = append(t.effects, effect)
	// 超过最大深度时，丢弃最旧的不可逆操作（保留可逆操作用于补偿）
	if len(t.effects) > t.maxDepth {
		t.compact()
	}
}

// CompensateAll 逆序执行所有可逆副作用操作的补偿
func (t *SideEffectTracker) CompensateAll() []error {
	t.mu.Lock()
	effects := make([]*SideEffect, len(t.effects))
	copy(effects, t.effects)
	t.mu.Unlock()

	var errs []error
	// 逆序执行补偿
	for i := len(effects) - 1; i >= 0; i-- {
		effect := effects[i]
		if effect.compensated || !effect.Reversible {
			continue
		}
		if effect.Compensate != nil {
			if err := effect.Compensate(); err != nil {
				errs = append(errs, fmt.Errorf("compensate %s: %w", effect.ID, err))
			}
		}
		effect.compensated = true
	}
	return errs
}

// UncompensatedEffects 返回未补偿的副作用列表（用于恢复时的报告）
func (t *SideEffectTracker) UncompensatedEffects() []*SideEffect {
	t.mu.Lock()
	defer t.mu.Unlock()
	var result []*SideEffect
	for _, e := range t.effects {
		if !e.compensated {
			result = append(result, e)
		}
	}
	return result
}

// RecoveryReport 生成恢复报告，列出所有未补偿的副作用
func (t *SideEffectTracker) RecoveryReport() string {
	effects := t.UncompensatedEffects()
	if len(effects) == 0 {
		return "所有副作用操作已补偿或无可逆操作。"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("恢复发现 %d 个未补偿的副作用操作:\n", len(effects)))
	for i, e := range effects {
		reversible := "可手动补偿"
		if !e.Reversible {
			reversible = "不可逆"
		}
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s — %s (%s)\n",
			i+1, e.Type, e.Description, e.Compensation, reversible))
	}
	return sb.String()
}

// Reset 清空追踪器（新 turn 开始时调用）
func (t *SideEffectTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.effects = nil
}

// compact 丢弃最旧的不可逆操作，保留可逆操作
func (t *SideEffectTracker) compact() {
	reversible := make([]*SideEffect, 0, len(t.effects))
	for _, e := range t.effects {
		if e.Reversible && !e.compensated {
			reversible = append(reversible, e)
		}
	}
	t.effects = reversible
}
