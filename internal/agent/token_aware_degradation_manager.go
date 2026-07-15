package agent
import "sync"

// ── OPT-229: TokenAwareDegradationManager (Token感知降级管理器) ──
// TokenAwareDegradationManager 在 token 压力下逐步降级服务质量，
// 根据 token 使用量与阈值比较返回降级级别。
type TokenAwareDegradationManager struct {
	mu             sync.RWMutex
	level          int
	thresholds     []int
	degradedCount  int
	recoveredCount int
	activeLevel    int
}

// NewTokenAwareDegradationManager 创建 Token 感知降级管理器。
// thresholds 按升序排列，表示触发各级降级的 token 使用量阈值。
func NewTokenAwareDegradationManager(thresholds []int) *TokenAwareDegradationManager {
	sorted := make([]int, len(thresholds))
	copy(sorted, thresholds)
	// 保证阈值升序排列
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	return &TokenAwareDegradationManager{
		thresholds: sorted,
	}
}

// Check 根据 token 使用量检查并返回当前降级级别。
// 返回 0 表示正常，返回值越大降级越深。
// 同时累计降级/恢复次数。
func (m *TokenAwareDegradationManager) Check(tokenUsage int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	level := tadmFindLevel(m.thresholds, tokenUsage)
	if level > m.activeLevel {
		m.degradedCount += level - m.activeLevel
	} else if level < m.activeLevel {
		m.recoveredCount += m.activeLevel - level
	}
	m.activeLevel = level
	m.level = level
	return level
}

// Degrade 手动降级一级，不超过阈值数量上限。
func (m *TokenAwareDegradationManager) Degrade() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.level < len(m.thresholds) {
		m.level++
		m.activeLevel = m.level
		m.degradedCount++
	}
}

// Recover 手动恢复一级，不低于 0。
func (m *TokenAwareDegradationManager) Recover() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.level > 0 {
		m.level--
		m.activeLevel = m.level
		m.recoveredCount++
	}
}

// GetLevel 返回当前降级级别。
func (m *TokenAwareDegradationManager) GetLevel() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.level
}

// GetStats 返回降级管理器的统计信息。
func (m *TokenAwareDegradationManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]interface{}{
		"level":          m.level,
		"activeLevel":    m.activeLevel,
		"degradedCount":  m.degradedCount,
		"recoveredCount": m.recoveredCount,
		"thresholdCount": len(m.thresholds),
	}
}

// Reset 重置降级管理器，级别归零，保留阈值配置。
func (m *TokenAwareDegradationManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.level = 0
	m.activeLevel = 0
	m.degradedCount = 0
	m.recoveredCount = 0
}

// tadmFindLevel 根据升序阈值列表与 token 使用量确定降级级别。
// tokenUsage 达到多少个阈值即为多少级。
func tadmFindLevel(thresholds []int, tokenUsage int) int {
	level := 0
	for i, t := range thresholds {
		if tokenUsage >= t {
			level = i + 1
		} else {
			break
		}
	}
	return level
}
