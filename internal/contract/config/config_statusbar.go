package config

import "strings"

var defaultDesktopStatusBarItems = []string{
	"model",
	"workspace",
	"git_branch",
	"cache",
	"cache_avg",
	"session_tokens",
	"turn_tokens",
	"turn_tps",
	"turn_output_tokens",
	"turn_cache_tokens",
	"turn_cost",
	"session_turns",
	"context",
	"compact",
	"cost",
	"balance",
}

var knownDesktopStatusBarItems = desktopStatusBarItemSet(defaultDesktopStatusBarItems)

func desktopStatusBarItemSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

// DefaultDesktopStatusBarItems returns the default ordered visible desktop
// status bar items.
func DefaultDesktopStatusBarItems() []string {
	return append([]string(nil), defaultDesktopStatusBarItems...)
}

// DesktopStatusBarItems normalizes the ordered visible desktop status bar items.
// An unset or empty list uses the default full set; explicit non-empty lists
// preserve user order and omit hidden items.
func (c *Config) DesktopStatusBarItems() []string {
	return normalizeDesktopStatusBarItems(c.Desktop.StatusBarItems)
}

func normalizeDesktopStatusBarItems(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, raw := range items {
		id := strings.TrimSpace(raw)
		if !knownDesktopStatusBarItems[id] || seen[id] {
			continue
		}
		out = append(out, id)
		seen[id] = true
	}
	if len(out) == 0 {
		return DefaultDesktopStatusBarItems()
	}
	return out
}
