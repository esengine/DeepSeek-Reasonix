package agent

import (
	"regexp"
	"strings"

	"reasonix/internal/provider"
)

var exitCodePattern = regexp.MustCompile(`(?i)exit (status|code)[:= ]+(-?\d+)`)

func errorCategory(toolName, errMsg string) string {
	msg := firstLine(errMsg)
	if strings.HasPrefix(msg, "argument_validation:") {
		return msg
	}
	if m := exitCodePattern.FindStringSubmatch(msg); len(m) == 3 {
		return toolName + ":exit:" + m[2]
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return toolName + ":transient:timeout"
	case strings.Contains(lower, "connection"):
		return toolName + ":transient:connection"
	case strings.Contains(lower, "fail") && strings.Contains(lower, "test"):
		return toolName + ":test_failure"
	}
	return toolName + ":" + msg
}

func consecutiveNormalizedFailure(calls []provider.ToolCall, outcomes []toolOutcome, loop *turnLoopState) bool {
	if loop == nil {
		return false
	}
	categories := map[string]int{}
	for i := 0; i < len(calls) && i < len(outcomes); i++ {
		if strings.TrimSpace(outcomes[i].errMsg) == "" {
			continue
		}
		name := calls[i].Name
		if calls[i].ResolvedName != "" {
			name = calls[i].ResolvedName
		}
		category := errorCategory(name, outcomes[i].errMsg)
		if strings.Contains(category, ":exit:") || strings.Contains(category, ":test_failure") {
			categories[category]++
		}
	}
	return loop.advanceErrorCategories(categories)
}
