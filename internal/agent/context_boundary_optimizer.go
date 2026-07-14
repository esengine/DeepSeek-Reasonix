package agent

import "sync"

// ── OPT-188: ContextBoundaryOptimizer (上下文边界优化器) ──
// 优化上下文分割边界以减少碎片化。通过移除产生过小片段的边界点，
// 合并细碎分段，降低上下文碎片化带来的 token 浪费和检索开销。
//
// 原理：上下文被边界点切分为多个片段，若相邻边界间距过小则形成
// 碎片片段。优化时以 minSegmentSize 为阈值，移除使片段小于
// 该阈值的边界点，使相邻片段自动合并。
//
// 效果：减少碎片片段数量，提升片段平均大小，降低上下文管理开销。

// ContextBoundaryOptimizer 上下文边界优化器，优化上下文分割边界以减少碎片化。
type ContextBoundaryOptimizer struct {
	mu                sync.RWMutex
	boundaries        []int
	optimizationCount int
	fragmentsReduced  int
	minSegmentSize    int
}

// NewContextBoundaryOptimizer 创建一个新的上下文边界优化器。
// minSegmentSize 指定片段的最小允许大小，低于该值的片段将被合并。
func NewContextBoundaryOptimizer(minSegmentSize int) *ContextBoundaryOptimizer {
	return &ContextBoundaryOptimizer{
		boundaries:        make([]int, 0),
		optimizationCount: 0,
		fragmentsReduced:  0,
		minSegmentSize:    minSegmentSize,
	}
}

// SetBoundaries 设置分割边界。
// boundaries 为边界位置列表（位置值），内部会拷贝以避免外部修改。
func (c *ContextBoundaryOptimizer) SetBoundaries(boundaries []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.boundaries = make([]int, len(boundaries))
	copy(c.boundaries, boundaries)
}

// Optimize 优化边界：移除产生过小片段的边界点。
// 以 minSegmentSize 为阈值，逐一遍历边界，若当前边界与前一个
// 有效边界间距小于阈值则跳过该边界点（使后续片段自动合并）。
// 返回优化后的边界列表，并更新优化计数和碎片减少数。
func (c *ContextBoundaryOptimizer) Optimize() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	originalFragments := len(c.boundaries) + 1
	optimized := cboOptimizeBoundaries(c.boundaries, c.minSegmentSize)
	optimizedFragments := len(optimized) + 1
	c.fragmentsReduced = originalFragments - optimizedFragments
	if c.fragmentsReduced < 0 {
		c.fragmentsReduced = 0
	}
	c.boundaries = optimized
	c.optimizationCount++
	result := make([]int, len(optimized))
	copy(result, optimized)
	return result
}

// GetFragmentCount 获取优化后的片段数。
// 片段数 = 边界数 + 1。
func (c *ContextBoundaryOptimizer) GetFragmentCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.boundaries) + 1
}

// GetStats 返回统计信息，包含 boundaryCount、optimizationCount、
// fragmentsReduced 和 minSegmentSize。
func (c *ContextBoundaryOptimizer) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"boundaryCount":     len(c.boundaries),
		"optimizationCount": c.optimizationCount,
		"fragmentsReduced":  c.fragmentsReduced,
		"minSegmentSize":    c.minSegmentSize,
	}
}

// Reset 重置优化器状态，清空边界列表和所有统计计数。
func (c *ContextBoundaryOptimizer) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.boundaries = make([]int, 0)
	c.optimizationCount = 0
	c.fragmentsReduced = 0
}

// cboOptimizeBoundaries 优化边界，移除产生过小片段的边界点（辅助函数）。
// 以 0 为起点，逐一检查每个边界与前一个有效边界之间的间距，
// 若间距小于 minSegmentSize 则跳过该边界点（不加入结果），
// 否则保留并更新前一个有效边界位置。
func cboOptimizeBoundaries(boundaries []int, minSegmentSize int) []int {
	if len(boundaries) == 0 {
		return []int{}
	}
	optimized := make([]int, 0, len(boundaries))
	prevBoundary := 0
	for _, b := range boundaries {
		segmentSize := b - prevBoundary
		if segmentSize >= minSegmentSize {
			optimized = append(optimized, b)
			prevBoundary = b
		}
		// 间距不足则跳过该边界点，使后续片段与前一片段合并
	}
	return optimized
}
