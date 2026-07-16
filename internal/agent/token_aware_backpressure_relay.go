package agent

import "sync"

// ── OPT-254: TokenAwareBackpressureRelay (Token感知背压中继器 / Token-Aware Backpressure Relay) ──
// 中继 token 流量，当负载超过容量时丢弃超出部分以施加背压，保护下游。
// 通过追踪当前负载和容量，Relay 在容量允许时中继 token，否则丢弃。
//
// 原理：类似水库中继，容量有限时多余的水被丢弃（溢流）。
// Relay 在 currentLoad + tokens <= capacity 时中继成功并累加负载，
// 否则丢弃并计入 droppedCount。Release 释放负载，降低 currentLoad。
//
// 效果：保护下游免受过载，统计中继、丢弃和总中继量，
// 为流量管理提供数据支撑。

// TokenAwareBackpressureRelay Token感知背压中继器
type TokenAwareBackpressureRelay struct {
	mu           sync.RWMutex
	capacity     int // 中继容量
	currentLoad  int // 当前负载
	relayCount   int // 成功中继次数
	droppedCount int // 丢弃次数
	totalRelayed int // 累计中继的 token 总数
}

// NewTokenAwareBackpressureRelay 创建 Token 感知背压中继器。
// capacity 指定中继容量，若 <= 0 则默认 10000。
func NewTokenAwareBackpressureRelay(capacity int) *TokenAwareBackpressureRelay {
	if capacity <= 0 {
		capacity = 10000
	}
	return &TokenAwareBackpressureRelay{
		capacity: capacity,
	}
}

// Relay 中继 token，超过容量则丢弃。
// 若 currentLoad + tokens <= capacity 则中继成功，递增 relayCount、
// 累加 totalRelayed 并返回 true；否则丢弃，递增 droppedCount 并返回 false。
func (r *TokenAwareBackpressureRelay) Relay(tokens int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if tokens <= 0 {
		return false
	}
	if r.currentLoad+tokens <= r.capacity {
		r.currentLoad += tokens
		r.relayCount++
		r.totalRelayed += tokens
		return true
	}
	r.droppedCount++
	return false
}

// Release 释放负载，降低 currentLoad（不低于 0）。
func (r *TokenAwareBackpressureRelay) Release(tokens int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if tokens <= 0 {
		return
	}
	r.currentLoad -= tokens
	if r.currentLoad < 0 {
		r.currentLoad = 0
	}
}

// GetLoad 获取当前负载。
func (r *TokenAwareBackpressureRelay) GetLoad() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentLoad
}

// GetUtilization 获取当前利用率（currentLoad / capacity）。
func (r *TokenAwareBackpressureRelay) GetUtilization() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return tabrComputeUtil(r.currentLoad, r.capacity)
}

// GetStats 返回中继器的统计信息。
// 包含 capacity、currentLoad、relayCount、droppedCount 和 totalRelayed。
func (r *TokenAwareBackpressureRelay) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]interface{}{
		"capacity":     r.capacity,
		"currentLoad":  r.currentLoad,
		"relayCount":   r.relayCount,
		"droppedCount": r.droppedCount,
		"totalRelayed": r.totalRelayed,
	}
}

// Reset 重置中继器的负载和计数（保留 capacity 配置）。
func (r *TokenAwareBackpressureRelay) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentLoad = 0
	r.relayCount = 0
	r.droppedCount = 0
	r.totalRelayed = 0
}

// tabrComputeUtil 计算利用率 = load / capacity。
// 若 capacity <= 0 则返回 0。
func tabrComputeUtil(load int, capacity int) float64 {
	if capacity <= 0 {
		return 0
	}
	return float64(load) / float64(capacity)
}
