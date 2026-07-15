package agent
import "sync"

// ── OPT-218: ContextDensityAnalyzer (上下文密度分析器) ──
// 分析上下文内容的信息密度（信息量 / 长度），用于判断是否值得压缩。
// 密度越高表示单位长度承载的信息越多，压缩收益越低；密度越低则冗余越多、压缩收益越高。
// 累计分析次数与密度统计（平均/最大/最小）以便观测上下文质量趋势。

// ContextDensityAnalyzer 上下文密度分析器，计算内容的信息密度分数（0~1）。
type ContextDensityAnalyzer struct {
	mu           sync.RWMutex
	analyses     int     // 累计分析次数
	totalDensity float64 // 累计密度之和（用于计算平均）
	maxDensity   float64 // 历史最大密度
	minDensity   float64 // 历史最小密度
}

// NewContextDensityAnalyzer 创建一个新的上下文密度分析器。
// minDensity 初始化为 1.0，maxDensity 初始化为 0.0。
func NewContextDensityAnalyzer() *ContextDensityAnalyzer {
	return &ContextDensityAnalyzer{
		minDensity: 1.0,
	}
}

// Analyze 分析内容的信息密度（信息量 / 长度），返回 0~1 的密度分数。
// 同时更新分析次数与密度统计。密度越高代表信息越密集。
func (c *ContextDensityAnalyzer) Analyze(content string) float64 {
	density := cdaComputeDensity(content)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.analyses++
	c.totalDensity += density
	if density > c.maxDensity {
		c.maxDensity = density
	}
	if density < c.minDensity {
		c.minDensity = density
	}
	return density
}

// GetAvgDensity 返回历史平均密度。若未分析过则返回 0。
func (c *ContextDensityAnalyzer) GetAvgDensity() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.analyses == 0 {
		return 0
	}
	return c.totalDensity / float64(c.analyses)
}

// GetMaxDensity 返回历史最大密度。
func (c *ContextDensityAnalyzer) GetMaxDensity() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.maxDensity
}

// GetMinDensity 返回历史最小密度。
func (c *ContextDensityAnalyzer) GetMinDensity() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.minDensity
}

// GetStats 返回分析器的统计信息。
// 包含: analyses, avgDensity, maxDensity, minDensity。
func (c *ContextDensityAnalyzer) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	avg := 0.0
	if c.analyses > 0 {
		avg = c.totalDensity / float64(c.analyses)
	}
	return map[string]interface{}{
		"analyses":   c.analyses,
		"avgDensity": avg,
		"maxDensity": c.maxDensity,
		"minDensity": c.minDensity,
	}
}

// Reset 重置分析器，清空所有统计，minDensity 恢复为 1.0。
func (c *ContextDensityAnalyzer) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.analyses = 0
	c.totalDensity = 0
	c.maxDensity = 0
	c.minDensity = 1.0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 cda 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// cdaComputeDensity 计算内容的信息密度。
// 以词级别的词汇多样性（唯一词数 / 总词数）作为信息量 / 长度的度量，
// 结果钳制在 [0, 1] 区间；空内容返回 0。
func cdaComputeDensity(content string) float64 {
	words := cdaSplitWords(content)
	total := len(words)
	if total == 0 {
		return 0
	}
	seen := make(map[string]struct{}, total)
	for _, w := range words {
		seen[w] = struct{}{}
	}
	unique := len(seen)
	density := float64(unique) / float64(total)
	if density < 0 {
		density = 0
	}
	if density > 1 {
		density = 1
	}
	return density
}

// cdaSplitWords 按空白字符（空格、制表、换行、回车等）切分内容为词列表。
// 连续空白不产生空词，便于作为信息密度计算的输入。
func cdaSplitWords(content string) []string {
	var words []string
	var current []rune
	for _, r := range content {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' {
			if len(current) > 0 {
				words = append(words, string(current))
				current = current[:0]
			}
			continue
		}
		current = append(current, r)
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}
