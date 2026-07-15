package agent

import "sync"

// ── OPT-212: CacheInvalidationCascade (缓存失效级联器) ──
// 处理缓存失效的级联传播。当一个缓存key失效时，所有依赖于该key的
// 缓存也需要被失效。通过维护依赖关系图，支持级联失效传播，
// 并通过maxDepth限制传播深度，防止无限递归。
//
// 核心能力：
//   - AddDependency: 注册key与dependent之间的依赖关系
//   - Invalidate: 失效指定key及其所有级联依赖，返回完整失效列表
//   - GetDependencies: 查询指定key的直接依赖列表
//
// 使用visited map防止循环引用导致的无限递归。

// CacheInvalidationCascade 缓存失效级联器。
type CacheInvalidationCascade struct {
	mu              sync.RWMutex
	dependencies    map[string][]string // key → dependent keys
	cascadeCount    int
	totalPropagated int
	maxDepth        int
}

// NewCacheInvalidationCascade 创建一个新的缓存失效级联器实例。
// maxDepth 指定级联传播的最大深度，0表示仅失效自身不传播。
func NewCacheInvalidationCascade(maxDepth int) *CacheInvalidationCascade {
	return &CacheInvalidationCascade{
		dependencies: make(map[string][]string),
		maxDepth:     maxDepth,
	}
}

// AddDependency 添加一条依赖关系：dependent依赖于key，
// 当key失效时，dependent也需要被失效。
func (c *CacheInvalidationCascade) AddDependency(key string, dependent string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.dependencies[key] = append(c.dependencies[key], dependent)
}

// Invalidate 失效指定的key及其所有级联依赖。
// 返回所有被失效的key列表（包括自身）。
// 级联深度受maxDepth限制，防止过度传播。
func (c *CacheInvalidationCascade) Invalidate(key string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cascadeCount++
	visited := make(map[string]bool)
	invalidated := cicCascadeCollect(c.dependencies, key, visited, 0, c.maxDepth)
	c.totalPropagated += len(invalidated)
	return invalidated
}

// GetDependencies 获取指定key的直接依赖列表。
// 返回的切片为副本，外部修改不影响内部状态。
func (c *CacheInvalidationCascade) GetDependencies(key string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	deps := c.dependencies[key]
	result := make([]string, len(deps))
	copy(result, deps)
	return result
}

// GetStats 返回级联器的统计信息。
func (c *CacheInvalidationCascade) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"dependencyCount": len(c.dependencies),
		"cascadeCount":    c.cascadeCount,
		"totalPropagated": c.totalPropagated,
		"maxDepth":        c.maxDepth,
	}
}

// Reset 重置级联器为初始状态。
func (c *CacheInvalidationCascade) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.dependencies = make(map[string][]string)
	c.cascadeCount = 0
	c.totalPropagated = 0
}

// cicCascadeCollect 递归收集key及其所有级联依赖。
// visited记录已访问的key以避免循环引用导致的无限递归。
// depth为当前递归深度，maxDepth限制最大传播深度：
//   - depth 0: 收集key自身
//   - depth < maxDepth时继续向下一级依赖传播
func cicCascadeCollect(dependencies map[string][]string, key string, visited map[string]bool, depth int, maxDepth int) []string {
	if visited[key] {
		return nil
	}
	visited[key] = true

	result := []string{key}

	if depth < maxDepth {
		for _, dep := range dependencies[key] {
			result = append(result, cicCascadeCollect(dependencies, dep, visited, depth+1, maxDepth)...)
		}
	}

	return result
}
