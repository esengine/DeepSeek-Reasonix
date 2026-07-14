package agent
import "sync"

// ── OPT-184: TokenAwareGatekeeper (Token 感知守门员 / Token-Aware Gatekeeper) ──
// 在 token 预算耗尽前阻止新请求。Allow 在剩余预算充足时消耗对应 token
// 并放行，否则拒绝并计数；Peek 仅检查不消耗。提供利用率与告警阈值
// 便于上游在接近预算上限时提前降级。

// TokenAwareGatekeeper Token 感知守门员。
type TokenAwareGatekeeper struct {
	mu               sync.RWMutex
	totalBudget      int
	consumedTokens   int
	blockedCount     int
	allowedCount     int
	warningThreshold float64
}

// NewTokenAwareGatekeeper 创建 Token 感知守门员。
// totalBudget 为 token 总预算，warningThreshold 为告警利用率阈值（0~1）。
func NewTokenAwareGatekeeper(totalBudget int, warningThreshold float64) *TokenAwareGatekeeper {
	return &TokenAwareGatekeeper{
		totalBudget:      totalBudget,
		warningThreshold: warningThreshold,
	}
}

// Allow 检查并消耗 token 预算。
// 若剩余预算 >= tokens 则消耗并放行（allowedCount++），返回 true；
// 否则拒绝（blockedCount++），返回 false。
func (g *TokenAwareGatekeeper) Allow(tokens int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.consumedTokens+tokens > g.totalBudget {
		g.blockedCount++
		return false
	}
	g.consumedTokens += tokens
	g.allowedCount++
	return true
}

// Peek 检查是否能在不超出预算的前提下容纳 tokens，但不实际消耗。
func (g *TokenAwareGatekeeper) Peek(tokens int) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.consumedTokens+tokens <= g.totalBudget
}

// GetRemainingBudget 返回剩余 token 预算。
func (g *TokenAwareGatekeeper) GetRemainingBudget() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.totalBudget - g.consumedTokens
}

// GetUtilization 返回当前预算利用率（已消耗 / 总预算）。
// 若总预算 <= 0 则返回 0。
func (g *TokenAwareGatekeeper) GetUtilization() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return tagkComputeUtilization(g.consumedTokens, g.totalBudget)
}

// GetStats 返回守门员统计信息，包括 totalBudget、consumedTokens、
// remainingBudget、utilization、blockedCount 与 allowedCount。
func (g *TokenAwareGatekeeper) GetStats() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return map[string]interface{}{
		"totalBudget":     g.totalBudget,
		"consumedTokens":  g.consumedTokens,
		"remainingBudget": g.totalBudget - g.consumedTokens,
		"utilization":     tagkComputeUtilization(g.consumedTokens, g.totalBudget),
		"blockedCount":    g.blockedCount,
		"allowedCount":    g.allowedCount,
	}
}

// Reset 重置守门员状态，清空已消耗 token 与计数（保留预算与阈值配置）。
func (g *TokenAwareGatekeeper) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.consumedTokens = 0
	g.blockedCount = 0
	g.allowedCount = 0
}

// tagkComputeUtilization 计算 token 预算利用率。
// 返回 consumed / total；若 total <= 0 则返回 0。
func tagkComputeUtilization(consumed int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(consumed) / float64(total)
}
