package agent
import "sync"

// ── OPT-165: PromptAssemblyOptimizer (提示组装优化器 / Prompt Assembly Optimizer) ──
// 优化提示段落组装顺序以最大化缓存命中。将可缓存（Cacheable=true）的段落
// 前置，使它们构成稳定的缓存前缀；不可缓存的动态段落后置。
//
// 原理：缓存通常按前缀匹配，若静态/可缓存内容位于前部，则更易命中已有缓存，
// 减少重复编码与 token 开销。
//
// 效果：在多轮提示中提升缓存命中率，降低重复前缀的 token 成本。

// AssemblySegment 提示组装段落，包含标识、内容、是否可缓存与顺序。
type AssemblySegment struct {
	ID        string
	Content   string
	Cacheable bool
	Order     int
}

// PromptAssemblyOptimizer 提示组装优化器，按可缓存性重排段落以提升缓存命中。
type PromptAssemblyOptimizer struct {
	mu                sync.RWMutex
	segments          []AssemblySegment
	assemblyCount     int
	cacheHitOptimized int
}

// NewPromptAssemblyOptimizer 创建一个新的 PromptAssemblyOptimizer。
func NewPromptAssemblyOptimizer() *PromptAssemblyOptimizer {
	return &PromptAssemblyOptimizer{}
}

// AddSegment 添加一个段落，其初始 Order 为当前段落数（追加顺序）。
func (o *PromptAssemblyOptimizer) AddSegment(id string, content string, cacheable bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.segments = append(o.segments, AssemblySegment{
		ID:        id,
		Content:   content,
		Cacheable: cacheable,
		Order:     len(o.segments),
	})
}

// Optimize 将 Cacheable=true 的段落前置、其余后置（保持各组内原序），
// 重新分配 Order，并返回优化后的段落副本。
// 若发生重排，则递增 cacheHitOptimized（执行缓存命中优化的次数）。
func (o *PromptAssemblyOptimizer) Optimize() []AssemblySegment {
	o.mu.Lock()
	defer o.mu.Unlock()
	before := make([]AssemblySegment, len(o.segments))
	copy(before, o.segments)
	paoSortSegments(o.segments)
	for i := range o.segments {
		o.segments[i].Order = i
	}
	reordered := false
	for i := range o.segments {
		if o.segments[i].ID != before[i].ID {
			reordered = true
			break
		}
	}
	if reordered {
		o.cacheHitOptimized++
	}
	result := make([]AssemblySegment, len(o.segments))
	copy(result, o.segments)
	return result
}

// Assemble 按当前段落顺序组装完整提示（段落间以换行分隔），并递增组装计数。
// 建议先调用 Optimize 以获得缓存命中优化的顺序。
func (o *PromptAssemblyOptimizer) Assemble() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.assemblyCount++
	total := 0
	for _, s := range o.segments {
		total += len(s.Content) + 1
	}
	buf := make([]byte, 0, total)
	for i, s := range o.segments {
		if i > 0 {
			buf = append(buf, '\n')
		}
		buf = append(buf, s.Content...)
	}
	return string(buf)
}

// GetStats 返回优化器的统计信息，包括 segmentCount、assemblyCount 和 cacheHitOptimized。
func (o *PromptAssemblyOptimizer) GetStats() map[string]interface{} {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return map[string]interface{}{
		"segmentCount":      len(o.segments),
		"assemblyCount":     o.assemblyCount,
		"cacheHitOptimized": o.cacheHitOptimized,
	}
}

// Reset 重置优化器的所有状态，清空段落与计数。
func (o *PromptAssemblyOptimizer) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.segments = nil
	o.assemblyCount = 0
	o.cacheHitOptimized = 0
}

// paoSortSegments 对段落进行稳定分区：Cacheable=true 的保持原序前置，其余保持原序后置。
func paoSortSegments(segments []AssemblySegment) {
	cacheable := make([]AssemblySegment, 0, len(segments))
	others := make([]AssemblySegment, 0, len(segments))
	for _, s := range segments {
		if s.Cacheable {
			cacheable = append(cacheable, s)
		} else {
			others = append(others, s)
		}
	}
	n := 0
	for _, s := range cacheable {
		segments[n] = s
		n++
	}
	for _, s := range others {
		segments[n] = s
		n++
	}
}
