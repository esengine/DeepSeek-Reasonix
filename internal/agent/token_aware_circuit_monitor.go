package agent

import "sync"

// OPT-249: TokenAwareCircuitMonitor / Token感知回路监控器
// 监控token处理回路的状态，支持 closed/open/half-open 三态切换。
type TokenAwareCircuitMonitor struct {
	mu           sync.RWMutex
	circuits     map[string]string // circuitID -> state
	totalChecks  int
	stateChanges int
	alerts       int
}

// NewTokenAwareCircuitMonitor 创建一个新的Token感知回路监控器。
func NewTokenAwareCircuitMonitor() *TokenAwareCircuitMonitor {
	return &TokenAwareCircuitMonitor{
		circuits: make(map[string]string),
	}
}

// Register 注册一个回路，初始状态为 closed。
// 若回路已存在则不做任何改动。
func (t *TokenAwareCircuitMonitor) Register(circuitID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.circuits[circuitID]; !ok {
		t.circuits[circuitID] = "closed"
	}
}

// Check 检查回路状态，返回状态字符串。
// 未注册回路返回 "unknown"；对处于 open 状态的回路会增加告警计数。
func (t *TokenAwareCircuitMonitor) Check(circuitID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalChecks++
	state, ok := t.circuits[circuitID]
	if !ok {
		return "unknown"
	}
	if state == "open" {
		t.alerts++
	}
	return state
}

// SetState 设置回路状态（closed/open/half-open）。
// 状态发生变更时增加 stateChanges 计数。
func (t *TokenAwareCircuitMonitor) SetState(circuitID string, state string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if old, ok := t.circuits[circuitID]; ok {
		if old != state {
			t.stateChanges++
		}
	} else {
		t.stateChanges++
	}
	t.circuits[circuitID] = state
}

// GetState 获取回路状态，未注册返回 "unknown"。
func (t *TokenAwareCircuitMonitor) GetState(circuitID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	state, ok := t.circuits[circuitID]
	if !ok {
		return "unknown"
	}
	return state
}

// GetStats 获取统计信息，包含 circuitCount、totalChecks、stateChanges、alerts、openCircuits。
func (t *TokenAwareCircuitMonitor) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"circuitCount": len(t.circuits),
		"totalChecks":  t.totalChecks,
		"stateChanges": t.stateChanges,
		"alerts":       t.alerts,
		"openCircuits": tacmCountByState(t.circuits, "open"),
	}
}

// Reset 重置所有状态，清空回路表与计数器。
func (t *TokenAwareCircuitMonitor) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.circuits = make(map[string]string)
	t.totalChecks = 0
	t.stateChanges = 0
	t.alerts = 0
}

// tacmCountByState 统计map中处于指定状态的回路数量（辅助函数）。
func tacmCountByState(circuits map[string]string, state string) int {
	count := 0
	for _, s := range circuits {
		if s == state {
			count++
		}
	}
	return count
}
