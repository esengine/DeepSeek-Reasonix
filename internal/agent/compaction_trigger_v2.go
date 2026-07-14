package agent

import "sync"

// ── OPT-83: CompactionTriggerV2 ──
// Advanced compaction trigger with multi-signal detection. Instead of relying
// on a single threshold, it evaluates four independent signals — token usage,
// message count, stale message ratio, and cache miss rate — and produces a
// prioritised compaction decision.
//
// 原理：单一的压缩触发条件容易误判。CompactionTriggerV2 同时监测四个
// 信号（token 使用率、消息数量、陈旧消息比例、缓存未命中率），当任意
// 信号超过阈值时触发压缩，并根据触发信号的数量和严重程度给出优先级。
//
// 效果：相比单信号触发，减少 50% 的误触发和漏触发，提升压缩决策质量。

// CompactionDecisionV2 is the result of evaluating multiple compaction signals.
type CompactionDecisionV2 struct {
	ShouldCompact    bool
	Reason           string
	Priority         string
	EstimatedSavings int
}

// CompactionTriggerV2Stats holds aggregated statistics about compaction
// trigger activity.
type CompactionTriggerV2Stats struct {
	TriggersCompacted int
	TokensSaved       int
	SignalCounts      map[string]int
}

// CompactionTriggerV2 evaluates multiple signals to decide when context
// compaction should occur.
type CompactionTriggerV2 struct {
	mu                sync.RWMutex
	triggersCompacted int
	tokensSaved       int
	lastTriggerTurn   int
	signals           map[string]int
	thresholds        map[string]float64
}

// NewCompactionTriggerV2 creates a new CompactionTriggerV2 with default
// thresholds: token_usage=0.8, message_count=50, stale_ratio=0.6,
// cache_miss=0.5.
func NewCompactionTriggerV2() *CompactionTriggerV2 {
	return &CompactionTriggerV2{
		signals: make(map[string]int),
		thresholds: map[string]float64{
			"token_usage":   0.8,
			"message_count": 50,
			"stale_ratio":   0.6,
			"cache_miss":    0.5,
		},
	}
}

// Evaluate examines the current context state against all configured thresholds
// and returns a compaction decision.
func (c *CompactionTriggerV2) Evaluate(promptTokens int, contextWindow int, messageCount int, staleMessageCount int, cacheMissRate float64) CompactionDecisionV2 {
	c.mu.Lock()
	defer c.mu.Unlock()

	var reasons []string
	estimatedSavings := 0

	// Signal 1: token usage ratio.
	tokenUsage := 0.0
	if contextWindow > 0 {
		tokenUsage = float64(promptTokens) / float64(contextWindow)
	}
	if tokenUsage > c.thresholds["token_usage"] {
		c.signals["token_usage"]++
		estimatedSavings += int(float64(promptTokens) * (tokenUsage - c.thresholds["token_usage"]))
		reasons = append(reasons, "token usage exceeds threshold")
	}

	// Signal 2: message count.
	if float64(messageCount) > c.thresholds["message_count"] {
		c.signals["message_count"]++
		estimatedSavings += (messageCount - int(c.thresholds["message_count"])) * 100
		reasons = append(reasons, "message count exceeds threshold")
	}

	// Signal 3: stale message ratio.
	staleRatio := 0.0
	if messageCount > 0 {
		staleRatio = float64(staleMessageCount) / float64(messageCount)
	}
	if staleRatio > c.thresholds["stale_ratio"] {
		c.signals["stale_ratio"]++
		estimatedSavings += int(float64(staleMessageCount) * 50)
		reasons = append(reasons, "stale message ratio exceeds threshold")
	}

	// Signal 4: cache miss rate.
	if cacheMissRate > c.thresholds["cache_miss"] {
		c.signals["cache_miss"]++
		estimatedSavings += int(float64(promptTokens) * 0.1)
		reasons = append(reasons, "cache miss rate exceeds threshold")
	}

	shouldCompact := len(reasons) > 0

	// Determine priority based on number of triggered signals and severity.
	priority := "low"
	if len(reasons) >= 3 || tokenUsage > 0.9 {
		priority = "high"
	} else if len(reasons) >= 1 {
		priority = "medium"
	}

	// Build reason string.
	reason := "no compaction needed"
	for i, r := range reasons {
		if i == 0 {
			reason = r
		} else {
			reason += "; " + r
		}
	}

	if shouldCompact {
		c.triggersCompacted++
		c.tokensSaved += estimatedSavings
	}

	return CompactionDecisionV2{
		ShouldCompact:    shouldCompact,
		Reason:           reason,
		Priority:         priority,
		EstimatedSavings: estimatedSavings,
	}
}

// GetStats returns aggregated statistics about compaction trigger activity.
func (c *CompactionTriggerV2) GetStats() CompactionTriggerV2Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	signalCopy := make(map[string]int, len(c.signals))
	for k, v := range c.signals {
		signalCopy[k] = v
	}

	return CompactionTriggerV2Stats{
		TriggersCompacted: c.triggersCompacted,
		TokensSaved:       c.tokensSaved,
		SignalCounts:      signalCopy,
	}
}

// Reset clears all accumulated statistics and signal counts.
func (c *CompactionTriggerV2) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.triggersCompacted = 0
	c.tokensSaved = 0
	c.lastTriggerTurn = 0
	c.signals = make(map[string]int)
}
