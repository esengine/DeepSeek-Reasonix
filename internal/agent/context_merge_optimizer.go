package agent

import "sync"

// ── OPT-168: ContextMergeOptimizer (上下文合并优化器) ──
// 优化上下文片段的合并顺序以减少 token 浪费。
// 通过检测并移除相邻片段间的重复前缀/后缀，避免冗余内容被重复发送。

// MergeFragment 合并片段，包含唯一标识、内容和估算的 token 数
type MergeFragment struct {
	ID              string
	Content         string
	EstimatedTokens int
}

// ContextMergeOptimizer 上下文合并优化器
type ContextMergeOptimizer struct {
	mu               sync.RWMutex
	fragments        []MergeFragment
	mergeCount       int
	tokensSaved      int
	overlapThreshold int
}

// NewContextMergeOptimizer 创建上下文合并优化器
func NewContextMergeOptimizer(overlapThreshold int) *ContextMergeOptimizer {
	return &ContextMergeOptimizer{
		fragments:        make([]MergeFragment, 0),
		overlapThreshold: overlapThreshold,
	}
}

// AddFragment 添加片段
func (o *ContextMergeOptimizer) AddFragment(id string, content string, estimatedTokens int) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.fragments = append(o.fragments, MergeFragment{
		ID:              id,
		Content:         content,
		EstimatedTokens: estimatedTokens,
	})
}

// Merge 合并所有片段，检测并移除相邻片段间的重复前缀/后缀
func (o *ContextMergeOptimizer) Merge() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.fragments) == 0 {
		return ""
	}

	merged := o.fragments[0].Content
	for i := 1; i < len(o.fragments); i++ {
		overlap := cmoFindOverlap(merged, o.fragments[i].Content, o.overlapThreshold)
		if overlap > 0 {
			// 跳过与前一片段后缀重叠的部分
			merged += o.fragments[i].Content[overlap:]
			o.tokensSaved += overlap / 4
		} else {
			merged += o.fragments[i].Content
		}
	}
	o.mergeCount++
	return merged
}

// GetMergeCount 获取合并次数
func (o *ContextMergeOptimizer) GetMergeCount() int {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return o.mergeCount
}

// GetStats 返回上下文合并优化器统计信息
func (o *ContextMergeOptimizer) GetStats() map[string]interface{} {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return map[string]interface{}{
		"fragmentCount":    len(o.fragments),
		"mergeCount":       o.mergeCount,
		"tokensSaved":      o.tokensSaved,
		"overlapThreshold": o.overlapThreshold,
	}
}

// Reset 重置优化器
func (o *ContextMergeOptimizer) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.fragments = make([]MergeFragment, 0)
	o.mergeCount = 0
	o.tokensSaved = 0
}

// cmoFindOverlap 查找两字符串间的重叠部分长度
// 即 s 的后缀与 next 的前缀的最大公共部分长度
// 当 threshold > 0 时，仅检测不超过 threshold 长度的重叠
func cmoFindOverlap(s string, next string, threshold int) int {
	maxOverlap := len(s)
	if maxOverlap > len(next) {
		maxOverlap = len(next)
	}
	if threshold > 0 && maxOverlap > threshold {
		maxOverlap = threshold
	}
	for i := maxOverlap; i > 0; i-- {
		if s[len(s)-i:] == next[:i] {
			return i
		}
	}
	return 0
}
