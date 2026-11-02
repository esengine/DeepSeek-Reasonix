package control

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/runtime/usecap"
)

func TestRemoveMCPServerAfterSourceDirectoryDeleted(t *testing.T) {
	for _, mode := range []string{"on-demand", "failed", "without-runtime", "failed-without-runtime", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			isolateControlConfigHome(t)
			workspace := t.TempDir()
			sourceDir := filepath.Join(workspace, ".reasonix")
			if err := os.MkdirAll(sourceDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sourceDir, "reasonix.toml"), []byte("[[plugins]]\nname = \"fssc-oracle\"\ncommand = \"missing-mcp-executable\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadForRoot(workspace)
			if err != nil || len(cfg.Plugins) != 1 {
				t.Fatalf("load declaration: %v, %v", cfg, err)
			}
			entry := cfg.Plugins[0]
			if err := config.DefaultActivationStore().SetServerEnabled(entry, workspace, config.ActivationProject, true); err != nil {
				t.Fatal(err)
			}
			host := plugin.NewHost()
			reg := tool.NewRegistry()
			var runtime *usecap.MCPCapabilityRuntime
			if mode != "without-runtime" && mode != "failed-without-runtime" {
				runtime = usecap.NewMCPCapabilityRuntime(t.Context(), host, nil, reg, nil)
			}
			c := New(Options{WorkspaceRoot: workspace, Host: host, Registry: reg, CapabilityRuntime: runtime})
			t.Cleanup(c.Close)
			if _, err := c.RegisterMCPServerOnDemand(entry); err != nil {
				t.Fatal(err)
			}
			if mode == "failed" || mode == "failed-without-runtime" {
				if _, err := c.ReconnectMCPServer(entry.Name); err == nil {
					t.Fatal("missing executable connected")
				}
				if failures := host.Failures(); len(failures) != 1 || failures[0].Name != entry.Name {
					t.Fatalf("failed startup was not retained: %+v", failures)
				}
			}
			if mode == "disabled" {
				c.DisconnectMCPServer(entry.Name)
			}
			if err := os.RemoveAll(sourceDir); err != nil {
				t.Fatal(err)
			}
			if names := c.ConfiguredMCPNames(); len(names) != 0 {
				t.Fatalf("deleted declaration still configured: %v", names)
			}
			if disconnected, err := c.RemoveMCPServer(entry.Name); err != nil {
				t.Fatalf("remove stale %s server = (%v, %v)", mode, disconnected, err)
			}
			if bindings := reg.MCPBindings(); len(bindings) != 0 {
				t.Fatalf("stale tools survived removal: %+v", bindings)
			}
			if health := c.MCPServerHealth(); len(health) != 0 {
				t.Fatalf("stale status row survived removal: %+v", health)
			}
			if runtime != nil {
				entries, _, _, _ := runtime.CatalogState()
				if len(entries) != 0 {
					t.Fatalf("stale capability survived removal: %+v", entries)
				}
				if _, found, err := config.DefaultActivationStore().ServerOverride(entry, workspace, config.ActivationProject); err != nil || found {
					t.Fatalf("stale activation survived removal: found=%v err=%v", found, err)
				}
			}
			if disconnected, err := c.RemoveMCPServer(entry.Name); err == nil || disconnected {
				t.Fatalf("unknown name should not succeed: (%v, %v)", disconnected, err)
			}
		})
	}
}

func TestRemoveMCPServerPreservesStaleStateWhenConfigReadFails(t *testing.T) {
	for _, source := range []struct{ file, valid, broken string }{
		{"reasonix.toml", "[[plugins]]\nname = \"mock\"\ncommand = \"mock-mcp\"\n", "broken = ["},
		{".mcp.json", `{"mcpServers":{"mock":{"command":"mock-mcp"}}}`, `{"mcpServers":`},
	} {
		t.Run(source.file, func(t *testing.T) {
			isolateControlConfigHome(t)
			workspace := t.TempDir()
			path := filepath.Join(workspace, source.file)
			if err := os.WriteFile(path, []byte(source.valid), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadForRoot(workspace)
			if err != nil {
				t.Fatal(err)
			}
			host := plugin.NewHost()
			reg := tool.NewRegistry()
			runtime := usecap.NewMCPCapabilityRuntime(t.Context(), host, nil, reg, nil)
			c := New(Options{WorkspaceRoot: workspace, Host: host, Registry: reg, CapabilityRuntime: runtime})
			t.Cleanup(c.Close)
			if _, err := c.RegisterMCPServerOnDemand(cfg.Plugins[0]); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(source.broken), 0o600); err != nil {
				t.Fatal(err)
			}
			if disconnected, err := c.RemoveMCPServer("mock"); err == nil || disconnected {
				t.Fatalf("unreadable config removal = (%v, %v)", disconnected, err)
			}
			if _, found := reg.Get("mcp__mock__connect"); !found {
				t.Fatal("config failure removed on-demand tools")
			}
			entries, _, _, _ := runtime.CatalogState()
			if len(entries) != 1 || entries[0].Name != "mock" {
				t.Fatalf("config failure removed runtime inventory: %+v", entries)
			}
		})
	}
}

func TestRemoveMCPServerKeepsLowerPriorityDeclaration(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte("[[plugins]]\nname = \"shared\"\ncommand = \"project-mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte("[[plugins]]\nname = \"shared\"\ncommand = \"user-mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := plugin.NewHost()
	reg := tool.NewRegistry()
	runtime := usecap.NewMCPCapabilityRuntime(context.Background(), host, nil, reg, nil)
	c := New(Options{WorkspaceRoot: workspace, Host: host, Registry: reg, CapabilityRuntime: runtime})
	t.Cleanup(c.Close)
	if _, err := c.RemoveMCPServer("shared"); err != nil {
		t.Fatal(err)
	}
	configured := c.ConfiguredMCPServers()
	if len(configured) != 1 || configured[0].Entry.Command != "user-mcp" || configured[0].Entry.Source != config.MCPSourceUserConfig {
		t.Fatalf("fallback declaration changed: %+v", configured)
	}
	if _, found := reg.Get("mcp__shared__connect"); !found {
		t.Fatal("fallback on-demand tools were not restored")
	}
	entries, _, _, _ := runtime.CatalogState()
	if len(entries) != 1 || entries[0].Source != config.MCPSourceUserConfig {
		t.Fatalf("fallback runtime = %+v", entries)
	}
}

func TestRemoveStaleMCPServerClearsOAuthAndRememberedConsent(t *testing.T) {
	isolateControlConfigHome(t)
	server := fakeOAuthMCP(t)
	c, requests := signInController(t, server.URL)
	var opened atomic.Int32
	done := signIn(c, browserThatApproves(&opened))
	c.Approve(nextApproval(t, requests).ID, true, false, true)
	if got := settle(t, done); got.err != nil {
		t.Fatal(got.err)
	}
	entry, err := c.configuredMCPServer("figma")
	if err != nil {
		t.Fatal(err)
	}
	stateDir := c.mcpSpec(entry).StateDir
	oauthPath := filepath.Join(stateDir, "oauth.json")
	if _, err := os.Stat(oauthPath); err != nil {
		t.Fatalf("sign-in did not persist OAuth state: %v", err)
	}
	if _, err := c.ReconnectMCPServer("figma"); err == nil {
		t.Fatal("fixture endpoint must reject the connection")
	}
	if err := os.Remove(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RemoveMCPServer("figma"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oauthPath); !os.IsNotExist(err) {
		t.Fatalf("stale OAuth state survived removal: %v", err)
	}
	if plugin.SignInHostRemembered(stateDir, plugin.AuthorizationHost(server.URL)) {
		t.Fatal("stale sign-in consent survived removal")
	}
	if health := c.MCPServerHealth(); len(health) != 0 {
		t.Fatalf("failed OAuth row survived removal: %+v", health)
	}
}

func TestRemoveMCPServerPreservesRuntimeWhenPersistenceFails(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := t.TempDir()
	sourceDir := filepath.Join(workspace, ".reasonix")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sourceDir, "reasonix.toml")
	if err := os.WriteFile(path, []byte("[[plugins]]\nname = \"mock\"\ncommand = \"mock-mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	host := plugin.NewHost()
	reg := tool.NewRegistry()
	runtime := usecap.NewMCPCapabilityRuntime(t.Context(), host, nil, reg, nil)
	c := New(Options{WorkspaceRoot: workspace, Host: host, Registry: reg, CapabilityRuntime: runtime})
	t.Cleanup(c.Close)
	if _, err := c.RegisterMCPServerOnDemand(cfg.Plugins[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sourceDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sourceDir, 0o755) })
	if probe, err := os.CreateTemp(sourceDir, "write-probe"); err == nil {
		_ = probe.Close()
		t.Skip("directory permissions do not deny writes on this platform or for this user")
	}
	if disconnected, err := c.RemoveMCPServer("mock"); err == nil || disconnected {
		t.Fatalf("failed persistence removal = (%v, %v)", disconnected, err)
	}
	if _, found := reg.Get("mcp__mock__connect"); !found {
		t.Fatal("persistence failure removed on-demand tools")
	}
	entries, _, _, _ := runtime.CatalogState()
	if len(entries) != 1 || entries[0].Name != "mock" {
		t.Fatalf("persistence failure removed runtime inventory: %+v", entries)
	}
	if configured := c.ConfiguredMCPServers(); len(configured) != 1 {
		t.Fatalf("persistence failure removed declaration: %+v", configured)
	}
}
