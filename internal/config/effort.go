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

// ResolveResult is the outcome of resolving a user-chosen effort level against a
// provider's capability.
type ResolveResult struct {
	Effort   string // wire-format level; "" means auto (caller omits param)
	Blocked  bool   // provider doesn't support effort at all; UI should hide switcher
	Degraded bool   // user's choice was outside capability; fell back
	Warning  string // human-readable note for the UI/CLI to surface
}

// ResolveEffort is the single decision point for mapping a user's chosen effort
// level to the wire-format value. Pure function: no globals, no provider lookups.
func ResolveEffort(caps EffortCapability, chosen string) ResolveResult {
	if !caps.Supported {
		return ResolveResult{Blocked: true}
	}

	level := strings.ToLower(strings.TrimSpace(chosen))
	if level == "" || level == "auto" {
		return ResolveResult{} // Effort="" = auto = omit from request
	}

	// Exact match: pass through.
	for _, l := range caps.Levels {
		if level == l {
			return ResolveResult{Effort: level}
		}
	}

	// Level not in capability — degrade to Default.
	fallback := caps.Default
	if fallback == "" {
		fallback = "auto"
	}
	// Ultimate safety: if fallback is also not in the list, use the first level.
	if fallback != "auto" && !containsLevel(caps.Levels, fallback) {
		if len(caps.Levels) > 0 {
			fallback = caps.Levels[0]
		} else {
			fallback = "auto"
		}
	}
	if fallback == "auto" {
		return ResolveResult{Degraded: true, Warning: fmt.Sprintf("level %q not supported, using auto", chosen)}
	}
	return ResolveResult{Effort: fallback, Degraded: true, Warning: fmt.Sprintf("level %q not supported, using %q", chosen, fallback)}
}

func containsLevel(levels []string, target string) bool {
	for _, l := range levels {
		if l == target {
			return true
		}
	}
	return false
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
		return EffortCapability{Supported: true, Levels: []string{"auto", "high", "max"}, Default: "auto"}
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
func EffortDisplay(effort string) string {
	if strings.TrimSpace(effort) == "" {
		return "auto"
	}
	return strings.ToLower(strings.TrimSpace(effort))
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
