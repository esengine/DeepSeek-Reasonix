package agent

import "sync"

// AwarenessReport describes the result of a single awareness check.
type AwarenessReport struct {
	Status         string
	UsagePercent   float64
	Recommendation string
}

// AwarenessStats holds aggregate statistics about token-awareness monitoring.
type AwarenessStats struct {
	TotalChecks     int
	OverBudgetCount int
	NearLimitCount  int
	AvgTokensPerTurn float64
}

// TokenAwarenessMonitor tracks token usage against the model context window
// and provides actionable recommendations when usage approaches or exceeds
// safe thresholds. OPT-58.
type TokenAwarenessMonitor struct {
	mu               sync.RWMutex
	totalChecks      int
	overBudgetCount  int
	nearLimitCount   int
	avgTokensPerTurn float64
	totalTokensUsed  int
	turnsTracked     int
}

// NewTokenAwarenessMonitor creates a new TokenAwarenessMonitor.
func NewTokenAwarenessMonitor() *TokenAwarenessMonitor {
	return &TokenAwarenessMonitor{}
}

// CheckAwareness evaluates the current token usage against the context window
// and returns a report with a status ("ok", "warning", or "critical"), the
// usage percentage, and a recommendation.
func (m *TokenAwarenessMonitor) CheckAwareness(promptTokens int, completionTokens int, contextWindow int) AwarenessReport {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalChecks++

	var report AwarenessReport
	totalTokens := promptTokens + completionTokens
	if contextWindow > 0 {
		report.UsagePercent = float64(totalTokens) / float64(contextWindow) * 100
	}

	switch {
	case report.UsagePercent >= 90:
		report.Status = "critical"
		report.Recommendation = "context window nearly exhausted; compact or prune history immediately"
		m.overBudgetCount++
	case report.UsagePercent >= 75:
		report.Status = "warning"
		report.Recommendation = "approaching context limit; consider pruning older messages"
		m.nearLimitCount++
	default:
		report.Status = "ok"
		report.Recommendation = "token usage within safe bounds"
	}

	return report
}

// TrackTurn records the token usage for a single turn and updates the running
// average of tokens consumed per turn.
func (m *TokenAwarenessMonitor) TrackTurn(promptTokens int, completionTokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.turnsTracked++
	m.totalTokensUsed += promptTokens + completionTokens
	if m.turnsTracked > 0 {
		m.avgTokensPerTurn = float64(m.totalTokensUsed) / float64(m.turnsTracked)
	}
}

// GetStats returns a snapshot of the current awareness-monitoring statistics.
func (m *TokenAwarenessMonitor) GetStats() AwarenessStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return AwarenessStats{
		TotalChecks:      m.totalChecks,
		OverBudgetCount:  m.overBudgetCount,
		NearLimitCount:   m.nearLimitCount,
		AvgTokensPerTurn: m.avgTokensPerTurn,
	}
}

// Reset clears all accumulated statistics.
func (m *TokenAwarenessMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalChecks = 0
	m.overBudgetCount = 0
	m.nearLimitCount = 0
	m.avgTokensPerTurn = 0
	m.totalTokensUsed = 0
	m.turnsTracked = 0
}
