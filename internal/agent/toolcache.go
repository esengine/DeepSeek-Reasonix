package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Tool-result cache (建模层缓存：工具调用结果复用，docs/DATA_FLYWHEEL.md §3).
//
// 目标：同一轮/相邻轮内模型重复调用同一只读工具（read_file/grep/glob/ls/
// code_index 等）时，直接复用上次结果，跳过真实执行 —— 减少工具延迟与
// provider round-trip。这是 agent 层"缓存命中"投入产出比最高的点。
//
// 正确性约束（宁可 miss 不可错）：
//   - 只缓存幂等只读工具（ReadOnly() + 白名单）；
//   - 参数先归一化（canonicalize：键排序、剥动态噪音键、空白规范化），
//     缓存 key = sha256(tool + canonical args)；
//   - 短 TTL（默认 30s）+ 容量上限，防过期结果与内存膨胀；
//   - 写工具/有状态工具/外部网络工具一律不缓存。
// ---------------------------------------------------------------------------

// cachedToolNames 是确定幂等且结果稳定的只读工具白名单。不在名单里的
// ReadOnly 工具（bash、webfetch 等）即使标记只读也不缓存 —— bash 有 shell
// 副作用，webfetch 结果随外部变化。
var cachedToolNames = map[string]bool{
	"read_file":    true,
	"readfile":     true,
	"grep":         true,
	"glob":         true,
	"ls":           true,
	"code_index":   true,
	"get_domain":   true,
	"get_path":     true,
	"is_palindrome": true,
	"reverse_string": true,
	"is_prime":     true,
	"unique_elements": true,
}

// dynamicKeys 是 canonicalize 时剥离的已知动态噪音键（时间戳/随机/会话），
// 避免同一逻辑请求因噪音参数不同而 miss。
var dynamicKeys = map[string]bool{
	"_ts": true, "timestamp": true, "ts": true, "time": true, "at": true,
	"_t": true, "session_id": true, "request_id": true, "nonce": true,
	"_random": true, "seed": true, "cache_buster": true,
}

// toolCacheEntry is one cached tool result.
type toolCacheEntry struct {
	value   string
	expires time.Time
}

// ToolCache is a small concurrent-safe TTL cache for idempotent read-only
// tool results. Key = sha256(toolName + "|" + canonicalArgs).
type ToolCache struct {
	mu         sync.Mutex
	m          map[string]toolCacheEntry
	ttl        time.Duration
	maxEntries int
	hits       int64
	misses     int64
}

// NewToolCache builds a cache with the given TTL and entry cap.
func NewToolCache(ttl time.Duration, maxEntries int) *ToolCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if maxEntries <= 0 {
		maxEntries = 512
	}
	return &ToolCache{m: make(map[string]toolCacheEntry), ttl: ttl, maxEntries: maxEntries}
}

// Key derives the cache key from a tool name and raw JSON arguments.
func (c *ToolCache) Key(tool, rawArgs string) string {
	h := sha256.Sum256([]byte(tool + "|" + canonicalizeArgs(rawArgs)))
	return fmt.Sprintf("%x", h[:16])
}

// Get returns the cached result and true on a live hit (also bumps the hit
// counter). Expired entries are evicted lazily.
func (c *ToolCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		c.misses++
		return "", false
	}
	if time.Now().After(e.expires) {
		delete(c.m, key)
		c.misses++
		return "", false
	}
	c.hits++
	return e.value, true
}

// Put stores a result under key. When at capacity, the oldest entry is evicted.
func (c *ToolCache) Put(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.maxEntries {
		var oldest string
		var oldestAt time.Time
		for k, e := range c.m {
			if oldest == "" || e.expires.Before(oldestAt) {
				oldest, oldestAt = k, e.expires
			}
		}
		delete(c.m, oldest)
	}
	c.m[key] = toolCacheEntry{value: value, expires: time.Now().Add(c.ttl)}
}

// Stats returns cumulative hit/miss counters (for diagnostics).
func (c *ToolCache) Stats() (hits, misses int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}

// Len returns the current entry count.
func (c *ToolCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

// canonicalizeArgs 把 JSON 参数归一化为稳定字符串：
//  1. 解析为通用结构；解析失败则原样返回（保守：不缓存错误解析）。
//  2. 递归规范化：map 键排序；剥离 dynamicKeys 噪音键；数字规范化为
//     json.Number 文本（1 vs 1.0 视为相同）；空白统一。
//  3. 重新序列化（紧凑、键有序）。
//
// 注意：这是"结构归一化"，不是语义归一化（"广州"vs"广州市"属于语义层，
// 需要实体规范化，不在此范围内）。
func canonicalizeArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw // 保守：无法解析的参数不做 key 归并
	}
	norm := normalizeValue(v)
	b, err := json.Marshal(norm)
	if err != nil {
		return raw
	}
	return string(b)
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if dynamicKeys[k] {
				continue
			}
			out[k] = normalizeValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeValue(val)
		}
		return out
	case json.Number:
		return t.String() // 1 与 1.0 归一为文本比较
	case float64:
		return json.Number(fmt.Sprintf("%v", t)).String()
	default:
		return v
	}
}

// sortedJSONKeys 用于测试断言（canonicalize 输出键有序）。
func sortedJSONKeys(s string) []string {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
