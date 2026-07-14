package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// ToolCallRecord represents a record of a single tool call.
type ToolCallRecord struct {
	ToolName   string
	Args       string
	Result     string
	Turn       int
	ResultHash string
}

// ToolCallOptStats holds statistics about tool call optimization.
type ToolCallOptStats struct {
	TotalOptimized int
	TotalSkipped   int
	TokensSaved    int
	UniqueCalls    int
}

// ToolCallOptimizer optimizes tool call patterns to reduce redundant
// tool invocations. It tracks call history and can detect when an
// identical call (same tool name and args) has been made recently,
// allowing the caller to skip redundant calls and reuse cached results.
type ToolCallOptimizer struct {
	mu             sync.RWMutex
	callHistory    []ToolCallRecord
	totalOptimized int
	totalSkipped   int
	tokensSaved    int
}

// NewToolCallOptimizer creates a new ToolCallOptimizer.
func NewToolCallOptimizer() *ToolCallOptimizer {
	return &ToolCallOptimizer{}
}

// ShouldSkipCall returns true if an identical call (same toolName and args)
// was made in the last 3 turns. When a skip is detected, totalSkipped and
// tokensSaved are updated to reflect the avoided redundant call.
func (t *ToolCallOptimizer) ShouldSkipCall(toolName string, args string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.callHistory) == 0 {
		return false
	}

	// Find the most recent turn in history
	maxTurn := 0
	for _, rec := range t.callHistory {
		if rec.Turn > maxTurn {
			maxTurn = rec.Turn
		}
	}

	// Check calls within the last 3 turns (maxTurn, maxTurn-1, maxTurn-2)
	minTurn := maxTurn - 2
	if minTurn < 0 {
		minTurn = 0
	}

	for _, rec := range t.callHistory {
		if rec.Turn >= minTurn && rec.ToolName == toolName && rec.Args == args {
			t.totalSkipped++
			t.tokensSaved += tcoEstimatedTokensSavedPerSkip
			return true
		}
	}

	return false
}

// RecordCall records a tool call in the history. The result is hashed for
// efficient comparison and storage.
func (t *ToolCallOptimizer) RecordCall(toolName string, args string, result string, turn int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.callHistory = append(t.callHistory, ToolCallRecord{
		ToolName:   toolName,
		Args:       args,
		Result:     result,
		Turn:       turn,
		ResultHash: tcoHashResult(result),
	})
	t.totalOptimized++
}

// GetCallFrequency returns how many times a tool with the given name has
// been called.
func (t *ToolCallOptimizer) GetCallFrequency(toolName string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	for _, rec := range t.callHistory {
		if rec.ToolName == toolName {
			count++
		}
	}
	return count
}

// GetStats returns statistics about the tool call optimizer, including the
// number of unique calls (distinct tool name + args combinations).
func (t *ToolCallOptimizer) GetStats() ToolCallOptStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	uniqueCalls := make(map[string]bool)
	for _, rec := range t.callHistory {
		key := rec.ToolName + ":" + rec.Args
		uniqueCalls[key] = true
	}

	return ToolCallOptStats{
		TotalOptimized: t.totalOptimized,
		TotalSkipped:   t.totalSkipped,
		TokensSaved:    t.tokensSaved,
		UniqueCalls:    len(uniqueCalls),
	}
}

// Reset resets all statistics and state.
func (t *ToolCallOptimizer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.callHistory = nil
	t.totalOptimized = 0
	t.totalSkipped = 0
	t.tokensSaved = 0
}

// ---------------------------------------------------------------------------
// Internal helpers (prefixed with tco to avoid naming conflicts)
// ---------------------------------------------------------------------------

// tcoEstimatedTokensSavedPerSkip is a rough estimate of tokens saved when a
// redundant tool call is skipped. This includes the request tokens for the
// tool call and the response tokens that would have been generated.
const tcoEstimatedTokensSavedPerSkip = 50

// tcoHashResult computes a SHA-256 hash of the given string and returns
// the hex-encoded digest.
func tcoHashResult(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
