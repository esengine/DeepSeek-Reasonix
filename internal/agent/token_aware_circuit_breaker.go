package agent

import "sync"

// ── OPT-199: TokenAwareCircuitBreaker (Token感知熔断器) ──
// 在 token 消耗异常时触发熔断保护，防止系统过载。
// 支持三种状态：closed（正常）、open（熔断）、half-open（半开探测）。
// 当失败次数达到阈值时触发熔断进入 open 状态，冷却期过后转入 half-open
// 进行探测，探测成功则恢复 closed，探测失败则重新熔断。

// TokenAwareCircuitBreaker Token感知熔断器
type TokenAwareCircuitBreaker struct {
	mu               sync.RWMutex
	state            string // "closed", "open", "half-open"
	failureThreshold int
	failureCount     int
	successCount     int
	lastFailure      int // 上一次失败的时间戳
	cooldownPeriod   int // 冷却周期
	trips            int // 熔断次数
}

// NewTokenAwareCircuitBreaker 创建一个新的Token感知熔断器。
// failureThreshold 为触发熔断的失败次数阈值，
// cooldownPeriod 为熔断后的冷却周期，state 初始为 "closed"。
func NewTokenAwareCircuitBreaker(failureThreshold int, cooldownPeriod int) *TokenAwareCircuitBreaker {
	return &TokenAwareCircuitBreaker{
		state:            "closed",
		failureThreshold: failureThreshold,
		cooldownPeriod:   cooldownPeriod,
	}
}

// Allow 检查是否允许请求通过。
// closed 状态返回 true，open 状态返回 false，half-open 状态返回 true。
func (c *TokenAwareCircuitBreaker) Allow() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	switch c.state {
	case "closed":
		return true
	case "open":
		return false
	case "half-open":
		return true
	default:
		return true
	}
}

// RecordSuccess 记录一次成功。
// 若当前处于 half-open 状态，转为 closed 并重置失败计数。
// 递增 successCount。
func (c *TokenAwareCircuitBreaker) RecordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.successCount++

	if c.state == "half-open" {
		c.state = "closed"
		c.failureCount = 0
	}
}

// RecordFailure 记录一次失败，超过阈值时熔断。
// currentTime 为当前时间戳。若处于 open 状态且冷却期已过，
// 先转为 half-open 再记录失败。失败次数达到阈值时进入 open 状态
// 并递增 trips。
func (c *TokenAwareCircuitBreaker) RecordFailure(currentTime int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 若处于 open 状态，检查冷却期是否已过
	if c.state == "open" {
		if tacbShouldReset(c.lastFailure, currentTime, c.cooldownPeriod) {
			c.state = "half-open"
		} else {
			return
		}
	}

	c.failureCount++
	c.lastFailure = currentTime

	if tacbShouldTrip(c.failureCount, c.failureThreshold) {
		c.state = "open"
		c.trips++
	}
}

// GetState 获取当前熔断器状态。
func (c *TokenAwareCircuitBreaker) GetState() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.state
}

// GetStats 返回熔断器的统计信息。
// 包含: state, failureThreshold, failureCount, successCount, trips。
func (c *TokenAwareCircuitBreaker) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"state":            c.state,
		"failureThreshold": c.failureThreshold,
		"failureCount":     c.failureCount,
		"successCount":     c.successCount,
		"trips":            c.trips,
	}
}

// Reset 重置熔断器，恢复初始状态。
func (c *TokenAwareCircuitBreaker) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.state = "closed"
	c.failureCount = 0
	c.successCount = 0
	c.lastFailure = 0
	c.trips = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tacb 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tacbShouldTrip 判断是否应该触发熔断。
// 条件: failureCount >= failureThreshold。
func tacbShouldTrip(failureCount int, failureThreshold int) bool {
	return failureCount >= failureThreshold
}

// tacbShouldReset 判断是否应该从 open 转为 half-open（冷却期已过）。
// 条件: currentTime - lastFailure >= cooldownPeriod。
func tacbShouldReset(lastFailure int, currentTime int, cooldownPeriod int) bool {
	return currentTime-lastFailure >= cooldownPeriod
}
