package provider

import "strings"

// Attribution is the per-workspace id pair a provider sends upstream: UserID as
// DeepSeek user_id / OpenAI user / Anthropic metadata.user_id, and SessionID as
// OpenRouter's top-level session_id. Both are empty unless the workspace
// configured them.
type Attribution struct {
	UserID    string
	SessionID string
}

// AttributionFromExtra reads the pair the config stamped into a provider's Extra
// map.
func AttributionFromExtra(extra map[string]any) Attribution {
	userID, _ := extra["user_id"].(string)
	sessionID, _ := extra["session_id"].(string)
	return Attribution{UserID: strings.TrimSpace(userID), SessionID: strings.TrimSpace(sessionID)}
}
