package boot

import (
	"slices"
	"testing"

	"reasonix/internal/browser"
	"reasonix/internal/config"
)

// A dead loopback port: the backend is lazy, so a build that never calls a
// browser tool must never notice that nothing is listening there.
const configuredBrowserSection = `
[browser]
enabled = true
endpoint = "http://127.0.0.1:1"
`

// TestConfiguredBrowserBackendStaysOffTheProviderSurface is the cache guard
// for the configured backend: turning [browser] on registers the tools for
// use_capability and leaves the provider request's tool array and the system
// prompt byte-identical to a session with no browser at all.
func TestConfiguredBrowserBackendStaysOffTheProviderSurface(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()

	writeFile(t, dir, "reasonix.toml", browserBootConfig)
	approveWorkspace(t, dir)
	without := captureBrowserSurface(t, nil)
	writeFile(t, dir, "reasonix.toml", browserBootConfig+configuredBrowserSection)
	approveWorkspace(t, dir)
	with := captureBrowserSurface(t, nil)

	for _, name := range browser.Names() {
		if !slices.Contains(with.registry, name) {
			t.Errorf("registry with [browser] enabled lacks %s", name)
		}
		if slices.Contains(without.registry, name) {
			t.Errorf("registry without [browser] registered %s", name)
		}
		if slices.Contains(with.visible, name) || slices.Contains(with.tools, name) {
			t.Errorf("%s leaked into the provider-visible surface", name)
		}
	}
	if !slices.Equal(with.tools, without.tools) {
		t.Fatalf("provider tool array changed with [browser] enabled:\nwith    %v\nwithout %v", with.tools, without.tools)
	}
	if with.prompt != without.prompt {
		t.Fatalf("system prompt changed with [browser] enabled: %s", firstDivergence(without.prompt, with.prompt))
	}
}

func TestBrowserBackendPrefersTheHostBrowser(t *testing.T) {
	host := bootBrowserExecutor{}
	// A host that already brought a browser keeps it, and owes no shutdown:
	// the desktop shell and the SSH broker outlive one controller.
	roots := []string{t.TempDir()}
	exec, shutdown := browserBackend(host, config.BrowserConfig{Enabled: true}, roots)
	if exec == nil || shutdown != nil {
		t.Fatalf("host browser = %v with shutdown %v, want the host executor and no shutdown", exec, shutdown != nil)
	}
	if exec, shutdown := browserBackend(nil, config.BrowserConfig{}, roots); exec != nil || shutdown != nil {
		t.Fatalf("disabled backend = %v, want no browser", exec)
	}
	exec, shutdown = browserBackend(nil, config.BrowserConfig{Enabled: true}, roots)
	if exec == nil || shutdown == nil {
		t.Fatal("an enabled backend must return an executor and the shutdown that reaps it")
	}
	shutdown()
}
