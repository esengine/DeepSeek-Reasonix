package agent
import "sync"

// OPT-191: TokenAwareQuotaManager / Token感知配额管理器
// 管理多租户的token配额，支持配额设置、消耗、补充等操作。

// TokenAwareQuotaManager Token感知配额管理器，管理多租户的token配额
type TokenAwareQuotaManager struct {
	mu             sync.RWMutex
	quotas         map[string]int // tenant->remaining
	limits         map[string]int // tenant->limit
	totalAllocated int
	totalConsumed  int
}

// NewTokenAwareQuotaManager 创建一个新的Token感知配额管理器
func NewTokenAwareQuotaManager() *TokenAwareQuotaManager {
	return &TokenAwareQuotaManager{
		quotas: make(map[string]int),
		limits: make(map[string]int),
	}
}

// SetQuota 设置租户配额
func (m *TokenAwareQuotaManager) SetQuota(tenant string, limit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 如果租户已存在，先减去旧的分配和消耗
	if oldLimit, ok := m.limits[tenant]; ok {
		oldConsumed := oldLimit - m.quotas[tenant]
		m.totalAllocated -= oldLimit
		m.totalConsumed -= oldConsumed
	}
	m.limits[tenant] = limit
	m.quotas[tenant] = limit
	m.totalAllocated += limit
}

// Consume 消耗配额，返回是否成功
func (m *TokenAwareQuotaManager) Consume(tenant string, tokens int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	remaining, ok := m.quotas[tenant]
	if !ok || remaining < tokens {
		return false
	}
	m.quotas[tenant] = remaining - tokens
	m.totalConsumed += tokens
	return true
}

// GetRemaining 获取剩余配额
func (m *TokenAwareQuotaManager) GetRemaining(tenant string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.quotas[tenant]
}

// Refill 补充配额
func (m *TokenAwareQuotaManager) Refill(tenant string, tokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.quotas[tenant]; !ok {
		return
	}
	m.quotas[tenant] += tokens
	m.totalConsumed -= tokens
	if m.totalConsumed < 0 {
		m.totalConsumed = 0
	}
}

// GetStats 返回统计信息，包括 tenantCount, totalAllocated, totalConsumed, totalRemaining
func (m *TokenAwareQuotaManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]interface{}{
		"tenantCount":    len(m.quotas),
		"totalAllocated": m.totalAllocated,
		"totalConsumed":  m.totalConsumed,
		"totalRemaining": taqmSumValues(m.quotas),
	}
}

// Reset 重置管理器，清空所有配额数据
func (m *TokenAwareQuotaManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quotas = make(map[string]int)
	m.limits = make(map[string]int)
	m.totalAllocated = 0
	m.totalConsumed = 0
}

// taqmSumValues 辅助函数，计算map中所有值的总和
func taqmSumValues(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}
