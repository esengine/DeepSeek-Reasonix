package boot

import (
	"context"

	"reasonix/internal/extension"
	"reasonix/internal/lsp"
	"reasonix/internal/plugin"
	"reasonix/internal/sessiontemp"
)

// wireRuntimeScopeCleanup folds MCP host / LSP / session-temp inventory into
// the RuntimeSet when it already holds generation effects. Empty sets stay
// empty (stage-3a Len()==0). Returns the controller cleanup func.
func wireRuntimeScopeCleanup(runtimeSet *extension.RuntimeSet, cleanup func(), sharedHost *plugin.Host, pluginHost *plugin.Host, lspMgr *lsp.Manager, sessionTemp *sessiontemp.Manager) func() {
	if runtimeSet == nil || runtimeSet.Len() == 0 {
		return func() {
			if cleanup != nil {
				cleanup()
			}
			_ = runtimeSet.Close()
		}
	}
	if sharedHost == nil && pluginHost != nil {
		host := pluginHost
		_ = extension.TrackMCPClient(runtimeSet.Scope(), "plugin-host", extension.FuncCloser(host.Close))
	}
	if lspMgr != nil {
		mgr := lspMgr
		_ = extension.TrackWatcher(runtimeSet.Scope(), "lsp-manager", func() error {
			mgr.Close()
			return nil
		})
	}
	if sessionTemp != nil {
		_ = extension.TrackBackgroundJob(runtimeSet.Scope(), "session-temp", nil, func(context.Context) error {
			return nil
		})
	}
	_ = extension.TrackControllerCleanup(runtimeSet.Scope(), "session-resources", func() error {
		return nil
	})
	return func() { _ = runtimeSet.Close() }
}
