package boot

import (
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/agentpreset"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/bestof"
	"reasonix/internal/tools/advisor"
	"reasonix/internal/tools/builtin"
	"reasonix/internal/tools/script"
)

// Canonical Agent role-setting identifiers re-exported for frontends that
// already import boot. Prefer agentpreset directly in new code.
const (
	AgentPresetBalanced = string(agentpreset.Balanced)
	AgentPresetDelivery = string(agentpreset.Delivery)
)

// Deprecated TokenMode constants — one compatibility version. Prefer
// AgentPreset* and NormalizeAgentPreset.
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
		"recall",
		"context_budget",
		"use_capability",
	}
}

// HostControlToolNames are collaboration/contract tools that may appear in the
// provider schema independently of the Agent role setting. The turn-exit tools
// are here because the host names them as ways out of an unfinished turn, and a
// name the schema does not carry is one the model cannot call.
func HostControlToolNames() []string {
	return []string{
		"ask",
		"await_user",
		"update_goal",
		"todo_write",
		"complete_step",
	}
}

// GoalOnlyToolNames are host-control tools whose contract exists only inside a
// Goal turn. An assembly that can never arm one drops them from the schema
// rather than shipping a definition the model can only fail to call.
func GoalOnlyToolNames() []string {
	return []string{"update_goal"}
}

// EvidenceToolNames are the tools whose whole purpose is the evidence
// contract, which the evidence arm drops from the schema. todo_write is not
// one, and neither is await_user: neither is an evidence claim, and an arm
// dropping them would measure more than the contract.
func EvidenceToolNames() []string {
	return []string{"complete_step"}
}

// BrowserToolNames are shown when the assembly has a browser to drive.
func BrowserToolNames() []string {
	var names []string
	for _, t := range builtin.BrowserTools(nil) {
		names = append(names, t.Name())
	}
	return names
}

// ComputerToolNames are shown when the host can operate this machine's
// applications.
func ComputerToolNames() []string {
	var names []string
	for _, t := range builtin.ComputerTools(nil) {
		names = append(names, t.Name())
	}
	return names
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

// applyUnifiedProviderToolSurface restricts Schemas/ContractEntries to the
// shared core + host-control tools, plus the tools of the MCP servers named in
// alwaysLoad. use_capability can still Get every registered tool, including
// those hidden from the provider schema.
func applyUnifiedProviderToolSurface(reg *tool.Registry, goalTurnsUnreachable bool, arm ablation.Set, alwaysLoad map[string]bool) {
	if reg == nil {
		return
	}
	unreachable := map[string]bool{}
	if goalTurnsUnreachable {
		for _, name := range GoalOnlyToolNames() {
			unreachable[name] = true
		}
	}
	// The arm has to reach the schema, not only the gate: left visible, both
	// runs pay the same prompt and the same evidence arguments, and the
	// comparison answers for the gate when the question was the contract.
	if arm.Off(ablation.Evidence) {
		for _, name := range EvidenceToolNames() {
			unreachable[name] = true
		}
	}
	// recall keeps its read half, so the arm swaps the tool rather than hiding
	// it: an address the fold index still carries stays readable either way.
	if arm.Off(ablation.RecallSearch) {
		reg.Add(builtin.RecallWithoutSearch())
	}
	allow := make([]string, 0, 16)
	for _, name := range UnifiedProviderToolNames() {
		if unreachable[name] {
			continue
		}
		if _, ok := reg.Get(name); ok {
			allow = append(allow, name)
		}
	}
	// A browser tool is shown only with a browser session behind it, which the
	// config decides at boot, so the schema stays the same for the whole session.
	for _, name := range BrowserToolNames() {
		if t, ok := reg.Get(name); ok && builtin.BrowserBound(t) {
			allow = append(allow, name)
		}
	}
	for _, name := range ComputerToolNames() {
		if t, ok := reg.Get(name); ok && builtin.ComputerBound(t) {
			allow = append(allow, name)
		}
	}
	// advise, best_of_n and run_script are registered only when config asks, at boot.
	for _, name := range []string{advisor.Name, bestof.Name, script.Name} {
		if _, ok := reg.Get(name); ok {
			allow = append(allow, name)
		}
	}
	allow = append(allow, alwaysLoadedMCPTools(reg, alwaysLoad)...)
	// Always keep use_capability if somehow only that remains.
	if len(allow) == 0 {
		if _, ok := reg.Get("use_capability"); ok {
			allow = []string{"use_capability"}
		}
	}
	reg.SetProviderVisibleTools(allow)
}

// ApplyUnifiedProviderToolSurface restricts a registry to what a provider is
// shown, at this arm. Exported so a harness reproduces the real surface
// instead of a second copy of this rule.
func ApplyUnifiedProviderToolSurface(reg *tool.Registry, goalTurnsUnreachable bool, arm ablation.Set) {
	applyUnifiedProviderToolSurface(reg, goalTurnsUnreachable, arm, nil)
}

// pinnedMCPServers is the always-load servers whose schema was known at boot.
// A server discovered during this session waits for the next one, whatever
// the discovery race, so the same config and cache give the same schema.
func pinnedMCPServers(alwaysLoad, schemaKnown map[string]bool) map[string]bool {
	pinned := map[string]bool{}
	for name := range alwaysLoad {
		if schemaKnown[name] {
			pinned[name] = true
		}
	}
	return pinned
}

// alwaysLoadedMCPTools names the registered tools of the always-loaded servers.
// Only a server whose schema is already known at boot contributes: a connect
// stub has no tools to show, and tools arriving after boot would rewrite the
// cached prefix mid-session. An unauthorized server stays behind
// use_capability, which is where its approval is asked for.
func alwaysLoadedMCPTools(reg *tool.Registry, alwaysLoad map[string]bool) []string {
	if len(alwaysLoad) == 0 {
		return nil
	}
	var names []string
	for _, b := range reg.MCPBindings() {
		if !alwaysLoad[b.Server] {
			continue
		}
		if t, ok := reg.Get(b.CallableName); ok && tool.IsMCPServerAuthorized(t) {
			names = append(names, b.CallableName)
		}
	}
	return names
}
