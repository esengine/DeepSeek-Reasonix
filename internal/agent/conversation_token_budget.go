package agent

import (
	"sync"
)

// ── OPT-98: ConversationTokenBudget (对话级 Token 预算管理) ──
// 管理跨对话轮次持久存在的 token 预算，确保整个对话不会超出
// 预设的 token 总量。
//
// 原理：
//   - Allocate 在每轮对话中分配 token，检查是否在剩余预算内
//   - Reserve 预留 token 供未来使用（如预计的工具调用开销）
//   - Release 释放之前预留的 token
//   - ShouldEndConversation 在剩余预算低于总量 10% 时建议结束对话
//   - 每次分配都会记录快照，用于预算历史分析
//
// 效果：防止单次对话消耗过多 token，在预算耗尽前主动提醒结束对话。

// BudgetSnapshot 预算快照，记录某一轮次的预算状态。
type BudgetSnapshot struct {
	Turn      int // 对话轮次
	Budget    int // 总预算
	Used      int // 已使用 token
	Remaining int // 剩余 token
}

// ConvBudgetStats 对话预算统计信息。
type ConvBudgetStats struct {
	TotalBudget      int     // 总预算
	UsedTokens       int     // 已使用 token
	ReservedTokens   int     // 已预留 token
	TurnsTracked     int     // 已跟踪的轮次数
	OverBudgetCount  int     // 超出预算次数
	UtilizationRate  float64 // 利用率（0-1）
}

// ConversationTokenBudget 管理对话级别的 token 预算。
type ConversationTokenBudget struct {
	mu              sync.RWMutex
	totalBudget     int
	usedTokens      int
	reservedTokens  int
	turnsTracked    int
	budgetHistory   []BudgetSnapshot
	overBudgetCount int
}

// NewConversationTokenBudget 创建一个新的 ConversationTokenBudget 实例。
func NewConversationTokenBudget(totalBudget int) *ConversationTokenBudget {
	return &ConversationTokenBudget{
		totalBudget: totalBudget,
	}
}

// Allocate 检查并分配 token。
// 如果请求的 token 数在剩余预算内，则扣减并记录快照，返回 true。
// 如果超出预算，增加超预算计数，返回 false。
func (b *ConversationTokenBudget) Allocate(turn int, tokens int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := b.totalBudget - b.usedTokens - b.reservedTokens
	if tokens > remaining {
		b.overBudgetCount++
		b.budgetHistory = append(b.budgetHistory, BudgetSnapshot{
			Turn:      turn,
			Budget:    b.totalBudget,
			Used:      b.usedTokens,
			Remaining: remaining,
		})
		b.turnsTracked++
		return false
	}

	b.usedTokens += tokens
	b.budgetHistory = append(b.budgetHistory, BudgetSnapshot{
		Turn:      turn,
		Budget:    b.totalBudget,
		Used:      b.usedTokens,
		Remaining: b.totalBudget - b.usedTokens - b.reservedTokens,
	})
	b.turnsTracked++

	return true
}

// GetRemaining 返回剩余可用 token（总预算 - 已使用 - 已预留）。
func (b *ConversationTokenBudget) GetRemaining() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.totalBudget - b.usedTokens - b.reservedTokens
}

// Reserve 预留 token 供未来使用。
// 如果剩余空间不足，返回 false。
func (b *ConversationTokenBudget) Reserve(tokens int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := b.totalBudget - b.usedTokens - b.reservedTokens
	if tokens > remaining {
		return false
	}

	b.reservedTokens += tokens
	return true
}

// Release 释放之前预留的 token。
// 释放量不会超过当前预留量。
func (b *ConversationTokenBudget) Release(tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if tokens > b.reservedTokens {
		tokens = b.reservedTokens
	}
	b.reservedTokens -= tokens
}

// ShouldEndConversation 返回 true 如果剩余预算低于总预算的 10%。
func (b *ConversationTokenBudget) ShouldEndConversation() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	remaining := b.totalBudget - b.usedTokens - b.reservedTokens
	threshold := b.totalBudget / 10
	return remaining < threshold
}

// GetStats 返回对话预算的统计信息。
func (b *ConversationTokenBudget) GetStats() ConvBudgetStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var utilization float64
	if b.totalBudget > 0 {
		utilization = float64(b.usedTokens) / float64(b.totalBudget)
	}

	return ConvBudgetStats{
		TotalBudget:     b.totalBudget,
		UsedTokens:      b.usedTokens,
		ReservedTokens:  b.reservedTokens,
		TurnsTracked:    b.turnsTracked,
		OverBudgetCount: b.overBudgetCount,
		UtilizationRate: utilization,
	}
}

// Reset 重置预算到初始状态。
func (b *ConversationTokenBudget) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.usedTokens = 0
	b.reservedTokens = 0
	b.turnsTracked = 0
	b.budgetHistory = b.budgetHistory[:0]
	b.overBudgetCount = 0
}
