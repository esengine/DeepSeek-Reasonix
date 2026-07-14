package agent
import "sync"

// ── OPT-203: ContextPruningStrategy (上下文修剪策略器 / Context Pruning Strategy) ──
// 支持多种上下文修剪策略：aggressive（激进）、moderate（适度）、conservative（保守）。
// 根据策略对上下文消息进行修剪，使总 token 数不超过保留上限。
//
// 原理：不同修剪策略对应不同的修剪比率。
// aggressive 修剪较多 token 以最大程度节省预算；
// moderate 适度修剪，在节省 token 和保留信息间取得平衡；
// conservative 保守修剪，仅修剪最少量的 token。
//
// 效果：根据策略灵活修剪上下文，统计修剪次数和修剪的 token 数，
// 计算平均修剪比率，为上下文管理提供反馈。

// ContextPruningStrategy 上下文修剪策略器
type ContextPruningStrategy struct {
	mu                sync.RWMutex
	strategy          string // 当前修剪策略
	pruneCount        int    // 修剪次数
	totalTokensPruned int    // 修剪的总 token 数
	maxRetainTokens   int    // 最大保留 token 数
}

// NewContextPruningStrategy 创建上下文修剪策略器。
// strategy 指定初始策略，可选 "aggressive"、"moderate"、"conservative"，若为空或无效则默认 "moderate"。
// maxRetainTokens 指定最大保留 token 数，若 <= 0 则默认 8192。
func NewContextPruningStrategy(strategy string, maxRetainTokens int) *ContextPruningStrategy {
	if maxRetainTokens <= 0 {
		maxRetainTokens = 8192
	}
	switch strategy {
	case "aggressive", "moderate", "conservative":
	default:
		strategy = "moderate"
	}
	return &ContextPruningStrategy{
		strategy:        strategy,
		maxRetainTokens: maxRetainTokens,
	}
}

// Prune 根据策略修剪上下文消息。
// messages 为待修剪的消息列表，estimatedTokens 为消息列表的预估总 token 数。
// 若预估 token 数不超过 maxRetainTokens，则不修剪直接返回。
// 否则根据策略对应的修剪比率计算目标保留量，从最早的消息开始逐步移除，
// 直至达到目标保留量。
// 返回修剪后的消息列表和实际修剪的 token 数。
func (c *ContextPruningStrategy) Prune(messages []string, estimatedTokens int) ([]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if estimatedTokens <= c.maxRetainTokens {
		return messages, 0
	}

	ratio := cpsSelectPruneRatio(c.strategy)

	// 目标保留量：预估总量 * (1 - 修剪比率)，但不低于 maxRetainTokens 的约束
	targetTokens := int(float64(estimatedTokens) * (1.0 - ratio))
	if targetTokens > c.maxRetainTokens {
		targetTokens = c.maxRetainTokens
	}

	result := make([]string, len(messages))
	copy(result, messages)

	currentTokens := estimatedTokens
	pruned := 0
	for currentTokens > targetTokens && len(result) > 1 {
		removed := result[0]
		result = result[1:]
		tokenRemoved := len(removed) / 4
		currentTokens -= tokenRemoved
		pruned += tokenRemoved
	}

	c.pruneCount++
	c.totalTokensPruned += pruned
	return result, pruned
}

// SetStrategy 切换修剪策略。
// strategy 可选 "aggressive"、"moderate"、"conservative"，若为空或无效则默认 "moderate"。
func (c *ContextPruningStrategy) SetStrategy(strategy string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch strategy {
	case "aggressive", "moderate", "conservative":
		c.strategy = strategy
	default:
		c.strategy = "moderate"
	}
}

// GetPruneRatio 返回平均修剪比率。
// 修剪比率 = 总修剪 token 数 / (总修剪 token 数 + 估算总保留 token 数)。
// 其中估算总保留 token 数 = 修剪次数 * maxRetainTokens。
// 若修剪次数为 0 则返回 0。
func (c *ContextPruningStrategy) GetPruneRatio() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.pruneCount == 0 {
		return 0
	}
	totalRetained := c.pruneCount * c.maxRetainTokens
	totalInput := c.totalTokensPruned + totalRetained
	if totalInput == 0 {
		return 0
	}
	return float64(c.totalTokensPruned) / float64(totalInput)
}

// GetStats 返回上下文修剪策略器的统计信息。
// 包含 strategy、pruneCount、totalTokensPruned 和 maxRetainTokens。
func (c *ContextPruningStrategy) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"strategy":          c.strategy,
		"pruneCount":        c.pruneCount,
		"totalTokensPruned": c.totalTokensPruned,
		"maxRetainTokens":   c.maxRetainTokens,
	}
}

// Reset 重置修剪策略器的统计信息（不重置策略和 maxRetainTokens）。
func (c *ContextPruningStrategy) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneCount = 0
	c.totalTokensPruned = 0
}

// cpsSelectPruneRatio 根据修剪策略选择对应的修剪比率。
// aggressive 返回 0.7（修剪 70% 的 token），
// moderate 返回 0.5（修剪 50%），
// conservative 返回 0.3（修剪 30%），
// 默认返回 0.5。
func cpsSelectPruneRatio(strategy string) float64 {
	switch strategy {
	case "aggressive":
		return 0.7
	case "moderate":
		return 0.5
	case "conservative":
		return 0.3
	default:
		return 0.5
	}
}
