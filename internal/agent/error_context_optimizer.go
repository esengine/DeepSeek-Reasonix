package agent

import (
	"strings"
	"sync"
)

// ErrorContextStats holds statistics about error-context optimization.
type ErrorContextStats struct {
	TotalOptimized int
	TokensSaved    int
	ErrorPatterns  map[string]int
}

// ErrorContextOptimizer reduces the context that is sent alongside an error
// retry by keeping only the lines relevant to the error and discarding the
// rest. OPT-59.
type ErrorContextOptimizer struct {
	mu             sync.RWMutex
	totalOptimized int
	tokensSaved    int
	errorPatterns  map[string]int
}

// NewErrorContextOptimizer creates a new ErrorContextOptimizer.
func NewErrorContextOptimizer() *ErrorContextOptimizer {
	return &ErrorContextOptimizer{
		errorPatterns: make(map[string]int),
	}
}

// OptimizeErrorContext extracts only the context relevant to the given error
// message and discards irrelevant lines. Statistics are updated to reflect the
// optimization.
func (e *ErrorContextOptimizer) OptimizeErrorContext(errorMsg string, fullContext string) string {
	optimized := e.ExtractErrorRelevantContext(errorMsg, fullContext)

	e.mu.Lock()
	defer e.mu.Unlock()

	e.totalOptimized++
	saved := (len(fullContext) - len(optimized)) / 4
	if saved < 0 {
		saved = 0
	}
	e.tokensSaved += saved

	pattern := classifyError(errorMsg)
	e.errorPatterns[pattern]++

	return optimized
}

// IsRetryableError reports whether the error message indicates a transient
// condition that may succeed on retry (timeout, rate limit, connection
// issues, server errors).
func (e *ErrorContextOptimizer) IsRetryableError(errorMsg string) bool {
	lower := strings.ToLower(errorMsg)
	retryable := []string{"timeout", "rate limit", "connection", "server error", "503", "429", "500"}
	for _, kw := range retryable {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// ExtractErrorRelevantContext scans the provided context for lines that
// contain error-related keywords (derived from the error message and a set of
// generic error terms) and returns those lines plus a three-line window of
// surrounding context. Irrelevant lines are removed.
func (e *ErrorContextOptimizer) ExtractErrorRelevantContext(errorMsg string, context string) string {
	lines := strings.Split(context, "\n")
	if len(lines) == 0 {
		return context
	}

	keywords := buildErrorKeywords(errorMsg)

	relevant := make(map[int]bool)
	for i, line := range lines {
		lowerLine := strings.ToLower(line)
		for _, kw := range keywords {
			if strings.Contains(lowerLine, kw) {
				start := i - 3
				if start < 0 {
					start = 0
				}
				end := i + 3
				if end >= len(lines) {
					end = len(lines) - 1
				}
				for j := start; j <= end; j++ {
					relevant[j] = true
				}
				break
			}
		}
	}

	if len(relevant) == 0 {
		// No keyword matches; return the full context unchanged.
		return context
	}

	var result []string
	for i, line := range lines {
		if relevant[i] {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// GetStats returns a snapshot of the current error-context optimization
// statistics. The returned ErrorPatterns map is a copy so callers cannot
// mutate internal state.
func (e *ErrorContextOptimizer) GetStats() ErrorContextStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	patterns := make(map[string]int, len(e.errorPatterns))
	for k, v := range e.errorPatterns {
		patterns[k] = v
	}
	return ErrorContextStats{
		TotalOptimized: e.totalOptimized,
		TokensSaved:    e.tokensSaved,
		ErrorPatterns:  patterns,
	}
}

// Reset clears all accumulated statistics and error patterns.
func (e *ErrorContextOptimizer) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.totalOptimized = 0
	e.tokensSaved = 0
	e.errorPatterns = make(map[string]int)
}

// classifyError maps an error message to a broad category for pattern
// tracking.
func classifyError(errorMsg string) string {
	lower := strings.ToLower(errorMsg)
	switch {
	case strings.Contains(lower, "timeout"):
		return "timeout"
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "429"):
		return "rate_limit"
	case strings.Contains(lower, "connection"):
		return "connection"
	case strings.Contains(lower, "server error") || strings.Contains(lower, "500") || strings.Contains(lower, "503"):
		return "server_error"
	case strings.Contains(lower, "not found") || strings.Contains(lower, "404"):
		return "not_found"
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401") || strings.Contains(lower, "403"):
		return "auth"
	default:
		return "other"
	}
}

// buildErrorKeywords assembles the set of lower-case keywords used to locate
// relevant context lines. It includes significant words from the error message
// itself as well as a set of generic error-related terms.
func buildErrorKeywords(errorMsg string) []string {
	generic := []string{
		"error", "err", "fail", "panic", "exception", "traceback",
		"undefined", "null", "nil", "fatal", "warning", "invalid",
	}

	words := strings.Fields(strings.ToLower(errorMsg))
	seen := make(map[string]bool)
	for _, w := range words {
		cleaned := strings.Trim(w, ".,;:!?\"'()[]{}")
		if len(cleaned) >= 3 && !seen[cleaned] {
			seen[cleaned] = true
		}
	}
	for _, g := range generic {
		if !seen[g] {
			seen[g] = true
		}
	}

	keywords := make([]string, 0, len(seen))
	for k := range seen {
		keywords = append(keywords, k)
	}
	return keywords
}
