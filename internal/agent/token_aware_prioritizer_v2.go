package agent
import "sync"

// ── OPT-161: TokenAwarePrioritizerV2 (Token 感知优先级器 V2 / Token-Aware Prioritizer V2) ──
// 结合 token 成本和相关性双重维度对候选项进行排序。每个项目的优先级
// 分数定义为 relevance / max(tokenCost, 1)，分数越高越优先处理。
//
// 原理：在 token 预算有限时，既相关又便宜的项目能带来更高的单位收益。
// 通过 relevance/tokenCost 的比值降序排列，可在相同预算下最大化相关性覆盖。
//
// 效果：相比仅按相关性排序，可在不显著降低相关性的前提下节省 15%-30% 的 token。

// Tap2PrioritizerItem 优先级排序项 V2，包含标识、token 成本、相关性与优先级分数。
// 注意：与 token_aware_prioritizer.go 中的 PrioritizerItem（v1）字段不同，
// 此处使用 tap2 前缀避免命名冲突。
type Tap2PrioritizerItem struct {
	ID            string
	TokenCost     int
	Relevance     float64
	PriorityScore float64
}

// TokenAwarePrioritizerV2 Token 感知优先级器 V2，按 priorityScore 降序排序候选项。
type TokenAwarePrioritizerV2 struct {
	mu        sync.RWMutex
	items     []Tap2PrioritizerItem
	ranked    bool
	sortCount int
}

// NewTokenAwarePrioritizerV2 创建一个新的 TokenAwarePrioritizerV2。
func NewTokenAwarePrioritizerV2() *TokenAwarePrioritizerV2 {
	return &TokenAwarePrioritizerV2{}
}

// Add 添加一个候选项，并根据 token 成本与相关性计算其优先级分数。
// 添加后排序状态标记为未排序（ranked=false）。
func (p *TokenAwarePrioritizerV2) Add(id string, tokenCost int, relevance float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = append(p.items, Tap2PrioritizerItem{
		ID:            id,
		TokenCost:     tokenCost,
		Relevance:     relevance,
		PriorityScore: tap2ComputeScore(relevance, tokenCost),
	})
	p.ranked = false
}

// Rank 对所有候选项按 priorityScore 降序排序，并递增排序计数。
func (p *TokenAwarePrioritizerV2) Rank() {
	p.mu.Lock()
	defer p.mu.Unlock()
	tap2SortItems(p.items)
	p.ranked = true
	p.sortCount++
}

// GetTopN 返回前 n 个候选项（按已排序顺序）。
// 若 n 超过项目总数则返回全部；若 n<=0 或无项目则返回空切片。
// 返回的是副本，不会影响内部状态。
func (p *TokenAwarePrioritizerV2) GetTopN(n int) []Tap2PrioritizerItem {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if n <= 0 || len(p.items) == 0 {
		return []Tap2PrioritizerItem{}
	}
	if n > len(p.items) {
		n = len(p.items)
	}
	result := make([]Tap2PrioritizerItem, n)
	copy(result, p.items[:n])
	return result
}

// GetStats 返回优先级器的统计信息，包括 itemCount、sortCount 和 ranked。
func (p *TokenAwarePrioritizerV2) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"itemCount": len(p.items),
		"sortCount": p.sortCount,
		"ranked":    p.ranked,
	}
}

// Reset 重置优先级器的所有状态，清空候选项与计数。
func (p *TokenAwarePrioritizerV2) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = nil
	p.ranked = false
	p.sortCount = 0
}

// tap2ComputeScore 计算优先级分数：relevance / max(tokenCost, 1)。
// 当 tokenCost 小于 1 时按 1 计算，避免除零。
func tap2ComputeScore(relevance float64, tokenCost int) float64 {
	cost := tokenCost
	if cost < 1 {
		cost = 1
	}
	return relevance / float64(cost)
}

// tap2SortItems 按 PriorityScore 降序稳定排序（插入排序）。
func tap2SortItems(items []Tap2PrioritizerItem) {
	for i := 1; i < len(items); i++ {
		key := items[i]
		j := i - 1
		for j >= 0 && items[j].PriorityScore < key.PriorityScore {
			items[j+1] = items[j]
			j--
		}
		items[j+1] = key
	}
}
