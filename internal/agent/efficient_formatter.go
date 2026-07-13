package agent

import (
	"encoding/json"
	"strings"
	"sync"
)

// FormatterStats holds statistics about token-efficient formatting operations.
type FormatterStats struct {
	TotalFormatted int
	TokensSaved    int
	ByType         map[string]int
}

// TokenEfficientFormatter reduces token usage by compacting JSON args,
// trimming tool output, and removing unnecessary whitespace.
type TokenEfficientFormatter struct {
	mu             sync.RWMutex
	totalFormatted int
	tokensSaved    int
	byType         map[string]int
}

// NewTokenEfficientFormatter creates a new TokenEfficientFormatter with
// initialized internal state.
func NewTokenEfficientFormatter() *TokenEfficientFormatter {
	return &TokenEfficientFormatter{
		byType: make(map[string]int),
	}
}

// maxTruncatedValueLen is the maximum length a string value may have before
// it is truncated during FormatToolArgs.
const maxTruncatedValueLen = 200

// defaultMaxTokens is used when a non-positive maxTokens value is supplied
// to FormatToolOutput.
const defaultMaxTokens = 2000

// FormatToolArgs parses the provided JSON args string, removes empty or zero
// values, shortens overly long string values, and returns compact JSON. On
// parse error the original string is returned unchanged. Stats are updated to
// reflect the token savings achieved.
func (f *TokenEfficientFormatter) FormatToolArgs(args string) string {
	origTokens := f.EstimateTokens(args)

	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		f.recordStats("args", origTokens, origTokens)
		return args
	}

	var data interface{}
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		// Gracefully return original on parse error.
		return args
	}

	cleaned := f.cleanValue(data)

	// json.Marshal produces compact JSON with no whitespace between tokens.
	result, err := json.Marshal(cleaned)
	if err != nil {
		return args
	}

	out := string(result)
	newTokens := f.EstimateTokens(out)
	f.recordStats("args", origTokens, newTokens)
	return out
}

// FormatToolOutput normalizes tool output by removing excessive blank lines,
// trimming trailing whitespace per line, and truncating to respect the
// maxTokens budget (approximated as maxTokens*4 characters). A non-positive
// maxTokens value defaults to defaultMaxTokens. Stats are updated to reflect
// the token savings achieved.
func (f *TokenEfficientFormatter) FormatToolOutput(output string, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	origTokens := f.EstimateTokens(output)

	processed := f.normalizeOutput(output)

	// Approximate token budget as 4 chars per token.
	charBudget := maxTokens * 4
	if len(processed) > charBudget {
		processed = processed[:charBudget] + "[truncated]"
	}

	newTokens := f.EstimateTokens(processed)
	f.recordStats("output", origTokens, newTokens)
	return processed
}

// CompactJSON removes all unnecessary whitespace from a JSON string. If the
// input cannot be parsed as JSON, the original string is returned unchanged.
func (f *TokenEfficientFormatter) CompactJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}

	var data interface{}
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return s
	}

	result, err := json.Marshal(data)
	if err != nil {
		return s
	}
	return string(result)
}

// EstimateTokens returns a rough estimate of the token count for the given
// text using the len(text)/4 heuristic.
func (f *TokenEfficientFormatter) EstimateTokens(text string) int {
	return len(text) / 4
}

// GetStats returns a snapshot of the formatter's cumulative statistics.
func (f *TokenEfficientFormatter) GetStats() FormatterStats {
	f.mu.RLock()
	defer f.mu.RUnlock()

	byTypeCopy := make(map[string]int, len(f.byType))
	for k, v := range f.byType {
		byTypeCopy[k] = v
	}

	return FormatterStats{
		TotalFormatted: f.totalFormatted,
		TokensSaved:    f.tokensSaved,
		ByType:         byTypeCopy,
	}
}

// Reset clears all accumulated statistics, returning the formatter to its
// initial state.
func (f *TokenEfficientFormatter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.totalFormatted = 0
	f.tokensSaved = 0
	f.byType = make(map[string]int)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// recordStats updates cumulative counters under the write lock.
func (f *TokenEfficientFormatter) recordStats(typeKey string, origTokens, newTokens int) {
	saved := origTokens - newTokens
	if saved < 0 {
		saved = 0
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.totalFormatted++
	f.tokensSaved += saved
	f.byType[typeKey]++
}

// cleanValue recursively removes empty/zero values from the decoded JSON
// structure and truncates long string values.
func (f *TokenEfficientFormatter) cleanValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, child := range val {
			cleaned := f.cleanValue(child)
			if f.isEmpty(cleaned) {
				continue
			}
			result[k] = cleaned
		}
		return result

	case []interface{}:
		result := make([]interface{}, 0, len(val))
		for _, item := range val {
			cleaned := f.cleanValue(item)
			if f.isEmpty(cleaned) {
				continue
			}
			result = append(result, cleaned)
		}
		return result

	case string:
		if len(val) > maxTruncatedValueLen {
			return val[:maxTruncatedValueLen] + "..."
		}
		return val

	default:
		return v
	}
}

// isEmpty reports whether a value should be considered empty/zero for the
// purposes of removal during cleaning. Boolean values are intentionally kept
// (including false) because they are semantically meaningful in tool args.
func (f *TokenEfficientFormatter) isEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case float64:
		return val == 0
	case int:
		return val == 0
	case map[string]interface{}:
		return len(val) == 0
	case []interface{}:
		return len(val) == 0
	default:
		return false
	}
}

// normalizeOutput trims trailing whitespace on each line and collapses runs of
// blank lines longer than one into a single blank line.
func (f *TokenEfficientFormatter) normalizeOutput(output string) string {
	lines := strings.Split(output, "\n")

	// Trim trailing whitespace on each line.
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}

	var builder strings.Builder
	builder.Grow(len(output))
	blankRun := 0

	for i, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank {
			blankRun++
			if blankRun > 1 {
				continue
			}
		} else {
			blankRun = 0
		}

		builder.WriteString(line)
		if i < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}
