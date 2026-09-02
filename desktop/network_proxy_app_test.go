package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
)

func TestNetworkProxySpecForRootMatchesEffectiveProjectConfig(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte("[network]\nproxy_mode = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PROJECT_PROXY_URL=http://127.0.0.1:9876\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[network]\nproxy_mode = \"custom\"\nproxy_url = \"${PROJECT_PROXY_URL}\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec := NewApp().networkProxySpecForRoot(root)
	if spec.Mode != netclient.ModeCustom || spec.URL != "http://127.0.0.1:9876" {
		t.Fatalf("model probe proxy = %+v, want effective project proxy", spec)
	}
}

// TestSaveProviderPersistsNoProxy pins the #9560 escape hatch: the custom
// provider editor's "connect directly" toggle must round-trip through the
// config entry so the provider host lands in the chat transport's direct list.
func TestSaveProviderPersistsNoProxy(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:      "custom-gateway",
		Kind:      "openai",
		BaseURL:   "https://gw.example.internal/v1",
		Models:    []string{"gw-model"},
		APIKeyEnv: "GW_API_KEY",
		NoProxy:   true,
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("custom-gateway")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if !got.NoProxy {
		t.Fatal("saved provider no_proxy = false, want true")
	}
	view := providerViewFromEntry(*got, false, true)
	if !view.NoProxy {
		t.Fatal("provider view noProxy = false, want true")
	}
	if spec := cfg.NetworkProxySpec(); !slices.Contains(spec.DirectHosts, "gw.example.internal") {
		t.Fatalf("NetworkProxySpec.DirectHosts = %v, want the no_proxy provider host", spec.DirectHosts)
	}
}

// TestWithProbeDirectHostMirrorsProviderNoProxy covers the unsaved-editor probe
// path: refreshing models for a no_proxy provider must bypass the proxy even
// before the entry is persisted, and a custom proxy mode must still win.
func TestWithProbeDirectHostMirrorsProviderNoProxy(t *testing.T) {
	auto := netclient.ProxySpec{Mode: netclient.ModeAuto, DirectHosts: []string{"preset.example.cn"}}
	got := withProbeDirectHost(auto, "https://gw.example.internal/v1", true)
	if !slices.Contains(got.DirectHosts, "gw.example.internal") {
		t.Fatalf("probe DirectHosts = %v, want the edited provider host", got.DirectHosts)
	}
	if !slices.Contains(got.DirectHosts, "preset.example.cn") {
		t.Fatalf("probe DirectHosts = %v, lost existing entries", got.DirectHosts)
	}

	custom := netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "http://corp-proxy.internal:3128"}
	if got := withProbeDirectHost(custom, "https://gw.example.internal/v1", true); len(got.DirectHosts) != 0 {
		t.Fatalf("custom proxy must override provider no_proxy, got DirectHosts %v", got.DirectHosts)
	}

	direct := withProbeDirectHost(auto, "https://gw.example.internal/v1", false)
	if slices.Contains(direct.DirectHosts, "gw.example.internal") {
		t.Fatalf("provider without no_proxy must stay proxied, got DirectHosts %v", direct.DirectHosts)
	}

	ipv6 := withProbeDirectHost(auto, "https://[2001:db8::1]:8443/v1", true)
	if !slices.Contains(ipv6.DirectHosts, "2001:db8::1") {
		t.Fatalf("IPv6 provider host = %v, want brackets and port removed", ipv6.DirectHosts)
	}
}
