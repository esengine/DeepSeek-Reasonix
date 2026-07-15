package agent
import "sync"

// ── OPT-241: TokenAwareOverflowSpillway (Token感知溢出溢洪道 / Token-Aware Overflow Spillway) ──
// 在 token 负载超出主通道容量时，将溢出的 token 请求引导到备用通道，
// 实现负载均衡与降级保护，避免主链路过载。
// 溢出时按顺序选择第一个可用的备用通道进行疏导。

// TokenAwareOverflowSpillway Token感知溢出溢洪道
type TokenAwareOverflowSpillway struct {
	mu                 sync.RWMutex
	capacity           int    // 主通道容量
	currentLoad        int    // 当前负载（累计溢出的 token 数）
	spilledCount       int    // 溢出事件次数
	totalSpilledTokens int    // 累计溢出的 token 总量
	channels           []string // 备用通道列表
}

// NewTokenAwareOverflowSpillway 创建一个新的 Token 感知溢出溢洪道实例。
// capacity 指定主通道容量，用于计算利用率。
func NewTokenAwareOverflowSpillway(capacity int) *TokenAwareOverflowSpillway {
	return &TokenAwareOverflowSpillway{
		capacity: capacity,
		channels: make([]string, 0),
	}
}

// AddChannel 添加一个备用通道。
// name 为备用通道名称。
func (t *TokenAwareOverflowSpillway) AddChannel(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.channels = append(t.channels, name)
}

// Spill 将指定数量的 token 溢出到第一个可用通道。
// 返回承接溢出的通道名与是否成功；无可用通道时返回 ("", false)。
func (t *TokenAwareOverflowSpillway) Spill(tokens int) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	channel, ok := taosFindChannel(t.channels)
	if !ok {
		return "", false
	}
	t.currentLoad += tokens
	t.spilledCount++
	t.totalSpilledTokens += tokens
	return channel, true
}

// GetCurrentLoad 获取当前累计负载（已溢出的 token 数）。
func (t *TokenAwareOverflowSpillway) GetCurrentLoad() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentLoad
}

// GetUtilization 获取利用率（当前负载 / 容量）。
// 容量非正时返回 0。
func (t *TokenAwareOverflowSpillway) GetUtilization() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.capacity <= 0 {
		return 0
	}
	return float64(t.currentLoad) / float64(t.capacity)
}

// GetStats 获取统计信息。
// 返回 capacity、currentLoad、channelCount、spilledCount、totalSpilledTokens、utilization。
func (t *TokenAwareOverflowSpillway) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	utilization := 0.0
	if t.capacity > 0 {
		utilization = float64(t.currentLoad) / float64(t.capacity)
	}
	return map[string]interface{}{
		"capacity":           t.capacity,
		"currentLoad":        t.currentLoad,
		"channelCount":       len(t.channels),
		"spilledCount":       t.spilledCount,
		"totalSpilledTokens": t.totalSpilledTokens,
		"utilization":        utilization,
	}
}

// Reset 重置累计统计信息（负载、溢出次数、溢出 token 总量）。
// 保留容量与已注册的备用通道配置。
func (t *TokenAwareOverflowSpillway) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentLoad = 0
	t.spilledCount = 0
	t.totalSpilledTokens = 0
}

// taosFindChannel 辅助函数，查找第一个可用的备用通道。
// 返回通道名与是否找到；通道列表为空时返回 ("", false)。
func taosFindChannel(channels []string) (string, bool) {
	if len(channels) == 0 {
		return "", false
	}
	return channels[0], true
}
