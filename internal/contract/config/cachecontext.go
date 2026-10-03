package config

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"reasonix/internal/base/workspaceid"
)

// maxCacheContextLen is DeepSeek's user_id length ceiling (512).
const maxCacheContextLen = 512

// maxSessionContextLen is OpenRouter's session_id length ceiling (256).
const maxSessionContextLen = 256

// cacheContextIDRegexp matches the characters providers reject in a user_id.
var cacheContextIDRegexp = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// EffectiveCacheContext is the per-workspace attribution id sent to providers as
// user_id (Anthropic metadata.user_id, OpenAI user). It is the literal
// cachecontext when set to anything but "auto"; the literal "auto" derives it
// from the workspace key; unset sends nothing. The id is never derived from the
// local account name.
func (c *Config) EffectiveCacheContext(root string) string {
	if c == nil {
		return ""
	}
	id := strings.TrimSpace(c.CacheContext)
	switch id {
	case "":
		return ""
	case "auto":
		if strings.TrimSpace(root) == "" {
			return ""
		}
		return boundContextID(workspaceid.Key(root), maxCacheContextLen)
	default:
		return boundContextID(id, maxCacheContextLen)
	}
}

// EffectiveSessionContext is the same workspace id bounded to OpenRouter's
// shorter session_id ceiling, so a project keeps one stable key per provider.
func (c *Config) EffectiveSessionContext(root string) string {
	id := c.EffectiveCacheContext(root)
	if id == "" {
		return ""
	}
	return boundContextID(id, maxSessionContextLen)
}

// boundContextID sanitizes an id to ^[a-zA-Z0-9_-]+$ and, when it exceeds
// maxLen, keeps the leading maxLen-17 characters and appends a hash of the whole
// so the result is exactly maxLen and distinct ids stay distinct.
func boundContextID(s string, maxLen int) string {
	s = sanitizeCacheContext(strings.TrimSpace(s))
	if len(s) <= maxLen {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	return s[:maxLen-17] + "-" + hex.EncodeToString(sum[:8])
}

// sanitizeCacheContext rewrites characters providers would reject as "-".
func sanitizeCacheContext(s string) string {
	return cacheContextIDRegexp.ReplaceAllString(s, "-")
}

// cacheAttribution is the per-workspace id pair stamped on a provider entry at
// load; both fields come from one EffectiveCacheContext value.
type cacheAttribution struct {
	userID    string
	sessionID string
}

// stampAttribution assigns the workspace id pair to every provider entry, so one
// shared provider entry carries this workspace's id without repeating it.
func stampAttribution(cfg *Config, root string) {
	userID := cfg.EffectiveCacheContext(root)
	sessionID := cfg.EffectiveSessionContext(root)
	for i := range cfg.Providers {
		cfg.Providers[i].attribution = cacheAttribution{userID: userID, sessionID: sessionID}
	}
}

// CacheContextValue returns the workspace user attribution id assigned at load
// (Config.EffectiveCacheContext). Empty when unset or the entry was built
// outside a workspace config.
func (e *ProviderEntry) CacheContextValue() string {
	if e == nil {
		return ""
	}
	return e.attribution.userID
}

// SessionContextValue returns the workspace OpenRouter session_id assigned at
// load (Config.EffectiveSessionContext). Empty when unset or the entry was built
// outside a workspace config.
func (e *ProviderEntry) SessionContextValue() string {
	if e == nil {
		return ""
	}
	return e.attribution.sessionID
}
