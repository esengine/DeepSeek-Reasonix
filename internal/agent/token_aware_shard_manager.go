package agent
import "sync"

// ── OPT-216: TokenAwareShardManager (Token感知分片管理器) ──
// 将 token 相关数据按 key 哈希分片存储，以降低单分片锁竞争、提升并发性能。
// 提供 Assign（分配）、GetShard（查询分片）、Migrate（迁移 key）等能力，
// 并累计分片总数、已分片 key 总数与迁移次数以便统计。

// TokenAwareShardManager Token感知分片管理器，按 hash 取模将 key 分配到固定数量的分片。
type TokenAwareShardManager struct {
	mu           sync.RWMutex
	shards       map[int][]string // shardID → keys
	shardCount   int              // 分片数量
	totalSharded int              // 累计已分片的 key 总数
	migrations   int              // 累计迁移次数
}

// NewTokenAwareShardManager 创建一个新的 Token 感知分片管理器。
// shardCount 指定分片数量，若 <= 0 则默认为 1。
func NewTokenAwareShardManager(shardCount int) *TokenAwareShardManager {
	if shardCount <= 0 {
		shardCount = 1
	}
	shards := make(map[int][]string, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = []string{}
	}
	return &TokenAwareShardManager{
		shards:     shards,
		shardCount: shardCount,
	}
}

// Assign 将 key 分配到分片（hash 取模），返回分片 ID。
// 同时将该 key 追加到对应分片，并递增 totalSharded。
func (t *TokenAwareShardManager) Assign(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	shardID := int(tasmHashKey(key) % uint32(t.shardCount))
	t.shards[shardID] = append(t.shards[shardID], key)
	t.totalSharded++
	return shardID
}

// GetShard 获取指定分片中的所有 key（返回副本，避免外部修改内部状态）。
// 若分片不存在则返回 nil。
func (t *TokenAwareShardManager) GetShard(shardID int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	keys, ok := t.shards[shardID]
	if !ok {
		return nil
	}
	out := make([]string, len(keys))
	copy(out, keys)
	return out
}

// Migrate 将 key 从 fromShard 迁移到 toShard。
// 若 key 在 fromShard 中不存在，或目标分片不存在，则返回 false。
// 迁移成功后递增 migrations。
func (t *TokenAwareShardManager) Migrate(key string, fromShard int, toShard int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	src, srcOK := t.shards[fromShard]
	if !srcOK {
		return false
	}
	if _, dstOK := t.shards[toShard]; !dstOK {
		return false
	}

	// 在源分片中查找并移除 key。
	found := -1
	for i, k := range src {
		if k == key {
			found = i
			break
		}
	}
	if found < 0 {
		return false
	}

	t.shards[fromShard] = append(src[:found], src[found+1:]...)
	t.shards[toShard] = append(t.shards[toShard], key)
	t.migrations++
	return true
}

// GetShardCount 返回分片数量。
func (t *TokenAwareShardManager) GetShardCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.shardCount
}

// GetStats 返回分片管理器的统计信息。
// 包含: shardCount, totalSharded, migrations, balancedShards。
// balancedShards 表示 key 数量与平均值偏差在容忍范围内的分片数。
func (t *TokenAwareShardManager) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	balancedShards := 0
	if t.shardCount > 0 {
		avg := float64(t.totalSharded) / float64(t.shardCount)
		tolerance := avg * 0.2
		if tolerance < 1 {
			tolerance = 1
		}
		for i := 0; i < t.shardCount; i++ {
			diff := float64(len(t.shards[i])) - avg
			if diff < 0 {
				diff = -diff
			}
			if diff <= tolerance {
				balancedShards++
			}
		}
	}

	return map[string]interface{}{
		"shardCount":     t.shardCount,
		"totalSharded":   t.totalSharded,
		"migrations":     t.migrations,
		"balancedShards": balancedShards,
	}
}

// Reset 重置分片管理器，清空所有分片数据与计数，保留 shardCount 配置。
func (t *TokenAwareShardManager) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := 0; i < t.shardCount; i++ {
		t.shards[i] = []string{}
	}
	t.totalSharded = 0
	t.migrations = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 tasm 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// tasmHashKey 对 key 计算 FNV-1a 哈希，返回 32 位无符号整数（始终非负）。
func tasmHashKey(key string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h
}
