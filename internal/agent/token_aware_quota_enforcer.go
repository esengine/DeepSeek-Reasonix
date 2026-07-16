package agent

import "sync"

// OPT-246: TokenAwareQuotaEnforcer / Token感知配额强制器
// 强制执行token配额限制，按租户维度追踪配额与使用量。
type TokenAwareQuotaEnforcer struct {
	mu            sync.RWMutex
	quotas        map[string]int // tenant -> quota 租户配额
	usage         map[string]int // tenant -> usage 租户使用量
	violations    int            // 违规次数
	totalEnforced int            // 累计强制检查次数
}

// NewTokenAwareQuotaEnforcer 创建一个新的Token感知配额强制器。
func NewTokenAwareQuotaEnforcer() *TokenAwareQuotaEnforcer {
	return &TokenAwareQuotaEnforcer{
		quotas: make(map[string]int),
		usage:  make(map[string]int),
	}
}

// SetQuota 设置指定租户的token配额。
func (t *TokenAwareQuotaEnforcer) SetQuota(tenant string, quota int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.quotas[tenant] = quota
}

// Enforce 强制配额检查，若加上本次token后未超过配额则允许并累加使用量，
// 否则记一次违规并拒绝。返回是否允许。
func (t *TokenAwareQuotaEnforcer) Enforce(tenant string, tokens int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalEnforced++

	quota, ok := t.quotas[tenant]
	if !ok {
		// 未设置配额的租户不限制
		t.usage[tenant] += tokens
		return true
	}
	if t.usage[tenant]+tokens > quota {
		t.violations++
		return false
	}
	t.usage[tenant] += tokens
	return true
}

// GetUsage 获取指定租户的当前token使用量。
func (t *TokenAwareQuotaEnforcer) GetUsage(tenant string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.usage[tenant]
}

// GetViolationCount 获取累计违规次数。
func (t *TokenAwareQuotaEnforcer) GetViolationCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.violations
}

// GetStats 获取统计信息，包含 tenantCount、violations、totalEnforced、totalQuota。
func (t *TokenAwareQuotaEnforcer) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"tenantCount":   len(t.quotas),
		"violations":    t.violations,
		"totalEnforced": t.totalEnforced,
		"totalQuota":    taqeSumValues(t.quotas),
	}
}

// Reset 重置所有状态，包括配额、使用量与计数器。
func (t *TokenAwareQuotaEnforcer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.quotas = make(map[string]int)
	t.usage = make(map[string]int)
	t.violations = 0
	t.totalEnforced = 0
}

// taqeSumValues 计算map中所有int值的总和（辅助函数）。
func taqeSumValues(m map[string]int) int {
	sum := 0
	for _, v := range m {
		sum += v
	}
	return sum
}
