package agent
import "sync"

// ── OPT-208: ContextFidelityMonitor (上下文保真度监控器 / Context Fidelity Monitor) ──
// 监控上下文处理过程中的信息损失。通过对比原始上下文与处理后的上下文，
// 计算保真度损失分数，确保压缩、裁剪等操作不会过度丢失关键信息。
//
// 原理：在上下文压缩、裁剪、摘要等处理环节中，不可避免地会产生信息损失。
// 通过量化损失分数（0=无损，1=完全损失），可以监控处理质量，
// 在损失过大时触发告警或回退策略。
//
// 效果：统计检查次数、损失次数、平均损失和最大损失，
// 为上下文处理质量评估提供数据支撑。

// ContextFidelityMonitor 上下文保真度监控器
type ContextFidelityMonitor struct {
	mu             sync.RWMutex
	checks         int     // 检查次数 number of checks
	fidelityLosses int     // 发生损失的次数 number of checks with loss
	totalLossScore float64 // 累计损失分数 total loss score
	maxLossScore   float64 // 最大损失分数 maximum loss score
}

// NewContextFidelityMonitor 创建上下文保真度监控器。
func NewContextFidelityMonitor() *ContextFidelityMonitor {
	return &ContextFidelityMonitor{}
}

// CheckFidelity 检查原始上下文与处理后上下文之间的保真度。
// 返回损失分数（0=无损，1=完全损失），并记录到监控统计中。
func (m *ContextFidelityMonitor) CheckFidelity(original string, processed string) float64 {
	score := cfmComputeLoss(original, processed)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.checks++
	m.totalLossScore += score
	if score > m.maxLossScore {
		m.maxLossScore = score
	}
	if score > 0 {
		m.fidelityLosses++
	}
	return score
}

// RecordLoss 记录一次已知的损失分数。
// 适用于外部已计算好损失分数的场景。
func (m *ContextFidelityMonitor) RecordLoss(score float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.checks++
	m.totalLossScore += score
	if score > m.maxLossScore {
		m.maxLossScore = score
	}
	if score > 0 {
		m.fidelityLosses++
	}
}

// GetAvgLoss 获取平均损失分数。若检查次数为 0 则返回 0。
func (m *ContextFidelityMonitor) GetAvgLoss() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.checks == 0 {
		return 0
	}
	return m.totalLossScore / float64(m.checks)
}

// GetMaxLoss 获取最大损失分数。
func (m *ContextFidelityMonitor) GetMaxLoss() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxLossScore
}

// GetStats 返回监控器的统计信息。
// 包含 checks、fidelityLosses、avgLoss 和 maxLoss。
func (m *ContextFidelityMonitor) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	avgLoss := float64(0)
	if m.checks > 0 {
		avgLoss = m.totalLossScore / float64(m.checks)
	}
	return map[string]interface{}{
		"checks":         m.checks,
		"fidelityLosses": m.fidelityLosses,
		"avgLoss":        avgLoss,
		"maxLoss":        m.maxLossScore,
	}
}

// Reset 重置监控器的所有计数和分数。
func (m *ContextFidelityMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks = 0
	m.fidelityLosses = 0
	m.totalLossScore = 0
	m.maxLossScore = 0
}

// cfmComputeLoss 计算原始上下文与处理后上下文之间的损失分数。
// 基于内容长度的比率进行估算：
//   - 若原始内容为空，损失为 0（无内容可损失）。
//   - 若处理后内容长度 >= 原始内容长度，损失为 0（无损失）。
//   - 否则损失 = 1 - len(processed) / len(original)，范围 [0, 1]。
func cfmComputeLoss(original string, processed string) float64 {
	if len(original) == 0 {
		return 0
	}
	processedLen := len(processed)
	if processedLen >= len(original) {
		return 0
	}
	return 1.0 - float64(processedLen)/float64(len(original))
}
