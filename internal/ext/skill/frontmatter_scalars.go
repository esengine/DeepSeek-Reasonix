package skill

import "strings"

func parseBoolFrontmatter(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes", "1", "on":
		return true
	default:
		return false
	}
}

func parseCost(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func parseAutoUse(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "off", "suggest", "prefer", "require":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

// parseInvocation maps frontmatter to an invocation mode. Anything other than
// "manual" (including absent) is "auto" — the existing, universal behavior.
func parseInvocation(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "manual") {
		return "manual"
	}
	return "auto"
}
