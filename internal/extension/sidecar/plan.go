package sidecar

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/secrets"
)

// PluginComponentID returns the dependency-graph component ID for an installed
// native runtime package.
func PluginComponentID(pluginName string) extension.ComponentID {
	return extension.ComponentID("plugin/" + strings.TrimSpace(pluginName))
}

// PluginNameFromComponentID extracts the plugin package name from a
// plugin/<name> component ID. Non-plugin IDs return "".
func PluginNameFromComponentID(id extension.ComponentID) string {
	const prefix = "plugin/"
	s := string(id)
	if !strings.HasPrefix(s, prefix) {
		return ""
	}
	return strings.TrimSpace(s[len(prefix):])
}

// StartPackagesWithPlan starts Added/Reloaded packages and adopts Unchanged
// clients from previous. previous may be nil. Required start failures fail
// the whole call and close the new Manager's resources.
func StartPackagesWithPlan(ctx context.Context, home string, sessionCtx protocol.SessionContext, ui UIHandler, previous *Manager, plan *extension.RuntimePlan) (*Manager, []string, error) {
	packages, warnings := LoadRuntimePackages(home)
	if plan == nil || plan.IsNoOp() && previous == nil {
		startupCtx, cancel := context.WithTimeout(ctx, packageStartupBudget)
		defer cancel()
		m, runtimeWarnings, err := startLoadedPackages(startupCtx, packages, sessionCtx, ui, StartClient)
		warnings = append(warnings, runtimeWarnings...)
		return m, warnings, err
	}
	if plan.IsNoOp() && previous != nil {
		// No component change: adopt every live client from previous.
		return adoptAll(previous), warnings, nil
	}

	activate := map[string]bool{}
	for _, id := range plan.Added {
		if name := PluginNameFromComponentID(id); name != "" {
			activate[name] = true
		}
	}
	for _, id := range plan.Reloaded {
		if name := PluginNameFromComponentID(id); name != "" {
			activate[name] = true
		}
	}
	unchanged := map[string]bool{}
	for _, id := range plan.Unchanged {
		if name := PluginNameFromComponentID(id); name != "" {
			unchanged[name] = true
		}
	}

	var toStart []pluginpkg.InstalledPackage
	for _, item := range packages {
		name := item.Installed.Name
		if activate[name] {
			toStart = append(toStart, item)
		}
	}

	startupCtx, cancel := context.WithTimeout(ctx, packageStartupBudget)
	defer cancel()
	m, runtimeWarnings, err := startLoadedPackages(startupCtx, toStart, sessionCtx, ui, StartClient)
	warnings = append(warnings, runtimeWarnings...)
	if n := len(toStart); n > 0 {
		extension.DefaultLifecycleMetrics.SidecarStarts.Add(uint64(n))
	}
	if err != nil {
		return m, warnings, err
	}

	// Adopt unchanged clients from previous. Detach so previous.Close after
	// publish does not kill still-active packages. Track detaches so a later
	// activation failure can reattach ONLY these unchanged clients.
	m.planAdopted = map[string]*Client{}
	rollback := func() {
		m.RollbackPlanStart(previous)
	}
	if previous != nil {
		for name := range unchanged {
			client := previous.Detach(name)
			if client == nil {
				// Unchanged in the graph but no live client: start it now.
				for _, item := range packages {
					if item.Installed.Name != name {
						continue
					}
					fresh, startErr := startOne(startupCtx, item, sessionCtx, ui)
					if startErr != nil {
						if item.Package.Manifest.Runtime != nil && item.Package.Manifest.Runtime.Required {
							rollback()
							return nil, warnings, &RequiredStartError{Plugin: name, Err: startErr}
						}
						warnings = append(warnings, fmt.Sprintf("%s: optional extension runtime failed to start: %v", name, startErr))
						break
					}
					if adoptErr := m.Adopt(name, fresh); adoptErr != nil {
						_ = fresh.Close()
						rollback()
						return nil, warnings, adoptErr
					}
					break
				}
				continue
			}
			if adoptErr := m.Adopt(name, client); adoptErr != nil {
				if reattachErr := previous.Adopt(name, client); reattachErr != nil {
					_ = client.Close()
				}
				rollback()
				return nil, warnings, adoptErr
			}
			m.planAdopted[name] = client
			extension.DefaultLifecycleMetrics.SidecarAdopts.Add(1)
		}
	}
	return m, warnings, nil
}

func adoptAll(previous *Manager) *Manager {
	m := &Manager{clients: make(map[string]*Client)}
	if previous == nil {
		return m
	}
	for _, client := range previous.Clients() {
		id := client.PluginID()
		if c := previous.Detach(id); c != nil {
			m.clients[id] = c
		}
	}
	return m
}

func startOne(ctx context.Context, item pluginpkg.InstalledPackage, sessionCtx protocol.SessionContext, ui UIHandler) (*Client, error) {
	pluginID := item.Installed.Name
	var binder UIBinder
	if b, ok := ui.(UIBinder); ok {
		binder = b
	}
	clientUI := ui
	if binder != nil {
		clientUI = binder.HandlerFor(pluginID)
	}
	return StartClient(ctx, ClientOptions{
		Package:   item.Package,
		Installed: item.Installed,
		Session:   sessionCtx,
		UI:        clientUI,
		OnCrash: func(err error) {
			slog.Warn("extension sidecar crashed", "plugin", pluginID, "err", secrets.RedactError(err))
			if binder != nil {
				binder.ClientCrashed(pluginID)
			}
		},
	})
}

// Detach removes a client from the manager without closing it. Returns nil
// when the plugin is not present or the manager is closed.
func (m *Manager) Detach(pluginID string) *Client {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	client := m.clients[pluginID]
	delete(m.clients, pluginID)
	return client
}

// Adopt takes ownership of an already-running client. Fails if the manager is
// closed or the plugin ID is already registered.
func (m *Manager) Adopt(pluginID string, client *Client) error {
	if m == nil {
		return fmt.Errorf("sidecar: nil Manager")
	}
	if client == nil {
		return fmt.Errorf("sidecar: nil Client")
	}
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return fmt.Errorf("sidecar: empty plugin id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("sidecar: manager closed")
	}
	if m.clients == nil {
		m.clients = make(map[string]*Client)
	}
	if _, exists := m.clients[pluginID]; exists {
		return fmt.Errorf("sidecar: plugin %q already registered", pluginID)
	}
	m.clients[pluginID] = client
	return nil
}

// Drain closes the listed plugin clients (if present) and removes them. Other
// clients are left running. Used after publish to retire Removed/Reloaded
// packages still held by the old generation's manager.
func (m *Manager) Drain(pluginIDs ...string) {
	if m == nil {
		return
	}
	for _, id := range pluginIDs {
		if c := m.Detach(id); c != nil {
			_ = c.Close()
		}
	}
}

// RollbackPlanStart undoes StartPackagesWithPlan ownership transfer:
//   - Unchanged clients recorded in planAdopted are reattached to previous
//   - Remaining clients (Added/Reloaded fresh starts) are closed via m.Close()
//
// Adopt errors are not swallowed: failed reattach closes the client to avoid leak.
func (m *Manager) RollbackPlanStart(previous *Manager) {
	if m == nil {
		return
	}
	m.mu.Lock()
	adopted := m.planAdopted
	m.planAdopted = nil
	m.mu.Unlock()
	for name, client := range adopted {
		if c := m.Detach(name); c != nil {
			client = c
		}
		if previous == nil {
			_ = client.Close()
			continue
		}
		if err := previous.Adopt(name, client); err != nil {
			// Previous still holds a Reloaded old client under the same name, or
			// is closed — close the orphaned client so the process does not leak.
			_ = client.Close()
		}
	}
	_ = m.Close()
}

// DrainPlan closes clients matching the plan's Removed and Reloaded sets.
func (m *Manager) DrainPlan(plan *extension.RuntimePlan) {
	if m == nil || plan == nil {
		return
	}
	var ids []string
	for _, id := range plan.Removed {
		if name := PluginNameFromComponentID(id); name != "" {
			ids = append(ids, name)
		}
	}
	for _, id := range plan.Reloaded {
		if name := PluginNameFromComponentID(id); name != "" {
			ids = append(ids, name)
		}
	}
	m.Drain(ids...)
}
