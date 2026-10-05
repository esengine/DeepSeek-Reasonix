package config

import "strings"

// mcpIdentity returns the fixed part of a server's override identity: whether
// its scope is forced, and the disambiguators that keep two packages exposing
// the same short server name apart.
func mcpIdentity(entry PluginEntry) (forcedProject bool, source, owner string) {
	source = strings.TrimSpace(string(entry.Source))
	if entry.Source == MCPSourcePluginPackage {
		return false, source, source
	}
	// Project-scoped by nature: a global row would reach another project's
	// same-named declaration. Unknown provenance counts as repository-declared,
	// because Source survives only a merge that succeeded.
	if entry.Source.ProjectScoped() || source == "" || source == "workspace_config" || source == "project" || source == ".mcp.json" {
		return true, source, ""
	}
	return false, source, ""
}

// ServerOverrideFor builds the override identity for entry at scope. Callers
// name the scope they mean; a repository-declared server is pinned to the
// project regardless of what was asked for.
func ServerOverrideFor(entry PluginEntry, root string, scope ActivationScope) ActivationOverride {
	forcedProject, source, owner := mcpIdentity(entry)
	if forcedProject {
		scope = ActivationProject
	}
	override := ActivationOverride{
		Kind:   CapabilityMCP,
		Scope:  scope,
		Source: source,
		Owner:  owner,
		Name:   strings.TrimSpace(entry.Name),
	}
	return placeProjectRow(override, root)
}

// ServerDecision reports what the store holds for entry in root, and whether an
// undecided one may start anyway. A repository-declared one may not: .mcp.json
// and a project reasonix.toml both arrive with a clone, so whoever wrote the
// repo chose the command. Every other source is a file only the user writes.
func (s *ActivationStore) ServerDecision(entry PluginEntry, root string) (ActivationDecision, bool, error) {
	repoDeclared, _, _ := mcpIdentity(entry)
	decision, err := s.decide(ServerOverrideFor(entry, root, ActivationGlobal), root, repoDeclared)
	return decision, DeclaredDefaultOn(entry), err
}

// DeclaredDefaultOn is what a server does when the store cannot be read. Every
// error fallback must use it rather than auto_start, which would re-enable
// exactly the servers the gate is for.
func DeclaredDefaultOn(entry PluginEntry) bool {
	repoDeclared, _, _ := mcpIdentity(entry)
	return !repoDeclared && entry.ShouldAutoStart()
}

// IsEnabled resolves the product enable state for entry in root. An override
// wins, project layer first; without one a repository-declared server stays off
// and anything else follows auto_start.
func (s *ActivationStore) IsEnabled(entry PluginEntry, root string) (bool, error) {
	decision, defaultOn, err := s.ServerDecision(entry, root)
	if err != nil {
		return false, err
	}
	if decision != ActivationUndecided {
		return decision == ActivationEnabled, nil
	}
	return defaultOn, nil
}

// AwaitingDecision reports a repository-declared server nobody has answered for.
// It is off, but not because the user turned it off — a surface that cannot tell
// those apart shows a project's MCP as simply missing.
func (s *ActivationStore) AwaitingDecision(entry PluginEntry, root string) bool {
	decision, defaultOn, err := s.ServerDecision(entry, root)
	if err != nil {
		return !DeclaredDefaultOn(entry) // unread store: ask again rather than go quiet
	}
	return decision == ActivationUndecided && !defaultOn
}

// SetServerEnabled records a durable decision for entry at scope.
func (s *ActivationStore) SetServerEnabled(entry PluginEntry, root string, scope ActivationScope, enabled bool) error {
	override := ServerOverrideFor(entry, root, scope)
	override.Enabled = enabled
	return s.SetOverride(override)
}

// ClearServer removes entry's override at scope, restoring what it inherits.
func (s *ActivationStore) ClearServer(entry PluginEntry, root string, scope ActivationScope) error {
	return s.ClearOverride(ServerOverrideFor(entry, root, scope))
}

// ClearServerEverywhere drops entry's rows at both layers. Uninstall uses it:
// leaving a row behind would silently govern a same-named server installed
// later. Only this workspace's project row is reachable — another project's row
// is keyed by an identity this process cannot enumerate.
func (s *ActivationStore) ClearServerEverywhere(entry PluginEntry, root string) error {
	if err := s.ClearServer(entry, root, ActivationGlobal); err != nil {
		return err
	}
	return s.ClearServer(entry, root, ActivationProject)
}

// ServerOverride returns the stored row for entry at scope, so a caller that
// must undo its own write can put back exactly what was there.
func (s *ActivationStore) ServerOverride(entry PluginEntry, root string, scope ActivationScope) (ActivationOverride, bool, error) {
	if s == nil {
		return ActivationOverride{}, false, nil
	}
	file, err := s.Load()
	if err != nil {
		return ActivationOverride{}, false, err
	}
	row, ok := findOverride(file.Overrides, overrideKey(ServerOverrideFor(entry, root, scope)))
	return row, ok, nil
}
