package boot

import (
	"reasonix/internal/agentpreset"
)

// Role vocabulary re-exported for old frontends. Runtime constraints live in
// internal/runtimepolicy; these names only parse compat inputs.

// AgentPreset label constants; light and its aliases fold to standard.
const (
	AgentPresetStandard = string(agentpreset.Standard)
	AgentPresetDelivery = string(agentpreset.Delivery)
)

// Deprecated compat aliases for one-version-old callers.
const (
	AgentPresetBalanced = AgentPresetStandard
	TokenModeFull       = "full"
	TokenModeDelivery   = "delivery"
)

// NormalizeAgentPreset maps free-form and legacy values to the canonical
// role label. Light folds to standard; unknown values return the input
// unchanged so callers can surface an error.
func NormalizeAgentPreset(raw string) string {
	if p, err := agentpreset.Normalize(raw); err == nil {
		return string(p)
	}
	return raw
}

// NormalizeAgentPresetErr is NormalizeAgentPreset reporting parse errors.
func NormalizeAgentPresetErr(raw string) (string, error) {
	p, err := agentpreset.Normalize(raw)
	return string(p), err
}

// NormalizeTokenMode is the deprecated alias that returns legacy tokenMode
// names (full/delivery) for dual-write and older clients.
func NormalizeTokenMode(mode string) string {
	return agentpreset.LegacyTokenMode(agentpreset.FromLegacyTokenMode(mode))
}

// AgentPresetFromTokenMode maps a legacy tokenMode onto a role setting.
func AgentPresetFromTokenMode(mode string) string {
	return string(agentpreset.FromLegacyTokenMode(mode))
}

// TokenModeFromAgentPreset maps a role setting onto the dual-write tokenMode.
func TokenModeFromAgentPreset(preset string) string {
	return agentpreset.LegacyTokenMode(agentpreset.FromLegacyTokenMode(preset))
}

// CoreProviderToolNames is the stable top-level tool surface shared by every
// Agent role setting under identical configuration. Host-control tools
// (ask, finish, update_goal, todo_write, complete_step) are appended when enabled.
func CoreProviderToolNames() []string {
	return []string{
		"bash",
		"bash_output",
		"kill_shell",
		"wait",
		"read_file",
		"edit_file",
		"write_file",
		"compress",
		"use_capability",
	}
}

// HostControlToolNames are collaboration/contract tools that may appear in the
// provider schema independently of the Agent role setting.
func HostControlToolNames() []string {
	return []string{
		"ask",
		"finish",
		"update_goal",
		"todo_write",
		"complete_step",
	}
}

// UnifiedProviderToolNames returns the provider-visible allowlist for a boot
// with host-control tools enabled.
func UnifiedProviderToolNames() []string {
	core := CoreProviderToolNames()
	host := HostControlToolNames()
	out := make([]string, 0, len(core)+len(host))
	out = append(out, core...)
	out = append(out, host...)
	return out
}
