package agent

import (
	"strings"
	"sync"
)

// ResponseTokenStats holds statistics about response token control.
type ResponseTokenStats struct {
	DefaultMaxTokens int
	TotalControlled  int
	TokensReduced    int
}

// ResponseTokenController controls response token generation by setting
// max_tokens based on the query type and available context. This helps
// avoid wasting tokens on overly long responses for simple queries while
// allowing generous limits for code generation tasks.
type ResponseTokenController struct {
	mu               sync.RWMutex
	defaultMaxTokens int
	totalControlled  int
	tokensReduced    int
	byResponseType   map[string]int
}

// NewResponseTokenController creates a new ResponseTokenController with
// a default max_tokens of 4096.
func NewResponseTokenController() *ResponseTokenController {
	return &ResponseTokenController{
		defaultMaxTokens: 4096,
		byResponseType:   make(map[string]int),
	}
}

// GetMaxTokens adjusts max_tokens based on query type and available context.
// Query type limits:
//   - "code_generation" -> 8192
//   - "explanation"     -> 2048
//   - "summary"         -> 1024
//   - default           -> 4096 (defaultMaxTokens)
//
// The returned value never exceeds availableContext/4 to ensure the response
// does not consume too much of the available context window.
func (r *ResponseTokenController) GetMaxTokens(queryType string, availableContext int) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.totalControlled++

	var maxTokens int
	switch queryType {
	case "code_generation":
		maxTokens = 8192
	case "explanation":
		maxTokens = 2048
	case "summary":
		maxTokens = 1024
	default:
		maxTokens = r.defaultMaxTokens
	}

	// Never exceed availableContext/4
	if availableContext > 0 {
		contextLimit := availableContext / 4
		if maxTokens > contextLimit {
			r.tokensReduced += maxTokens - contextLimit
			maxTokens = contextLimit
		}
	}

	r.byResponseType[queryType]++

	return maxTokens
}

// ClassifyQuery classifies a query type based on keywords.
//   - "write/create/implement/build" -> "code_generation"
//   - "explain/describe/what"        -> "explanation"
//   - "summarize/brief/tl;dr"        -> "summary"
//   - default                        -> "general"
func (r *ResponseTokenController) ClassifyQuery(query string) string {
	q := strings.ToLower(query)

	// Check for code generation keywords
	for _, kw := range []string{"write", "create", "implement", "build"} {
		if strings.Contains(q, kw) {
			return "code_generation"
		}
	}

	// Check for explanation keywords
	for _, kw := range []string{"explain", "describe", "what"} {
		if strings.Contains(q, kw) {
			return "explanation"
		}
	}

	// Check for summary keywords
	for _, kw := range []string{"summarize", "brief", "tl;dr"} {
		if strings.Contains(q, kw) {
			return "summary"
		}
	}

	return "general"
}

// GetStats returns statistics about the response token controller.
func (r *ResponseTokenController) GetStats() ResponseTokenStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return ResponseTokenStats{
		DefaultMaxTokens: r.defaultMaxTokens,
		TotalControlled:  r.totalControlled,
		TokensReduced:    r.tokensReduced,
	}
}

// Reset resets all statistics and state.
func (r *ResponseTokenController) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalControlled = 0
	r.tokensReduced = 0
	r.byResponseType = make(map[string]int)
}
