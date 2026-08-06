package agent

import "sync"

// ── OPT-263: ContextWindowDynamicResizer (上下文窗口动态调整器) ──
// 在 [minSize, maxSize] 范围内动态调整上下文窗口大小，支持直接设定
// 目标值、按量增长与按量缩小。每次实际变更都会累计次数与总调整量，
// 并记录历史轨迹用于后续分析。
//
// 原理：上下文窗口过大会消耗更多 token 预算并降低命中率，过小则
// 丢失必要上下文。根据运行时负载与缓存命中情况动态调整窗口，可以
// 在成本与质量之间取得平衡。
//
// 效果：避免窗口固定带来的僵化，提升 token 利用率与响应质量。

// ContextWindowDynamicResizer 上下文窗口动态调整器。
type ContextWindowDynamicResizer struct {
	mu            sync.RWMutex
	minSize       int
	maxSize       int
	currentSize   int
	resizeCount   int
	totalResized  int
	resizeHistory []int
}

// NewContextWindowDynamicResizer 创建一个新的上下文窗口动态调整器。
// minSize 与 maxSize 定义允许的窗口范围（若 min > max 则自动交换），
// initialSize 会被钳制到该范围内作为初始大小。
func NewContextWindowDynamicResizer(minSize int, maxSize int, initialSize int) *ContextWindowDynamicResizer {
	if minSize > maxSize {
		minSize, maxSize = maxSize, minSize
	}
	clamped := cwdrClamp(initialSize, minSize, maxSize)
	return &ContextWindowDynamicResizer{
		minSize:       minSize,
		maxSize:       maxSize,
		currentSize:   clamped,
		resizeHistory: make([]int, 0),
	}
}

// Resize 将窗口调整到 targetSize（钳制到 min/max 范围内），返回实际大小。
// 若目标值与当前值不同，则累计 resizeCount、totalResized 并记录历史。
func (r *ContextWindowDynamicResizer) Resize(targetSize int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	target := cwdrClamp(targetSize, r.minSize, r.maxSize)
	if target != r.currentSize {
		delta := target - r.currentSize
		if delta < 0 {
			delta = -delta
		}
		r.totalResized += delta
		r.resizeCount++
		r.currentSize = target
		r.resizeHistory = append(r.resizeHistory, target)
	}
	return r.currentSize
}

// Grow 增长窗口大小，返回增长后的实际大小。
func (r *ContextWindowDynamicResizer) Grow(amount int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resizeLocked(r.currentSize + amount)
}

// Shrink 缩小窗口大小，返回缩小后的实际大小。
func (r *ContextWindowDynamicResizer) Shrink(amount int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resizeLocked(r.currentSize - amount)
}

// GetCurrentSize 返回当前窗口大小。
func (r *ContextWindowDynamicResizer) GetCurrentSize() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentSize
}

// GetStats 返回统计信息，包含 minSize、maxSize、currentSize、
// resizeCount 和 totalResized。
func (r *ContextWindowDynamicResizer) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]interface{}{
		"minSize":      r.minSize,
		"maxSize":      r.maxSize,
		"currentSize":  r.currentSize,
		"resizeCount":  r.resizeCount,
		"totalResized": r.totalResized,
	}
}

// Reset 重置调整器状态，将当前大小恢复为 minSize 并清空计数与历史，
// 但保留 minSize/maxSize 配置。
func (r *ContextWindowDynamicResizer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentSize = r.minSize
	r.resizeCount = 0
	r.totalResized = 0
	r.resizeHistory = make([]int, 0)
}

// resizeLocked 在已持锁的情况下执行调整逻辑（内部辅助方法）。
func (r *ContextWindowDynamicResizer) resizeLocked(targetSize int) int {
	target := cwdrClamp(targetSize, r.minSize, r.maxSize)
	if target != r.currentSize {
		delta := target - r.currentSize
		if delta < 0 {
			delta = -delta
		}
		r.totalResized += delta
		r.resizeCount++
		r.currentSize = target
		r.resizeHistory = append(r.resizeHistory, target)
	}
	return r.currentSize
}

// cwdrClamp 将 value 钳制到 [min, max] 范围内（辅助函数）。
func cwdrClamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
