package agent

import "sync"

// ── OPT-256: TokenAwareGracefulDegrader (Token 感知优雅降级器) ──
// 在 Token 预算紧张时按级别优雅降级非核心功能，并在预算恢复后逐级恢复。
// 通过 activeFeatures 注册表记录各功能的启用/禁用状态，便于统计当前被关闭的功能数量。

// TokenAwareGracefulDegrader Token 感知优雅降级器，按级别管理降级与功能开关。
type TokenAwareGracefulDegrader struct {
	mu             sync.RWMutex
	level          int             // 当前降级级别（0 表示未降级）
	maxLevel       int             // 允许的最大降级级别
	degradedSteps  int             // 累计降级步数
	recoveredSteps int             // 累计恢复步数
	activeFeatures map[string]bool // 功能注册表：true=启用，false=禁用
}

// NewTokenAwareGracefulDegrader 创建一个新的 Token 感知优雅降级器。
// maxLevel 指定允许的最大降级级别，若 < 0 则视为 0。
func NewTokenAwareGracefulDegrader(maxLevel int) *TokenAwareGracefulDegrader {
	if maxLevel < 0 {
		maxLevel = 0
	}
	return &TokenAwareGracefulDegrader{
		level:          0,
		maxLevel:       maxLevel,
		degradedSteps:  0,
		recoveredSteps: 0,
		activeFeatures: make(map[string]bool),
	}
}

// Degrade 将降级级别提升一级，返回当前级别。
// 已达到 maxLevel 时不再提升。
func (d *TokenAwareGracefulDegrader) Degrade() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.level < d.maxLevel {
		d.level++
		d.degradedSteps++
	}
	return d.level
}

// Recover 将降级级别降低一级，返回当前级别。
// 已为 0 时不再降低。
func (d *TokenAwareGracefulDegrader) Recover() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.level > 0 {
		d.level--
		d.recoveredSteps++
	}
	return d.level
}

// DisableFeature 禁用指定名称的功能。
func (d *TokenAwareGracefulDegrader) DisableFeature(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.activeFeatures[name] = false
}

// EnableFeature 启用指定名称的功能。
func (d *TokenAwareGracefulDegrader) EnableFeature(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.activeFeatures[name] = true
}

// IsFeatureEnabled 返回指定功能是否启用。
// 未注册的功能默认视为启用。
func (d *TokenAwareGracefulDegrader) IsFeatureEnabled(name string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	enabled, ok := d.activeFeatures[name]
	if !ok {
		return true
	}
	return enabled
}

// GetLevel 返回当前降级级别。
func (d *TokenAwareGracefulDegrader) GetLevel() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.level
}

// GetStats 返回降级器的统计信息。
// 包含: level, maxLevel, degradedSteps, recoveredSteps, disabledFeatureCount。
func (d *TokenAwareGracefulDegrader) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]interface{}{
		"level":                d.level,
		"maxLevel":             d.maxLevel,
		"degradedSteps":        d.degradedSteps,
		"recoveredSteps":       d.recoveredSteps,
		"disabledFeatureCount": tagdCountDisabled(d.activeFeatures),
	}
}

// Reset 重置降级器，清空级别、步数与功能开关，保留 maxLevel 配置。
func (d *TokenAwareGracefulDegrader) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.level = 0
	d.degradedSteps = 0
	d.recoveredSteps = 0
	d.activeFeatures = make(map[string]bool)
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tagd 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tagdCountDisabled 统计当前被禁用的功能数量。
func tagdCountDisabled(features map[string]bool) int {
	count := 0
	for _, enabled := range features {
		if !enabled {
			count++
		}
	}
	return count
}
