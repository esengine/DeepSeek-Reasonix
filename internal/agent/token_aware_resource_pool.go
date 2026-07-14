package agent
import "sync"

// ── OPT-206: TokenAwareResourcePool (Token 感知资源池 / Token-Aware Resource Pool) ──
// 池化 token 资源以减少分配开销。通过复用已释放的资源，避免频繁的内存分配，
// 降低 GC 压力并提升吞吐量。
//
// 原理：在 token 处理流水线中，频繁创建和销毁资源（如缓冲区、上下文对象）
// 会带来显著的分配开销。资源池维护一组可复用的资源，Acquire 时优先从池中
// 取出复用，Release 时将资源放回池中供后续使用。
//
// 效果：减少资源分配次数，提高复用率，统计分配/复用/释放计数，
// 为性能分析提供数据支撑。

// TokenAwareResourcePool Token 感知资源池
type TokenAwareResourcePool struct {
	mu           sync.RWMutex
	pool         []string // 池化的资源 pooled resources
	maxPoolSize  int      // 池最大容量 maximum pool size
	allocCount   int      // 分配次数（池为空时） allocation count (when pool empty)
	reuseCount   int      // 复用次数 reuse count
	releaseCount int      // 释放次数 release count
}

// NewTokenAwareResourcePool 创建 Token 感知资源池。
// maxPoolSize 指定池的最大容量，若 <= 0 则默认 64。
func NewTokenAwareResourcePool(maxPoolSize int) *TokenAwareResourcePool {
	if maxPoolSize <= 0 {
		maxPoolSize = 64
	}
	return &TokenAwareResourcePool{
		pool:        make([]string, 0, maxPoolSize),
		maxPoolSize: maxPoolSize,
	}
}

// Acquire 从池中获取资源。
// 若池中有可用资源则弹出并返回 (resource, true)（复用）；
// 若池为空则返回 ("", false)，表示调用方需自行分配新资源。
func (t *TokenAwareResourcePool) Acquire() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	n := len(t.pool)
	if n == 0 {
		t.allocCount++
		return "", false
	}

	// 弹出最后一个资源
	resource := t.pool[n-1]
	t.pool = t.pool[:n-1]
	t.reuseCount++
	return resource, true
}

// Release 将资源释放回池中。
// 若池已满则丢弃资源，但仍计入释放次数。
func (t *TokenAwareResourcePool) Release(resource string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.releaseCount++
	if len(t.pool) < t.maxPoolSize {
		t.pool = append(t.pool, resource)
	}
}

// GetPoolSize 获取当前池中可用资源数量。
func (t *TokenAwareResourcePool) GetPoolSize() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.pool)
}

// GetReuseRate 获取资源复用率。
// 复用率 = 复用次数 / (复用次数 + 分配次数)。若总次数为 0 则返回 0。
func (t *TokenAwareResourcePool) GetReuseRate() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return tarpComputeReuseRate(t.reuseCount, t.allocCount)
}

// GetStats 返回资源池的统计信息。
// 包含 maxPoolSize、poolSize、allocCount、reuseCount、releaseCount 和 reuseRate。
func (t *TokenAwareResourcePool) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"maxPoolSize":  t.maxPoolSize,
		"poolSize":     len(t.pool),
		"allocCount":   t.allocCount,
		"reuseCount":   t.reuseCount,
		"releaseCount": t.releaseCount,
		"reuseRate":    tarpComputeReuseRate(t.reuseCount, t.allocCount),
	}
}

// Reset 重置资源池的所有计数并清空池（保留 maxPoolSize 配置）。
func (t *TokenAwareResourcePool) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pool = make([]string, 0, t.maxPoolSize)
	t.allocCount = 0
	t.reuseCount = 0
	t.releaseCount = 0
}

// tarpComputeReuseRate 计算资源复用率。
// 复用率 = 复用次数 / (复用次数 + 分配次数)。若总次数为 0 则返回 0。
func tarpComputeReuseRate(reuseCount, allocCount int) float64 {
	total := reuseCount + allocCount
	if total == 0 {
		return 0
	}
	return float64(reuseCount) / float64(total)
}
