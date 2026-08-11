package boot

import (
	"reasonix/internal/browserhost"
	"reasonix/internal/extension"
	"reasonix/internal/extension/sidecar"
)

// bindBrowserHandlers installs generation-scoped browser bindings on every
// live sidecar client. Plugins that did not declare the browser capability
// still receive a handler; the client denies with capability_not_declared.
//
// For stage: new clients get the next generation binding (calls fail with
// stale_generation until publish). For commit/no-op: every client (including
// adopted) is swapped to the new binding before/with publish.
//
// Dispose of previous bindings is the caller's responsibility via the
// previous generation drain cancel registered inside each Binding.
func bindBrowserHandlers(mgr *sidecar.Manager, backend browserhost.Backend, owner *extension.RuntimeOwner, generation uint64) {
	if mgr == nil {
		return
	}
	for _, client := range mgr.Clients() {
		binding := browserhost.NewBinding(browserhost.BindingOptions{
			Backend:    backend,
			Owner:      owner,
			Generation: generation,
			PluginID:   client.PluginID(),
		})
		client.SetBrowserHandler(binding)
	}
}
