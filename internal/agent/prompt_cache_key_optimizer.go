package agent

import "sync"

// ── OPT-225: PromptCacheKeyOptimizer (提示缓存键优化器) ──
// 优化缓存键的生成以最大化命中率：先规范化（去首尾空白、转小写、折叠
// 连续空白），再做哈希压缩为固定长度摘要，从而减少键长度、提升比较与
// 存储效率，并保留 original→optimized 映射以支持反向查找与碰撞统计。
//
// 原理：规范化消除等价键的表面差异（如大小写、多余空格），哈希压缩将
// 任意长度键归约为固定 16 字符摘要；通过映射表检测不同原始键哈希到同一
// 摘要的碰撞。
//
// 效果：降低缓存键存储与比较开销，提升命中率，并提供键长度缩减率与
// 碰撞计数用于质量评估。

// PromptCacheKeyOptimizer 提示缓存键优化器。
type PromptCacheKeyOptimizer struct {
	mu             sync.RWMutex
	keyCache       map[string]string // original → optimized
	optimizedCount int
	collisionCount int
	totalKeyLength int
}

// NewPromptCacheKeyOptimizer 创建缓存键优化器。
func NewPromptCacheKeyOptimizer() *PromptCacheKeyOptimizer {
	return &PromptCacheKeyOptimizer{
		keyCache: make(map[string]string),
	}
}

// Optimize 优化缓存键（规范化+哈希压缩），返回优化后的键。
// 相同原始键命中缓存直接返回；若哈希与已有不同原始键碰撞则计 collisionCount。
func (o *PromptCacheKeyOptimizer) Optimize(key string) string {
	o.mu.Lock()
	defer o.mu.Unlock()

	if opt, ok := o.keyCache[key]; ok {
		return opt
	}

	optimized := pckoHashKey(pckoNormalize(key))

	// 碰撞检测：是否有不同原始键已映射到同一优化键
	for orig, opt := range o.keyCache {
		if opt == optimized && orig != key {
			o.collisionCount++
			break
		}
	}

	o.keyCache[key] = optimized
	o.optimizedCount++
	o.totalKeyLength += len(key)
	return optimized
}

// GetOriginalKey 反向查找优化键对应的原始键。
func (o *PromptCacheKeyOptimizer) GetOriginalKey(optimized string) (string, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for orig, opt := range o.keyCache {
		if opt == optimized {
			return orig, true
		}
	}
	return "", false
}

// GetKeyReduction 返回键长度缩减率 (0~1)。
// 缩减率 = 1 - 优化后总长度 / 优化前总长度。
func (o *PromptCacheKeyOptimizer) GetKeyReduction() float64 {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.optimizedCount == 0 || o.totalKeyLength == 0 {
		return 0
	}

	optimizedLen := 0
	for _, opt := range o.keyCache {
		optimizedLen += len(opt)
	}

	return 1 - float64(optimizedLen)/float64(o.totalKeyLength)
}

// GetStats 返回统计信息：optimizedCount、collisionCount、avgKeyLength、totalKeyLength。
func (o *PromptCacheKeyOptimizer) GetStats() map[string]interface{} {
	o.mu.RLock()
	defer o.mu.RUnlock()

	avgKeyLength := 0.0
	if o.optimizedCount > 0 {
		avgKeyLength = float64(o.totalKeyLength) / float64(o.optimizedCount)
	}

	return map[string]interface{}{
		"optimizedCount": o.optimizedCount,
		"collisionCount": o.collisionCount,
		"avgKeyLength":   avgKeyLength,
		"totalKeyLength": o.totalKeyLength,
	}
}

// Reset 重置优化器，清除所有映射与统计。
func (o *PromptCacheKeyOptimizer) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.keyCache = make(map[string]string)
	o.optimizedCount = 0
	o.collisionCount = 0
	o.totalKeyLength = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 pcko 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// pckoNormalize 规范化缓存键：去首尾空白、转小写、折叠连续空白为单个空格。
func pckoNormalize(key string) string {
	isSpace := func(ch byte) bool {
		return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
	}

	start := 0
	for start < len(key) && isSpace(key[start]) {
		start++
	}
	end := len(key)
	for end > start && isSpace(key[end-1]) {
		end--
	}

	var b []byte
	prevSpace := false
	for i := start; i < end; i++ {
		ch := key[i]
		if isSpace(ch) {
			if prevSpace {
				continue
			}
			b = append(b, ' ')
			prevSpace = true
		} else {
			if ch >= 'A' && ch <= 'Z' {
				ch += 32
			}
			b = append(b, ch)
			prevSpace = false
		}
	}
	return string(b)
}

// pckoHashKey 使用 FNV-1a 64 位哈希将键压缩为 16 字符的十六进制摘要。
func pckoHashKey(s string) string {
	const (
		offset64 uint64 = 14695981039346656037
		prime64  uint64 = 1099511628211
	)
	h := offset64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}

	const hexdigits = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hexdigits[h&0xf]
		h >>= 4
	}
	return string(out)
}
