package main

import (
	"reasonix/internal/capdiag"
	"reasonix/internal/plugin"
)

// CapabilityDiagnostics returns a read-only capability report for the active
// workspace. When includeSessionRuntime is true, connected/failed/deferred
// status is merged from the active tab Host only — no new MCP processes are
// started, and no controller rebuild or session snapshot is triggered.
func (a *App) CapabilityDiagnostics(includeSessionRuntime bool) capdiag.Report {
	root, host := a.activeDiagSnapshot(includeSessionRuntime)
	opts := capdiag.Options{Root: root, Live: false}

	if !includeSessionRuntime {
		return capdiag.Collect(opts)
	}

	opts.RuntimeHost = host
	if host == nil {
		return capdiag.CollectWithRuntimeUnavailable(opts)
	}
	return capdiag.Collect(opts)
}

// activeDiagSnapshot reads workspace root and optional Host under one lock so
// a tab switch cannot pair project A's config with project B's runtime Host.
func (a *App) activeDiagSnapshot(includeHost bool) (root string, host *plugin.Host) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	tab := a.activeTabLocked()
	if tab == nil {
		return "", nil
	}
	root = tab.WorkspaceRoot
	if includeHost && tab.Ctrl != nil {
		host = tab.Ctrl.Host()
	}
	return root, host
}
