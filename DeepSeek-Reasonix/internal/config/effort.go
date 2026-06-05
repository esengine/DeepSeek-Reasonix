package config

import (
	"fmt"
	"net/url"
	"strings"
)

// EffortCapability describes the abstract effort levels a provider/model can set
// through the /effort command.
type EffortCapability struct {
	Supported bool
	Levels    []string
	Default   string
}

// EffortCapabilityForEntry returns the user-facing /effort levels for a resolved
// provider entry. Provider implementations still decide how a stored effort is
// serialized into requests.
func EffortCapabilityForEntry(e *ProviderEntry) EffortCapability {
	switch {
	case isDeepSeekEntry(e):
		return EffortCapability{Supported: true, Levels: []string{"auto", "high", "max"}, Default: "high"}
	case e != nil && e.Kind == "anthropic":
		return EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high", "xhigh", "max"}, Default: "auto"}
	default:
		return EffortCapability{}
	}
}

// NormalizeEffort maps a user-supplied /effort level into the value stored in
// config. Empty means auto/provider default.
func NormalizeEffort(e *ProviderEntry, raw string) (string, error) {
	level := strings.ToLower(strings.TrimSpace(raw))
	if level == "" {
		return "", fmt.Errorf("usage: /effort auto|<level>")
	}
	if level == "auto" {
		return "", nil
	}
	switch {
	case isDeepSeekEntry(e):
		switch level {
		case "high", "max":
			return level, nil
		case "low", "medium":
			return "high", nil
		case "xhigh":
			return "max", nil
		default:
			return "", fmt.Errorf("usage: /effort auto|high|max")
		}
	case e != nil && e.Kind == "anthropic":
		switch level {
		case "low", "medium", "high", "xhigh", "max":
			return level, nil
		default:
			return "", fmt.Errorf("usage: /effort auto|low|medium|high|xhigh|max")
		}
	default:
		name := ""
		if e != nil {
			name = e.Name
		}
		if name == "" {
			name = "this model"
		}
		return "", fmt.Errorf("effort is not configurable for %s", name)
	}
}

// EffortDisplay returns the selected /effort level, using "auto" for provider
// default.
func EffortDisplay(e *ProviderEntry) string {
	if e == nil || strings.TrimSpace(e.Effort) == "" {
		return "auto"
	}
	return strings.ToLower(strings.TrimSpace(e.Effort))
}

func isDeepSeekEntry(e *ProviderEntry) bool {
	if e == nil || e.Kind != "openai" {
		return false
	}
	u, err := url.Parse(e.BaseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "api.deepseek.com" || strings.HasSuffix(host, ".deepseek.com")
}
