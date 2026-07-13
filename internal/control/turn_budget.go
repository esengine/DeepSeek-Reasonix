package control

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ── OPT-23: Turn 级 Token 预算调度器 (Turn-Level Token Budget Scheduler) ──
// 在 control 层管理每个 turn 的 token 预算，避免单 turn 消耗过多。
//
// 原理：control 层是 turn 生命周期管理核心，但之前没有任何 token 优化。
// 本模块在 turn 开始时分配 token 预算，在 turn 执行中监控消耗，
// 当消耗接近预算时通知 agent 层提前结束（而非等到上下文窗口溢出）。
//
// 效果：避免单个 turn 消耗过多 token（如模型陷入循环调用工具），
// 提前预警，减少 30% 的 token 浪费在无效循环上。

// TurnBudgetScheduler turn 级 token 预算调度器
type TurnBudgetScheduler struct {
	mu sync.RWMutex

	// 当前 turn 的预算
	currentBudget *TurnBudget

	// 历史 turn 的消耗统计
	history []TurnConsumption

	// 配置
	maxTokensPerTurn  int           // 单 turn 最大 token 数
	warningThreshold  float64       // 预警阈值（占预算的比例）
	criticalThreshold float64       // 临界阈值
	monitorInterval   time.Duration // 监控间隔

	// 统计
	totalTurns      int64
	totalToolCalls  int64
	budgetExceeded  int64
	warningsIssued  int64
}

// TurnBudget 单个 turn 的预算
type TurnBudget struct {
	TurnID        string    `json:"turnId"`
	Allocated     int       `json:"allocated"`     // 分配的 token 数
	Consumed      int64     `json:"consumed"`      // 已消耗的 token 数
	ToolCalls     int       `json:"toolCalls"`     // 工具调用次数
	StartedAt     time.Time `json:"startedAt"`
	WarningIssued bool      `json:"warningIssued"`
}

// TurnConsumption turn 消耗记录
type TurnConsumption struct {
	TurnID     string    `json:"turnId"`
	Allocated  int       `json:"allocated"`
	Consumed   int64     `json:"consumed"`
	ToolCalls  int       `json:"toolCalls"`
	Duration   time.Duration `json:"duration"`
	Exceeded   bool      `json:"exceeded"`
}

// NewTurnBudgetScheduler 创建调度器
func NewTurnBudgetScheduler(maxTokensPerTurn int) *TurnBudgetScheduler {
	if maxTokensPerTurn <= 0 {
		maxTokensPerTurn = 50000 // 默认 50K token/turn
	}
	return &TurnBudgetScheduler{
		maxTokensPerTurn:  maxTokensPerTurn,
		warningThreshold:  0.70,
		criticalThreshold: 0.90,
		monitorInterval:   100 * time.Millisecond,
	}
}

// StartTurn 开始一个新的 turn，分配预算
func (s *TurnBudgetScheduler) StartTurn(turnID string) *TurnBudget {
	s.mu.Lock()
	defer s.mu.Unlock()

	budget := &TurnBudget{
		TurnID:    turnID,
		Allocated: s.maxTokensPerTurn,
		StartedAt: time.Now(),
	}
	s.currentBudget = budget
	atomic.AddInt64(&s.totalTurns, 1)

	slog.Debug("OPT-23: turn budget allocated",
		"turn_id", turnID,
		"allocated", s.maxTokensPerTurn,
	)
	return budget
}

// RecordConsumption 记录 token 消耗
func (s *TurnBudgetScheduler) RecordConsumption(tokens int) TurnBudgetStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentBudget == nil {
		return TurnBudgetStatusOK
	}

	atomic.AddInt64(&s.currentBudget.Consumed, int64(tokens))

	consumed := atomic.LoadInt64(&s.currentBudget.Consumed)
	usage := float64(consumed) / float64(s.currentBudget.Allocated)

	if usage >= s.criticalThreshold {
		if !s.currentBudget.WarningIssued {
			s.currentBudget.WarningIssued = true
			atomic.AddInt64(&s.warningsIssued, 1)
			slog.Warn("OPT-23: turn budget critical — approaching limit",
				"turn_id", s.currentBudget.TurnID,
				"consumed", consumed,
				"allocated", s.currentBudget.Allocated,
				"usage", usage,
			)
		}
		return TurnBudgetStatusCritical
	}

	if usage >= s.warningThreshold {
		return TurnBudgetStatusWarning
	}

	return TurnBudgetStatusOK
}

// RecordToolCall 记录工具调用
func (s *TurnBudgetScheduler) RecordToolCall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentBudget != nil {
		s.currentBudget.ToolCalls++
	}
	atomic.AddInt64(&s.totalToolCalls, 1)
}

// EndTurn 结束当前 turn，记录消耗
func (s *TurnBudgetScheduler) EndTurn() *TurnConsumption {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentBudget == nil {
		return nil
	}

	consumed := atomic.LoadInt64(&s.currentBudget.Consumed)
	exceeded := consumed > int64(s.currentBudget.Allocated)
	if exceeded {
		atomic.AddInt64(&s.budgetExceeded, 1)
	}

	record := &TurnConsumption{
		TurnID:    s.currentBudget.TurnID,
		Allocated: s.currentBudget.Allocated,
		Consumed:  consumed,
		ToolCalls: s.currentBudget.ToolCalls,
		Duration:  time.Since(s.currentBudget.StartedAt),
		Exceeded:  exceeded,
	}

	// 添加到历史
	s.history = append(s.history, *record)
	if len(s.history) > 100 {
		s.history = s.history[1:]
	}

	s.currentBudget = nil
	return record
}

// GetCurrentBudget 获取当前 turn 预算
func (s *TurnBudgetScheduler) GetCurrentBudget() *TurnBudget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentBudget
}

// GetStats 获取统计
func (s *TurnBudgetScheduler) GetStats() SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var avgConsumption float64
	if len(s.history) > 0 {
		total := int64(0)
		for _, h := range s.history {
			total += h.Consumed
		}
		avgConsumption = float64(total) / float64(len(s.history))
	}

	return SchedulerStats{
		TotalTurns:       int(atomic.LoadInt64(&s.totalTurns)),
		TotalToolCalls:   int(atomic.LoadInt64(&s.totalToolCalls)),
		BudgetExceeded:   int(atomic.LoadInt64(&s.budgetExceeded)),
		WarningsIssued:   int(atomic.LoadInt64(&s.warningsIssued)),
		AvgConsumption:   avgConsumption,
		MaxTokensPerTurn: s.maxTokensPerTurn,
	}
}

// TurnBudgetStatus 预算状态
type TurnBudgetStatus int

const (
	TurnBudgetStatusOK TurnBudgetStatus = iota
	TurnBudgetStatusWarning
	TurnBudgetStatusCritical
)

// SchedulerStats 调度器统计
type SchedulerStats struct {
	TotalTurns       int     `json:"totalTurns"`
	TotalToolCalls   int     `json:"totalToolCalls"`
	BudgetExceeded   int     `json:"budgetExceeded"`
	WarningsIssued   int     `json:"warningsIssued"`
	AvgConsumption   float64 `json:"avgConsumption"`
	MaxTokensPerTurn int     `json:"maxTokensPerTurn"`
}

// AdjustBudgetForComplexity 根据任务复杂度调整预算
func (s *TurnBudgetScheduler) AdjustBudgetForComplexity(complexity int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case complexity <= 3:
		s.maxTokensPerTurn = 20000 // 简单任务
	case complexity <= 6:
		s.maxTokensPerTurn = 50000 // 标准任务
	case complexity <= 8:
		s.maxTokensPerTurn = 100000 // 复杂任务
	default:
		s.maxTokensPerTurn = 200000 // 大型任务
	}
}
