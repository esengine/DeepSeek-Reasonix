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
	supported := normalizedSupportedEfforts(e)
	if len(supported) > 0 {
		levels := make([]string, 0, len(supported)+1)
		levels = append(levels, "auto")
		levels = append(levels, supported...)
		def := normalizeEffortLevel(e.DefaultEffort)
		if def == "" || !containsString(supported, def) {
			def = supported[0]
		}
		return EffortCapability{Supported: true, Levels: levels, Default: def}
	}
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
	level := normalizeEffortLevel(raw)
	if level == "" {
		return "", fmt.Errorf("usage: /effort auto|<level>")
	}
	if level == "auto" {
		return "", nil
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) > 0 {
		if containsString(supported, level) {
			return level, nil
		}
		return "", fmt.Errorf("usage: /effort auto|%s", strings.Join(supported, "|"))
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
	return normalizeEffortLevel(e.Effort)
}

// EffectiveEffort resolves the provider-visible effort value. Explicit
// ProviderEntry.Effort wins; otherwise a configured SupportedEfforts list makes
// DefaultEffort (or the first supported level) the runtime default. Empty means
// provider default / omit the provider-specific effort field.
func EffectiveEffort(e *ProviderEntry) string {
	if e == nil {
		return ""
	}
	if effort := normalizeEffortLevel(e.Effort); effort != "" {
		return effort
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) == 0 {
		return ""
	}
	def := normalizeEffortLevel(e.DefaultEffort)
	if def == "" || !containsString(supported, def) {
		return supported[0]
	}
	return def
}

func normalizeEffortConfig(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		normalizeProviderEffortFields(&c.Providers[i])
	}
}

func normalizeProviderEffortFields(e *ProviderEntry) {
	if e == nil {
		return
	}
	e.Effort = normalizeEffortLevel(e.Effort)
	if e.Effort == "off" {
		e.Effort = ""
	}
	e.DefaultEffort = normalizeEffortLevel(e.DefaultEffort)
	e.SupportedEfforts = normalizedSupportedEfforts(e)
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

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func normalizeEffortLevel(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizedSupportedEfforts(e *ProviderEntry) []string {
	if e == nil || len(e.SupportedEfforts) == 0 {
		return nil
	}
	out := make([]string, 0, len(e.SupportedEfforts))
	seen := map[string]bool{}
	for _, raw := range e.SupportedEfforts {
		level := normalizeEffortLevel(raw)
		if level == "" || level == "auto" || seen[level] {
			continue
		}
		seen[level] = true
		out = append(out, level)
	}
	return out
}
