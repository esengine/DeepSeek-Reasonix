package agent

import "sync"

// ── OPT-106: SemanticCacheRouter (语义缓存路由器) ──
// 根据语义相似度路由缓存查询，而非依赖精确匹配。
//
// 原理：对查询进行分词后使用 Jaccard 相似度（交集/并集）与路由表
// 中的已有 key 进行比对。当相似度达到阈值时返回对应的缓存 key，
// 从而让措辞略有不同但含义相同的查询也能命中缓存。
//
// 效果：语义路由可减少重复计算与不必要的上下文重建，在查询表述
// 多样化的场景下提升缓存利用率。

// SemanticCacheRouter 语义缓存路由器，根据语义相似度路由缓存查询。
type SemanticCacheRouter struct {
	mu                  sync.RWMutex
	routes              int
	hits                int
	misses              int
	routeTable          map[string]string
	similarityThreshold float64
}

// NewSemanticCacheRouter 创建新的语义缓存路由器，默认相似度阈值为 0.75。
func NewSemanticCacheRouter() *SemanticCacheRouter {
	return &SemanticCacheRouter{
		routeTable:          make(map[string]string),
		similarityThreshold: 0.75,
	}
}

// Route 在路由表中查找与 query 语义最相似的 key。
// 若最高相似度 >= 阈值则返回对应的缓存 key，否则返回空字符串。
// 每次调用递增 routes 计数，并根据是否命中更新 hits/misses。
func (r *SemanticCacheRouter) Route(query string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.routes++

	var bestKey string
	var bestSim float64

	queryTokens := scrTokenize(query)
	for key := range r.routeTable {
		sim := scrJaccardSimilarity(queryTokens, scrTokenize(key))
		if sim > bestSim {
			bestSim = sim
			bestKey = key
		}
	}

	if bestSim >= r.similarityThreshold {
		r.hits++
		return r.routeTable[bestKey]
	}

	r.misses++
	return ""
}

// AddRoute 添加一条路由条目，将 query 映射到 cacheKey。
func (r *SemanticCacheRouter) AddRoute(query string, cacheKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routeTable[query] = cacheKey
}

// RecordHit 手动记录一次缓存命中。
func (r *SemanticCacheRouter) RecordHit() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits++
}

// RecordMiss 手动记录一次缓存未命中。
func (r *SemanticCacheRouter) RecordMiss() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.misses++
}

// GetHitRate 返回当前缓存命中率（命中次数 / 总路由次数）。
// 若总路由次数为 0 则返回 0。
func (r *SemanticCacheRouter) GetHitRate() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.routes == 0 {
		return 0
	}
	return float64(r.hits) / float64(r.routes)
}

// GetStats 返回路由器的统计信息，包括 routes、hits、misses、
// hitRate、threshold 和 tableSize。
func (r *SemanticCacheRouter) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var hitRate float64
	if r.routes > 0 {
		hitRate = float64(r.hits) / float64(r.routes)
	}

	return map[string]interface{}{
		"routes":    r.routes,
		"hits":      r.hits,
		"misses":    r.misses,
		"hitRate":   hitRate,
		"threshold": r.similarityThreshold,
		"tableSize": len(r.routeTable),
	}
}

// Reset 重置路由器的所有状态，包括计数和路由表。
func (r *SemanticCacheRouter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = 0
	r.hits = 0
	r.misses = 0
	r.routeTable = make(map[string]string)
}

// scrTokenize 将字符串按空白字符分词，返回小写 token 切片。
func scrTokenize(s string) []string {
	var tokens []string
	current := make([]rune, 0, len(s))
	for _, ch := range s {
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if len(current) > 0 {
				tokens = append(tokens, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, ch)
		}
	}
	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}
	return tokens
}

// scrJaccardSimilarity 计算两组 token 的 Jaccard 相似度（交集/并集）。
func scrJaccardSimilarity(a, b []string) float64 {
	setA := make(map[string]struct{}, len(a))
	for _, t := range a {
		setA[t] = struct{}{}
	}

	intersection := 0
	for _, t := range b {
		if _, ok := setA[t]; ok {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
