package agent

import "sync"

// ── OPT-140: CacheKeyHasher (缓存键哈希器) ──
// 生成高效的缓存哈希键。键由 FNV-1a 哈希值、内容长度与前 8 字符组合而成，
// 兼顾唯一性与紧凑性。同时维护正反向映射，以支持原始内容查找与碰撞检测。
//
// 碰撞语义:
//   - 不同内容映射到相同哈希键 -> 真实哈希碰撞
//   - 相同内容产生不同哈希键 -> 视为冲突（确定性哈希下不应出现）

// CacheKeyHasher 缓存键哈希器，生成高效哈希键。
type CacheKeyHasher struct {
	mu              sync.RWMutex
	totalHashed     int
	totalCollisions int
	hashTable       map[string]string // hash -> content
	reverseLookup   map[string]string // content -> hash
}

// NewCacheKeyHasher 创建一个新的缓存键哈希器。
func NewCacheKeyHasher() *CacheKeyHasher {
	return &CacheKeyHasher{
		hashTable:     make(map[string]string),
		reverseLookup: make(map[string]string),
	}
}

// Hash 生成内容哈希键：FNV-1a 哈希 + 内容长度 + 前 8 字符。
// 同时记录正反向映射；若内容已哈希过则返回原哈希键。
// 检测到哈希碰撞时递增 totalCollisions。
func (h *CacheKeyHasher) Hash(content string) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	hashKey := ckhBuildKey(content)
	h.totalHashed++

	if existing, ok := h.reverseLookup[content]; ok {
		// 内容已哈希过
		if existing != hashKey {
			// 相同内容产生不同哈希 -> 视为冲突
			h.totalCollisions++
		}
		return existing
	}

	// 新内容
	if stored, exists := h.hashTable[hashKey]; exists && stored != content {
		// 不同内容映射到相同哈希 -> 真实哈希碰撞
		h.totalCollisions++
	}
	h.hashTable[hashKey] = content
	h.reverseLookup[content] = hashKey
	return hashKey
}

// Lookup 通过哈希查找原始内容。
func (h *CacheKeyHasher) Lookup(hash string) (string, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	content, ok := h.hashTable[hash]
	return content, ok
}

// CheckCollision 检查内容是否已哈希过。
// 若内容已存在，或相同哈希键已映射到不同内容，均视为碰撞并返回 true。
func (h *CacheKeyHasher) CheckCollision(content string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hashKey := ckhBuildKey(content)
	if existing, ok := h.reverseLookup[content]; ok {
		if existing != hashKey {
			return true // 相同内容不同哈希
		}
		return true // 已哈希过
	}
	if stored, exists := h.hashTable[hashKey]; exists && stored != content {
		return true // 不同内容相同哈希
	}
	return false
}

// GetStats 返回哈希器的统计信息。
// 包含 totalHashed、totalCollisions、collisionRate、tableSize。
func (h *CacheKeyHasher) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	collisionRate := 0.0
	if h.totalHashed > 0 {
		collisionRate = float64(h.totalCollisions) / float64(h.totalHashed)
	}
	return map[string]interface{}{
		"totalHashed":     h.totalHashed,
		"totalCollisions": h.totalCollisions,
		"collisionRate":   collisionRate,
		"tableSize":       len(h.hashTable),
	}
}

// Reset 重置哈希器的所有映射与统计信息。
func (h *CacheKeyHasher) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.totalHashed = 0
	h.totalCollisions = 0
	h.hashTable = make(map[string]string)
	h.reverseLookup = make(map[string]string)
}

// ---------------------------------------------------------------------------
// 辅助函数 (ckh 前缀)
// ---------------------------------------------------------------------------

// ckhBuildKey 构建哈希键: 内容长度:前8字符:FNV-1a 哈希值。
func ckhBuildKey(content string) string {
	length := len(content)
	prefix := ckhPrefix(content, 8)
	hash := ckhFNV1a(content)
	return ckhIntToStr(length) + ":" + prefix + ":" + ckhUint32ToStr(hash)
}

// ckhPrefix 返回字符串前 n 个字符。
func ckhPrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ckhFNV1a 使用 FNV-1a 算法计算 32 位哈希值。
func ckhFNV1a(s string) uint32 {
	hash := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		hash ^= uint32(s[i])
		hash *= 16777619
	}
	return hash
}

// ckhIntToStr 将 int 转为字符串（不依赖 strconv）。
func ckhIntToStr(n int) string {
	if n == 0 {
		return "0"
	}
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ckhUint32ToStr 将 uint32 转为字符串（不依赖 strconv）。
func ckhUint32ToStr(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
