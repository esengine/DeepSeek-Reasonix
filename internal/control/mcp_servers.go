package control

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/plugin"
)

// AddMCPServer connects an MCP server live and persists it to the user-global
// config. Its tools are registered immediately and become available on the next
// turn (the agent reads the registry per turn). The raw entry — ${VARS} intact —
// is what's written to disk; the live connection uses the expanded form. Returns
// the number of tools the server exposed. Persistence is transactional: a config
// or activation failure removes the just-connected client so the live registry
// never claims an install that will disappear after restart.
func (c *Controller) AddMCPServer(e config.PluginEntry) (int, error) {
	// AddMCPServer is an explicit user action. Mark the live entry with the same
	// provenance it will receive when the saved user config is loaded next time,
	// so /mcp add is add-and-use in the current session too.
	e.Source = config.MCPSourceUserConfig
	if effective, loadErr := config.LoadForRootReadOnly(c.workspaceRoot); loadErr != nil {
		return 0, loadErr
	} else {
		for _, configured := range effective.Plugins {
			if configured.Name != e.Name {
				continue
			}
			if configured.Source != config.MCPSourceUserConfig && configured.Source != config.MCPSourceLegacyUser {
				return 0, fmt.Errorf("MCP server %q is already configured by %s; edit or remove that declaration before installing a global server with the same name", e.Name, configured.Source)
			}
			break
		}
	}
	n, err := c.connectMCPServer(e)
	if err != nil {
		return 0, err
	}
	if _, err := config.InstallUserPluginForRoot(c.workspaceRoot, e, true); err != nil {
		c.DisconnectMCPServer(e.Name)
		return 0, fmt.Errorf("saving MCP server config: %w", err)
	}
	return n, nil
}

// ConnectMCPServer connects an MCP server entry for this session without writing
// it to config. Desktop owns config placement so it can keep user-level settings
// out of project reasonix.toml while preserving the CLI AddMCPServer semantics.
func (c *Controller) ConnectMCPServer(e config.PluginEntry) (int, error) {
	return c.connectMCPServer(e)
}

// RegisterMCPServerOnDemand restores a configured server's cached provider
// surface without forcing a handshake. It is the durable-enable counterpart to
// ConnectMCPServer, which remains the explicit install/retry operation.
func (c *Controller) RegisterMCPServerOnDemand(e config.PluginEntry) (int, error) {
	spec := c.mcpSpec(e)
	n, err := c.mcp.registerSpecOnDemand(spec)
	if err == nil && c.capabilityRuntime != nil {
		c.capabilityRuntime.UpsertServer(e, spec, true)
	}
	return n, err
}

// connectMCPServer expands an entry's ${VARS}, applies the known-server
// overrides scoped to the workspace, and connects it live via the mcp manager.
func (c *Controller) connectMCPServer(e config.PluginEntry) (int, error) {
	spec := c.mcpSpec(e)
	n, err := c.mcp.connectSpec(spec)
	if err == nil && c.capabilityRuntime != nil {
		c.capabilityRuntime.UpsertServer(e, spec, true)
	}
	return n, err
}

func (c *Controller) mcpSpec(e config.PluginEntry) plugin.Spec {
	exp := e.ExpandedPlugin()
	spec := mcpIdentitySpec(e, c.WorkspaceRoot())
	spec.StartupTimeout = controllerMCPTimeout(exp.StartupTimeoutSeconds)
	spec.DefaultCallTimeout = c.mcp.defaultCallTimeout
	spec.CallTimeout = controllerMCPTimeout(exp.CallTimeoutSeconds)
	spec.ToolTimeouts = controllerMCPToolTimeouts(exp.ToolTimeoutSeconds)
	spec.Authorized = exp.Source.UserAuthorized()
	// Explicit user installs and reconnects run as trusted host processes.
	spec.ProcessMode = plugin.MCPProcessHost
	if c.mcpConfigureSpec != nil {
		c.mcpConfigureSpec(&spec)
		if spec.ProcessMode == "" {
			spec.ProcessMode = plugin.MCPProcessHost
		}
	}
	return spec
}

// syncCapabilityRuntimeFromConfig restores one server's authoritative runtime
// entry after a transactional disconnect/rollback. enabledOverride is used for
// a session-only disconnect; nil re-resolves the durable activation state.
func (c *Controller) syncCapabilityRuntimeFromConfig(name string, enabledOverride *bool) {
	if c == nil || c.capabilityRuntime == nil {
		return
	}
	name = strings.TrimSpace(name)
	cfg, err := config.LoadForRoot(c.workspaceRoot)
	if err != nil {
		// The caller revokes first. A config read failure must not re-enable a
		// potentially stale spec or shared-Host client.
		return
	}
	for _, entry := range cfg.Plugins {
		if strings.TrimSpace(entry.Name) != name {
			continue
		}
		enabled := config.DeclaredDefaultOn(entry)
		if enabledOverride != nil {
			enabled = *enabledOverride
		} else if resolved, resolveErr := config.DefaultActivationStore().IsEnabled(entry, c.workspaceRoot); resolveErr == nil {
			enabled = resolved
		}
		c.capabilityRuntime.UpsertServer(entry, c.mcpSpec(entry), enabled)
		return
	}
	c.capabilityRuntime.RemoveServer(name)
}

// ImportMCPEntries persists selected MCP entries and attempts to connect them
// live. A connection failure does not roll back the config import: the user can
// fix local dependencies and reconnect in a later session.
func (c *Controller) ImportMCPEntries(entries []config.PluginEntry) (total, added, updated, connected, failed, skipped int, err error) {
	total, added, updated, err = config.ImportCCSwitchMCPEntries(entries)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	effectiveCfg, loadErr := config.LoadForRoot(c.workspaceRoot)
	if loadErr != nil {
		return 0, 0, 0, 0, 0, 0, loadErr
	}
	effective := make(map[string]config.PluginEntry, len(effectiveCfg.Plugins))
	for _, entry := range effectiveCfg.Plugins {
		effective[entry.Name] = entry
	}
	for _, imported := range entries {
		e, ok := effective[imported.Name]
		if !ok || e.Source != config.MCPSourceUserConfig {
			// A project declaration with the same name remains effective. The
			// imported global entry is saved as its lower-priority fallback.
			skipped++
			continue
		}
		if c.mcp.hasServer(e.Name) {
			if c.capabilityRuntime != nil {
				// Import updates may intentionally keep an existing live client, but
				// future proxy reconnects must use the newly persisted spec.
				c.capabilityRuntime.UpsertServer(e, c.mcpSpec(e), true)
			}
			skipped++
			continue
		}
		if _, err := c.AddMCPServer(e); err != nil {
			failed++
			continue
		}
		connected++
	}
	return total, added, updated, connected, failed, skipped, nil
}

func (c *Controller) ConfiguredMCPNames() []string {
	cfg, err := config.LoadForRootReadOnly(c.workspaceRoot)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		names = append(names, p.Name)
	}
	return names
}

func (c *Controller) DisconnectedMCPNames() []string {
	cfg, err := config.LoadForRootReadOnly(c.workspaceRoot)
	if err != nil {
		return nil
	}
	connected := map[string]bool{}
	for _, name := range c.mcp.serverNames() {
		connected[name] = true
	}
	var names []string
	for _, p := range cfg.Plugins {
		if !connected[p.Name] {
			names = append(names, p.Name)
		}
	}
	return names
}

func (c *Controller) ConnectConfiguredMCPServer(name string) (int, error) {
	p, err := c.configuredMCPServer(name)
	if err != nil {
		return 0, err
	}
	c.approveOnExplicitConnect(p)
	return c.connectMCPServer(p)
}

func (c *Controller) configuredMCPServer(name string) (config.PluginEntry, error) {
	cfg, err := config.LoadForRoot(c.workspaceRoot)
	if err != nil {
		return config.PluginEntry{}, err
	}
	for _, p := range cfg.Plugins {
		if p.Name == name {
			return p, nil
		}
	}
	return config.PluginEntry{}, &config.ServerNotFoundError{Name: name}
}

// RemoveMCPServer removes writable config before disconnecting the live server.
// A persistence failure must not produce a false-successful session-only removal.
// MCPs contributed by installed plugin packages cannot be removed independently.
func (c *Controller) RemoveMCPServer(name string) (disconnected bool, err error) {
	cfg, lerr := config.LoadForRoot(c.workspaceRoot)
	if lerr != nil {
		return false, lerr
	}
	if owner, ok := cfg.PluginPackageOwner(name); ok {
		return false, fmt.Errorf("MCP server %q is managed by plugin %q; disable or remove the plugin instead", name, owner)
	}
	entry, removed, _, rerr := config.RemovePluginFromEffectiveSourceForRoot(c.workspaceRoot, name)
	if rerr != nil {
		return false, rerr
	}
	if !removed {
		return false, fmt.Errorf("no removable MCP server named %q", name)
	}
	_ = config.DefaultActivationStore().ClearServerEverywhere(entry, c.workspaceRoot)
	removedState := reconcileRemovedMCPState(c.workspaceRoot, name)
	if c.capabilityRuntime != nil {
		// Revoke before touching the shared Host so an overlapping resolver cannot
		// reuse a sibling tab's still-connected client.
		c.capabilityRuntime.RemoveServer(name)
	}
	disconnected = c.mcp.disconnect(name)
	if !disconnected {
		c.mcp.removeToolPrefix(name)
	}
	// A lower-priority same-name declaration may now be effective. Restore its
	// cached/on-demand surface without starting a process; otherwise ensure the
	// removed name stays absent.
	if removedState.fallbackFound {
		enabled := config.DeclaredDefaultOn(removedState.fallback)
		if resolved, resolveErr := config.DefaultActivationStore().IsEnabled(removedState.fallback, c.workspaceRoot); resolveErr == nil {
			enabled = resolved
		}
		if enabled {
			_, _ = c.RegisterMCPServerOnDemand(removedState.fallback)
		} else {
			c.syncCapabilityRuntimeFromConfig(name, &enabled)
		}
	} else {
		c.syncCapabilityRuntimeFromConfig(name, nil)
	}
	return disconnected, removedState.cleanupErr
}

// DisconnectMCPServer disconnects a live server for this session without touching
// config — the connector toggle's "off". Its tools vanish next turn; it reconnects
// on the next session start, or now via ConnectConfiguredMCPServer (the "on").
// Reports whether a live server was actually disconnected.
func (c *Controller) DisconnectMCPServer(name string) bool {
	if c.capabilityRuntime != nil {
		c.capabilityRuntime.SetServerEnabled(name, false)
	}
	disconnected := c.mcp.disconnect(name)
	removedPlaceholder := 0
	if !disconnected {
		removedPlaceholder = c.mcp.removeToolPrefix(name)
	}
	// Keep configured servers discoverable as disabled, but forget runtime-only
	// or rolled-back installs that no longer exist in configuration.
	disabled := false
	c.syncCapabilityRuntimeFromConfig(name, &disabled)
	return disconnected || removedPlaceholder > 0
}

// UnregisterMCPServerTools hides a shared MCP server from this controller only.
// The desktop shared-host path uses this for per-tab connector toggles: the
// shared client stays alive for sibling tabs, while this session's registry drops
// the server's provider-visible tools before the next turn.
func (c *Controller) UnregisterMCPServerTools(name string) bool {
	if c.capabilityRuntime != nil {
		c.capabilityRuntime.SetServerEnabled(name, false)
	}
	return c.mcp.suspendToolPrefix(name)
}
