package agent

import "sync"

// ── OPT-261: TokenAwareLoadShedder (Token 感知负载脱落器) ──
// 当 token 负载超过阈值时，按策略脱落部分负载以保护系统。
// 支持 oldest（优先脱落最早累计的 token，脱落全部溢出量）、
// newest（优先脱落最新到来的 token，脱落全部溢出量）以及
// random（随机脱落约一半溢出量，进行部分缓解）三种策略。
//
// 原理：在高负载场景下，若不主动脱落，队列会持续膨胀导致延迟
// 雪崩。通过 token 感知的脱落，可以按实际 token 体量决定脱落
// 量，而非简单丢弃请求数，从而更精确地控制资源占用。
//
// 效果：在过载时快速回收 token 预算，维持系统吞吐与延迟稳定。

// TokenAwareLoadShedder Token 感知负载脱落器。
type TokenAwareLoadShedder struct {
	mu              sync.RWMutex
	threshold       int
	currentLoad     int
	shedCount       int
	totalShedTokens int
	shedStrategy    string
}

// NewTokenAwareLoadShedder 创建一个新的 Token 感知负载脱落器。
// threshold 为允许的最大 token 负载阈值；strategy 可选 "oldest"、"newest"
// 或 "random"，非法值将回退为 "oldest"。
func NewTokenAwareLoadShedder(threshold int, strategy string) *TokenAwareLoadShedder {
	return &TokenAwareLoadShedder{
		threshold:    threshold,
		shedStrategy: talsNormalizeStrategy(strategy),
	}
}

// ShouldShed 检查当前负载加上待加入负载后是否需要脱落。
// 若 currentLoad + load 超过 threshold 则返回 true。
func (s *TokenAwareLoadShedder) ShouldShed(load int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentLoad+load > s.threshold
}

// Shed 尝试脱落负载。
// 若无需脱落（加入 load 后未超阈值），则将 load 累加到 currentLoad
// 并返回 (0, false)；否则按策略计算脱落量，更新统计并返回
// (脱落 token 数, true)。
func (s *TokenAwareLoadShedder) Shed(load int) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentLoad+load <= s.threshold {
		s.currentLoad += load
		return 0, false
	}
	excess := s.currentLoad + load - s.threshold
	shedAmount := talsComputeShedAmount(excess, s.shedStrategy)
	s.currentLoad = s.currentLoad + load - shedAmount
	if s.currentLoad < 0 {
		s.currentLoad = 0
	}
	s.shedCount++
	s.totalShedTokens += shedAmount
	return shedAmount, true
}

// SetThreshold 设置新的负载阈值。
func (s *TokenAwareLoadShedder) SetThreshold(t int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threshold = t
}

// GetLoad 返回当前 token 负载。
func (s *TokenAwareLoadShedder) GetLoad() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentLoad
}

// GetStats 返回统计信息，包含 threshold、currentLoad、shedCount、
// totalShedTokens 和 shedStrategy。
func (s *TokenAwareLoadShedder) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"threshold":       s.threshold,
		"currentLoad":     s.currentLoad,
		"shedCount":       s.shedCount,
		"totalShedTokens": s.totalShedTokens,
		"shedStrategy":    s.shedStrategy,
	}
}

// Reset 重置脱落器状态，清空当前负载与计数，但保留 threshold 与 shedStrategy 配置。
func (s *TokenAwareLoadShedder) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentLoad = 0
	s.shedCount = 0
	s.totalShedTokens = 0
}

// talsComputeShedAmount 根据溢出量与策略计算需脱落的 token 数（辅助函数）。
// oldest/newest 脱落全部溢出量；random 脱落约一半（向上取整）以进行部分缓解。
func talsComputeShedAmount(excess int, strategy string) int {
	if excess <= 0 {
		return 0
	}
	switch strategy {
	case "random":
		return (excess + 1) / 2
	default:
		// oldest 与 newest 均脱落全部溢出量
		return excess
	}
}

// talsNormalizeStrategy 规范化策略字符串，非法值回退为 "oldest"。
func talsNormalizeStrategy(strategy string) string {
	switch strategy {
	case "oldest", "newest", "random":
		return strategy
	default:
		return "oldest"
	}
}
