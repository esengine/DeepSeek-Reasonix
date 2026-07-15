package agent
import "sync"

// ── OPT-231: TokenAwareOverflowHandler (Token感知溢出处理器 / Token-Aware Overflow Handler) ──
// 在 token 预算溢出时，根据配置策略处理超出部分。
// 支持 drop（丢弃）、defer（延迟）、compress（压缩）三种策略。
// 若单次溢出量超过 maxOverflow，无论策略如何均强制使用 drop 策略。

// TokenAwareOverflowHandler Token感知溢出处理器
type TokenAwareOverflowHandler struct {
	mu                  sync.RWMutex
	overflowCount       int    // 溢出次数
	totalOverflowTokens int    // 累计溢出 token 数
	maxOverflow         int    // 单次最大可处理溢出量
	strategy            string // 处理策略: drop/defer/compress
	droppedCount        int    // 被丢弃的次数
}

// NewTokenAwareOverflowHandler 创建一个新的 Token 感知溢出处理器。
// maxOverflow 指定单次最大可处理的溢出量，strategy 为处理策略，
// 可选值: "drop"（丢弃）、"defer"（延迟）、"compress"（压缩）。
func NewTokenAwareOverflowHandler(maxOverflow int, strategy string) *TokenAwareOverflowHandler {
	return &TokenAwareOverflowHandler{
		maxOverflow: maxOverflow,
		strategy:    strategy,
	}
}

// Handle 处理一次 token 溢出事件，返回实际使用的策略。
// 若溢出量超过 maxOverflow，强制返回 "drop"。
// 否则按配置的 strategy 返回对应策略。
func (h *TokenAwareOverflowHandler) Handle(excessTokens int) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 记录溢出统计
	h.overflowCount++
	h.totalOverflowTokens += excessTokens

	// 超出单次最大处理能力，强制丢弃
	if h.maxOverflow > 0 && excessTokens > h.maxOverflow {
		h.droppedCount++
		return "drop"
	}

	// 按配置策略处理
	switch h.strategy {
	case "drop":
		h.droppedCount++
		return "drop"
	case "defer":
		return "defer"
	case "compress":
		return "compress"
	default:
		h.droppedCount++
		return "drop"
	}
}

// CanHandle 检查是否能处理指定数量的溢出 token。
// 若 maxOverflow <= 0 表示无上限，始终返回 true。
func (h *TokenAwareOverflowHandler) CanHandle(excessTokens int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.maxOverflow <= 0 {
		return true
	}
	return excessTokens <= h.maxOverflow
}

// GetOverflowRatio 返回平均溢出比率。
// 比率 = (累计溢出 token / 溢出次数) / maxOverflow。
// 若无溢出记录或 maxOverflow 为 0，返回 0。
func (h *TokenAwareOverflowHandler) GetOverflowRatio() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return taohComputeRatio(h.totalOverflowTokens, h.overflowCount, h.maxOverflow)
}

// GetStats 返回溢出处理器的统计信息。
// 包含 overflowCount、totalOverflowTokens、maxOverflow、strategy、droppedCount。
func (h *TokenAwareOverflowHandler) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return map[string]interface{}{
		"overflowCount":       h.overflowCount,
		"totalOverflowTokens": h.totalOverflowTokens,
		"maxOverflow":         h.maxOverflow,
		"strategy":            h.strategy,
		"droppedCount":        h.droppedCount,
	}
}

// Reset 重置溢出处理器的统计信息（不重置 maxOverflow 和 strategy 配置）。
func (h *TokenAwareOverflowHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.overflowCount = 0
	h.totalOverflowTokens = 0
	h.droppedCount = 0
}

// taohComputeRatio 计算平均溢出比率（辅助函数）。
// 公式: (totalTokens / count) / maxOverflow。
// 若 count 为 0 或 maxOverflow 为 0，返回 0。
func taohComputeRatio(totalTokens int, count int, maxOverflow int) float64 {
	if count == 0 || maxOverflow == 0 {
		return 0
	}
	avg := float64(totalTokens) / float64(count)
	return avg / float64(maxOverflow)
}
