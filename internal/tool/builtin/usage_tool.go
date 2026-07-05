package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"reasonix/internal/tool"
	"reasonix/internal/usage"
)

var (
	usageTrackerMu sync.Mutex
	usageTracker   *usage.Tracker
	usageSessionID string
)

func SetUsageTracker(t *usage.Tracker, sessionID string) {
	usageTrackerMu.Lock()
	defer usageTrackerMu.Unlock()
	usageTracker = t
	usageSessionID = sessionID
}

func getUsageContext() (*usage.Tracker, string, bool) {
	usageTrackerMu.Lock()
	defer usageTrackerMu.Unlock()
	if usageTracker == nil {
		return nil, "", false
	}
	return usageTracker, usageSessionID, true
}

func init() {
	tool.RegisterBuiltin(usageCheckTool{})
}

type usageCheckTool struct{}

func (usageCheckTool) Name() string { return "usage_check" }

func (usageCheckTool) Description() string {
	return "Check the current session's token usage and estimated cost. Shows prompt tokens, completion tokens, cache hits, total cost, and per-source breakdowns."
}

func (usageCheckTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "scope":{
    "type":"string",
    "description":"What to show: 'session' for current session only, 'all' for all sessions, 'summary' for total across all sessions.",
    "enum":["session","all","summary"]
  }
}
}`)
}

func (usageCheckTool) ReadOnly() bool { return true }

func (usageCheckTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	tracker, sessionID, ok := getUsageContext()
	if !ok {
		return "", fmt.Errorf("usage tracking is not available")
	}

	var p struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Scope == "" {
		p.Scope = "session"
	}

	switch p.Scope {
	case "all":
		sessions := tracker.ListRecent(20)
		if len(sessions) == 0 {
			return "No usage data available.", nil
		}
		return fmt.Sprintf("Recent sessions (%d):\n%s", len(sessions), formatRecentSessions(sessions)), nil

	case "summary":
		return tracker.Summary(), nil

	default:
		return tracker.FormatSessionUsage(sessionID), nil
	}
}

func formatRecentSessions(sessions []usage.SessionUsage) string {
	var result string
	for i, s := range sessions {
		if i > 0 {
			result += "\n"
		}
		currency := s.Currency
		if currency == "" {
			currency = "¥"
		}
		result += fmt.Sprintf("  %s: %d tokens, %s%.4f (%d turns)",
			s.SessionID[:min(20, len(s.SessionID))],
			s.TotalTokens, currency, s.Cost, s.Turns)
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
