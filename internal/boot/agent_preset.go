package boot

import (
	"reasonix/internal/agentpreset"
)

// Deprecated execution-mode vocabulary re-exported for old frontends.
// Runtime constraints live in internal/runtimepolicy; these names only parse compat inputs.

// Deprecated AgentPreset label constants — one compatibility version.
const (
	AgentPresetLight    = string(agentpreset.Light)
	AgentPresetBalanced = string(agentpreset.Balanced)
	AgentPresetDelivery = string(agentpreset.Delivery)
)

// Deprecated TokenMode constants — one compatibility version.
const (
	TokenModeFull     = "full"
	TokenModeEconomy  = "economy"
	TokenModeDelivery = "delivery"
)

// NormalizeAgentPreset maps free-form and legacy values to a canonical
// agent role setting. Empty/unknown → balanced.
func NormalizeAgentPreset(raw string) string {
	return string(agentpreset.Normalize(raw))
}

// NormalizeTokenMode is the deprecated alias that returns legacy tokenMode
// names (economy/full/delivery) for dual-write and older clients.
func NormalizeTokenMode(mode string) string {
	return agentpreset.LegacyTokenMode(agentpreset.Normalize(mode))
}

// AgentPresetFromTokenMode maps a legacy tokenMode onto a role setting.
func AgentPresetFromTokenMode(mode string) string {
	return string(agentpreset.FromLegacyTokenMode(mode))
}

// TokenModeFromAgentPreset maps a role setting onto the dual-write tokenMode.
func TokenModeFromAgentPreset(preset string) string {
	return agentpreset.LegacyTokenMode(agentpreset.Normalize(preset))
}

// CoreProviderToolNames is the stable top-level tool surface shared by every
// Agent role setting under identical configuration. Host-control tools
// (ask, update_goal, todo_write, complete_step) are appended when enabled.
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
