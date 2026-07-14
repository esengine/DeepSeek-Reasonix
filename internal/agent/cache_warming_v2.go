package agent

import (
	"strings"
	"sync"
	"time"
)

// ── OPT-96: CacheWarmingV2 (高级缓存预热 V2) ──
// 基于模式学习的高级缓存预热，通过记录查询间的跟随关系来预测
// 下一个可能的查询，并提前预热缓存以提升命中率。
//
// 原理：
//   - LearnPattern 记录"查询 A 之后通常跟查询 B"的跟随关系
//   - PredictFollowUp 基于已学习的模式预测当前查询的后续查询
//   - WarmCache 在匹配到高频模式（频率 > 2）时自动触发预热函数
//   - RecordOutcome 记录预热结果的命中/未命中，用于更新模式成功率
//
// 效果：通过模式学习预测后续查询，提前预热缓存，减少缓存未命中，
// 提升整体响应速度。

// WarmingPattern 表示一个已学习的查询预热模式。
type WarmingPattern struct {
	QueryPrefix     string   // 查询前缀（模式键，已归一化为小写）
	FollowUpQueries []string // 记录的后续查询列表
	Frequency       int      // 出现频率
	LastSeen        int64    // 最后出现时间（Unix 时间戳）
	SuccessRate     float64  // 预热成功率（0-1）
}

// CacheWarmingV2Stats 缓存预热 V2 统计信息。
type CacheWarmingV2Stats struct {
	TotalWarmed     int     // 预热总次数
	TotalHits       int     // 命中次数
	TotalMisses     int     // 未命中次数
	HitRate         float64 // 命中率（0-1）
	PatternsLearned int     // 已学习模式数
}

// CacheWarmingV2 基于模式学习的高级缓存预热器。
type CacheWarmingV2 struct {
	mu            sync.RWMutex
	patterns      map[string]*WarmingPattern
	totalWarmed   int
	totalHits     int
	totalMisses   int
	lastWarmedKey string          // 最近一次预热的模式键，用于 RecordOutcome
	outcomeCount  map[string]int  // 每个模式的结局计数，用于增量更新成功率
}

// NewCacheWarmingV2 创建一个新的 CacheWarmingV2 实例。
func NewCacheWarmingV2() *CacheWarmingV2 {
	return &CacheWarmingV2{
		patterns:     make(map[string]*WarmingPattern),
		outcomeCount: make(map[string]int),
	}
}

// LearnPattern 记录查询间的跟随关系。
// query 之后通常跟 followUp，系统会记录这一模式用于后续预测。
func (c *CacheWarmingV2) LearnPattern(query string, followUp string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := strings.ToLower(strings.TrimSpace(query))
	if key == "" {
		return
	}

	now := time.Now().Unix()

	if p, exists := c.patterns[key]; exists {
		p.Frequency++
		p.LastSeen = now
		// 添加 followUp（避免重复）
		found := false
		for _, f := range p.FollowUpQueries {
			if strings.EqualFold(f, followUp) {
				found = true
				break
			}
		}
		if !found && followUp != "" {
			p.FollowUpQueries = append(p.FollowUpQueries, followUp)
		}
	} else {
		initial := []string{}
		if followUp != "" {
			initial = append(initial, followUp)
		}
		c.patterns[key] = &WarmingPattern{
			QueryPrefix:     key,
			FollowUpQueries: initial,
			Frequency:       1,
			LastSeen:        now,
			SuccessRate:     0,
		}
	}
}

// PredictFollowUp 基于已学习的模式预测当前查询的后续查询。
// 首先尝试精确匹配，然后尝试前缀匹配。返回最可能的后续查询，
// 如果没有匹配的模式则返回空字符串。
func (c *CacheWarmingV2) PredictFollowUp(currentQuery string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalized := strings.ToLower(strings.TrimSpace(currentQuery))
	if normalized == "" {
		return ""
	}

	// 精确匹配
	if p, exists := c.patterns[normalized]; exists && len(p.FollowUpQueries) > 0 {
		return bestWarmingFollowUp(p.FollowUpQueries)
	}

	// 前缀匹配：查找 QueryPrefix 是当前查询前缀的模式
	var bestPattern *WarmingPattern
	for _, p := range c.patterns {
		if strings.HasPrefix(normalized, p.QueryPrefix) {
			if bestPattern == nil || p.Frequency > bestPattern.Frequency {
				bestPattern = p
			}
		}
	}

	if bestPattern != nil && len(bestPattern.FollowUpQueries) > 0 {
		return bestWarmingFollowUp(bestPattern.FollowUpQueries)
	}

	return ""
}

// WarmCache 在查询匹配到高频模式时触发缓存预热。
// 如果查询匹配到频率 > 2 的模式，调用 prepareFn 并传入预测的后续查询。
// 返回 true 表示预热已触发，false 表示未触发。
func (c *CacheWarmingV2) WarmCache(query string, prepareFn func(string)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return false
	}

	// 查找匹配的模式（先精确，再前缀）
	var matched *WarmingPattern
	matchedKey := ""

	if p, exists := c.patterns[normalized]; exists {
		matched = p
		matchedKey = normalized
	} else {
		for key, p := range c.patterns {
			if strings.HasPrefix(normalized, p.QueryPrefix) {
				if matched == nil || p.Frequency > matched.Frequency {
					matched = p
					matchedKey = key
				}
			}
		}
	}

	if matched == nil || matched.Frequency <= 2 {
		return false
	}

	if len(matched.FollowUpQueries) == 0 {
		return false
	}

	followUp := bestWarmingFollowUp(matched.FollowUpQueries)
	if followUp == "" {
		return false
	}

	c.totalWarmed++
	c.lastWarmedKey = matchedKey

	if prepareFn != nil {
		prepareFn(followUp)
	}

	return true
}

// RecordOutcome 记录预热结果是否命中缓存。
// 更新全局命中/未命中计数，以及对应模式的成功率。
func (c *CacheWarmingV2) RecordOutcome(wasHit bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if wasHit {
		c.totalHits++
	} else {
		c.totalMisses++
	}

	if c.lastWarmedKey != "" {
		if p, exists := c.patterns[c.lastWarmedKey]; exists {
			count := c.outcomeCount[c.lastWarmedKey] + 1
			c.outcomeCount[c.lastWarmedKey] = count

			if wasHit {
				p.SuccessRate = (p.SuccessRate*float64(count-1) + 1.0) / float64(count)
			} else {
				p.SuccessRate = p.SuccessRate * float64(count-1) / float64(count)
			}
		}
	}
}

// GetStats 返回缓存预热 V2 的统计信息。
func (c *CacheWarmingV2) GetStats() CacheWarmingV2Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var hitRate float64
	total := c.totalHits + c.totalMisses
	if total > 0 {
		hitRate = float64(c.totalHits) / float64(total)
	}

	return CacheWarmingV2Stats{
		TotalWarmed:     c.totalWarmed,
		TotalHits:       c.totalHits,
		TotalMisses:     c.totalMisses,
		HitRate:         hitRate,
		PatternsLearned: len(c.patterns),
	}
}

// Reset 清除所有已学习模式和统计信息。
func (c *CacheWarmingV2) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.patterns = make(map[string]*WarmingPattern)
	c.outcomeCount = make(map[string]int)
	c.totalWarmed = 0
	c.totalHits = 0
	c.totalMisses = 0
	c.lastWarmedKey = ""
}

// bestWarmingFollowUp 返回后续查询列表中出现频率最高的查询。
func bestWarmingFollowUp(followUps []string) string {
	if len(followUps) == 0 {
		return ""
	}

	counts := make(map[string]int)
	for _, f := range followUps {
		counts[f]++
	}

	best := followUps[0]
	bestCount := 0
	for f, cnt := range counts {
		if cnt > bestCount {
			best = f
			bestCount = cnt
		}
	}

	return best
}
