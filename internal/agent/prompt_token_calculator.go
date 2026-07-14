package agent
import "sync"

// OPT-175: PromptTokenCalculator — 提示Token计算器
// PromptTokenCalculator precisely calculates the number of tokens in prompts.
// It records each calculation and maintains aggregate statistics (min, max, avg, total).
type TokenCalculation struct {
	PromptID   string // 提示标识符 prompt identifier
	TokenCount int    // token数量 token count
	Timestamp  int64  // 时间戳 timestamp
}

// PromptTokenCalculator calculates and records token counts for prompts.
// PromptTokenCalculator 计算并记录提示的token数。
type PromptTokenCalculator struct {
	mu             sync.RWMutex
	totalCalculated int                  // 总计算次数 total number of calculations
	totalTokens     int                  // 累计token总数 total tokens across all calculations
	maxTokensSeen   int                  // 单次最大token数 maximum tokens seen in a single calculation
	minTokensSeen   int                  // 单次最小token数 minimum tokens seen in a single calculation
	calculations    []TokenCalculation   // 计算记录 calculation records
}

// NewPromptTokenCalculator creates a new PromptTokenCalculator.
// NewPromptTokenCalculator 创建新的提示Token计算器。
func NewPromptTokenCalculator() *PromptTokenCalculator {
	return &PromptTokenCalculator{
		totalCalculated: 0,
		totalTokens:     0,
		maxTokensSeen:   0,
		minTokensSeen:   0,
		calculations:    make([]TokenCalculation, 0),
	}
}

// Calculate computes the token count for the given prompt content and records it.
// Calculate 计算给定提示内容的token数并记录。
func (p *PromptTokenCalculator) Calculate(promptID string, content string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	tokenCount := ptcEstimateTokens(content)

	p.totalCalculated++
	p.totalTokens += tokenCount

	if p.totalCalculated == 1 {
		p.maxTokensSeen = tokenCount
		p.minTokensSeen = tokenCount
	} else {
		if tokenCount > p.maxTokensSeen {
			p.maxTokensSeen = tokenCount
		}
		if tokenCount < p.minTokensSeen {
			p.minTokensSeen = tokenCount
		}
	}

	p.calculations = append(p.calculations, TokenCalculation{
		PromptID:   promptID,
		TokenCount: tokenCount,
		Timestamp:  0, // 时间戳由调用方设置 timestamp set by caller
	})

	return tokenCount
}

// Estimate estimates the token count for the given content without recording it.
// Estimate 估算给定内容的token数但不记录。
func (p *PromptTokenCalculator) Estimate(content string) int {
	return ptcEstimateTokens(content)
}

// GetTotalTokens returns the total tokens across all calculations.
// GetTotalTokens 返回所有计算的累计token总数。
func (p *PromptTokenCalculator) GetTotalTokens() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.totalTokens
}

// GetAvgTokens returns the average tokens per calculation.
// GetAvgTokens 返回每次计算的平均token数。
func (p *PromptTokenCalculator) GetAvgTokens() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ptcComputeAvg(p.totalTokens, p.totalCalculated)
}

// GetStats returns statistics about the calculator.
// GetStats 返回计算器的统计信息。
func (p *PromptTokenCalculator) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"totalCalculated": p.totalCalculated,
		"totalTokens":     p.totalTokens,
		"maxTokensSeen":   p.maxTokensSeen,
		"minTokensSeen":   p.minTokensSeen,
		"avgTokens":       ptcComputeAvg(p.totalTokens, p.totalCalculated),
	}
}

// Reset resets the calculator to its initial state.
// Reset 将计算器重置为初始状态。
func (p *PromptTokenCalculator) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalCalculated = 0
	p.totalTokens = 0
	p.maxTokensSeen = 0
	p.minTokensSeen = 0
	p.calculations = make([]TokenCalculation, 0)
}

// ptcEstimateTokens estimates the number of tokens in a string (len/4).
// ptcEstimateTokens 估算字符串中的token数（长度除以4）。
func ptcEstimateTokens(s string) int {
	return len(s) / 4
}

// ptcComputeAvg computes the average given a total and count, returning 0 if count is 0.
// ptcComputeAvg 根据总数和次数计算平均值，次数为0时返回0。
func ptcComputeAvg(total int, count int) float64 {
	if count == 0 {
		return 0.0
	}
	return float64(total) / float64(count)
}
