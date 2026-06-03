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
// provider entry. When the provider declares SupportedEfforts in its config,
// those take precedence; otherwise built-in heuristics apply (DeepSeek / Anthropic).
func EffortCapabilityForEntry(e *ProviderEntry) EffortCapability {
	if e != nil && len(e.SupportedEfforts) > 0 {
		def := e.DefaultEffort
		if def == "" {
			def = "auto"
		}
		return EffortCapability{Supported: true, Levels: e.SupportedEfforts, Default: def}
	}
	// Built-in provider heuristics (backward compatibility for old TOML configs).
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

	// Config-driven: when the provider declares SupportedEfforts, validate against them.
	if e != nil && len(e.SupportedEfforts) > 0 {
		for _, valid := range e.SupportedEfforts {
			if level == valid {
				return level, nil
			}
		}
		// Level not supported — degrade to DefaultEffort.
		fallback := e.DefaultEffort
		if fallback == "" {
			fallback = "auto"
		}
		// Ultimate safety: if fallback is also not in the list, use the first level.
		if fallback != "auto" {
			found := false
			for _, valid := range e.SupportedEfforts {
				if fallback == valid {
					found = true
					break
				}
			}
			if !found {
				fallback = e.SupportedEfforts[0]
			}
		}
		if fallback == "auto" {
			return "", nil
		}
		return fallback, nil
	}

	// Built-in provider heuristics (backward compatibility).
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
