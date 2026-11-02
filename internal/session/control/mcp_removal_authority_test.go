package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/runtime/usecap"
)

func TestRemoveMCPServerDoesNotDeleteFallbackWhenOwnerCorrupt(t *testing.T) {
	for _, source := range []struct{ file, valid, broken string }{
		{"reasonix.toml", "[[plugins]]\nname = \"mock\"\ncommand = \"project-mcp\"\n", "broken = ["},
		{".mcp.json", `{"mcpServers":{"mock":{"command":"project-mcp"}}}`, `{"mcpServers":`},
	} {
		t.Run(source.file, func(t *testing.T) {
			isolateControlConfigHome(t)
			workspace := t.TempDir()
			path := filepath.Join(workspace, source.file)
			if err := os.WriteFile(path, []byte(source.valid), 0o600); err != nil {
				t.Fatal(err)
			}
			fallback := []byte("[[plugins]]\nname = \"mock\"\ncommand = \"user-mcp\"\n")
			userPath := config.UserConfigPath()
			if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(userPath, fallback, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadForRootReadOnly(workspace)
			if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Command != "project-mcp" {
				t.Fatalf("load effective owner: %v, %v", cfg, err)
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
				t.Fatalf("corrupt owner removal = (%v, %v)", disconnected, err)
			}
			if data, err := os.ReadFile(userPath); err != nil || string(data) != string(fallback) {
				t.Fatalf("corrupt owner changed fallback: %q, %v", data, err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != source.broken {
				t.Fatalf("corrupt owner was rewritten: %q, %v", data, err)
			}
			if _, found := reg.Get("mcp__mock__connect"); !found {
				t.Fatal("corrupt owner removed retained tools")
			}
			if entry, found := runtime.ServerIdentity("mock"); !found || entry.Source != cfg.Plugins[0].Source {
				t.Fatalf("corrupt owner changed runtime identity: %+v, %v", entry, found)
			}
		})
	}
}

func TestRemoveMCPServerRequiresClaudeDeclarationToBeAbsent(t *testing.T) {
	for _, scope := range []string{"user", "local"} {
		for _, transition := range []string{"present", "corrupt", "unreadable", "deleted"} {
			t.Run(scope+"/"+transition, func(t *testing.T) {
				isolateControlConfigHome(t)
				t.Setenv("REASONIX_HOME", "")
				workspace := t.TempDir()
				claudeDir := t.TempDir()
				t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
				path := filepath.Join(claudeDir, ".claude.json")
				body := `{"mcpServers":{"mock":{"command":"claude-mcp"}}}`
				if scope == "local" {
					body = fmt.Sprintf(`{"projects":{%q:{"mcpServers":{"mock":{"command":"claude-mcp"}}}}}`, filepath.ToSlash(workspace))
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				cfg, err := config.LoadForRootReadOnly(workspace)
				if err != nil || len(cfg.Plugins) != 1 {
					t.Fatalf("load Claude declaration: %v, %v", cfg, err)
				}
				host := plugin.NewHost()
				reg := tool.NewRegistry()
				runtime := usecap.NewMCPCapabilityRuntime(t.Context(), host, nil, reg, nil)
				c := New(Options{WorkspaceRoot: workspace, Host: host, Registry: reg, CapabilityRuntime: runtime})
				t.Cleanup(c.Close)
				if _, err := c.RegisterMCPServerOnDemand(cfg.Plugins[0]); err != nil {
					t.Fatal(err)
				}
				switch transition {
				case "corrupt":
					if err := os.WriteFile(path, []byte(`{"projects":`), 0o600); err != nil {
						t.Fatal(err)
					}
				case "unreadable":
					if err := os.Chmod(path, 0); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
					if _, err := os.ReadFile(path); err == nil {
						t.Skip("file permissions do not deny reads on this platform or for this user")
					}
				case "deleted":
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				disconnected, err := c.RemoveMCPServer("mock")
				if transition == "deleted" {
					if err != nil {
						t.Fatalf("deleted Claude declaration removal: %v", err)
					}
					if _, found := runtime.ServerIdentity("mock"); found {
						t.Fatal("deleted Claude declaration remains in runtime")
					}
					return
				}
				if err == nil || disconnected {
					t.Fatalf("existing Claude source removal = (%v, %v)", disconnected, err)
				}
				if _, found := reg.Get("mcp__mock__connect"); !found {
					t.Fatal("existing Claude source lost retained tools")
				}
				if _, found := runtime.ServerIdentity("mock"); !found {
					t.Fatal("existing Claude source lost runtime identity")
				}
			})
		}
	}
}

func TestRemoveMCPServerNamesMalformedClaudeSourceForProjectDeclaration(t *testing.T) {
	isolateControlConfigHome(t)
	t.Setenv("REASONIX_HOME", "")
	workspace := t.TempDir()
	projectPath := filepath.Join(workspace, "reasonix.toml")
	projectBody := []byte("[[plugins]]\nname = \"mock\"\ncommand = \"project-mcp\"\n")
	if err := os.WriteFile(projectPath, projectBody, 0o600); err != nil {
		t.Fatal(err)
	}
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	claudePath := filepath.Join(claudeDir, ".claude.json")
	if err := os.WriteFile(claudePath, []byte(`{"projects":`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRootReadOnly(workspace)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Source != config.MCPSourceProjectConfig {
		t.Fatalf("load project declaration: %v, %v", cfg, err)
	}
	host := plugin.NewHost()
	reg := tool.NewRegistry()
	runtime := usecap.NewMCPCapabilityRuntime(t.Context(), host, nil, reg, nil)
	c := New(Options{WorkspaceRoot: workspace, Host: host, Registry: reg, CapabilityRuntime: runtime})
	t.Cleanup(c.Close)
	if _, err := c.RegisterMCPServerOnDemand(cfg.Plugins[0]); err != nil {
		t.Fatal(err)
	}
	disconnected, err := c.RemoveMCPServer("mock")
	if err == nil || disconnected {
		t.Fatalf("malformed Claude source removal = (%v, %v)", disconnected, err)
	}
	if !strings.Contains(err.Error(), claudePath) {
		t.Errorf("removal error = %q, want it to name %s", err, claudePath)
	}
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Errorf("removal error = %v, want wrapped JSON syntax error", err)
	}
	if data, err := os.ReadFile(projectPath); err != nil || string(data) != string(projectBody) {
		t.Fatalf("malformed Claude source changed project declaration: %q, %v", data, err)
	}
	if _, found := reg.Get("mcp__mock__connect"); !found {
		t.Fatal("malformed Claude source removed retained tools")
	}
	if entry, found := runtime.ServerIdentity("mock"); !found || entry.Source != config.MCPSourceProjectConfig {
		t.Fatalf("malformed Claude source changed runtime identity: %+v, %v", entry, found)
	}
	if err := os.WriteFile(claudePath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RemoveMCPServer("mock"); err != nil {
		t.Fatalf("removal after repairing Claude source: %v", err)
	}
	if names := c.ConfiguredMCPNames(); len(names) != 0 {
		t.Fatalf("removed declaration survived config reload: %v", names)
	}
	if _, found := runtime.ServerIdentity("mock"); found {
		t.Fatal("removed declaration survived in runtime")
	}
}

func TestRemoveMCPServerRetainsPluginOwnershipAfterFailedManifestLoss(t *testing.T) {
	home := isolateControlConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := t.TempDir()
	root := pluginpkg.InstallRoot(reasonixHome, "managed")
	manifest := filepath.Join(root, pluginpkg.NativeManifest)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"apiVersion":"reasonix.io/plugin/v2","name":"managed","version":"1.0.0","mcpServers":{"helper":{"command":"missing-managed-mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{Name: "managed", Root: pluginpkg.RelativeRoot(reasonixHome, root), Enabled: true, ManifestKind: "reasonix"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRootReadOnly(workspace)
	if err != nil || len(cfg.Plugins) != 1 {
		t.Fatalf("load managed declaration: %v, %v", cfg, err)
	}
	host := plugin.NewHost()
	reg := tool.NewRegistry()
	runtime := usecap.NewMCPCapabilityRuntime(t.Context(), host, nil, reg, nil)
	c := New(Options{WorkspaceRoot: workspace, Host: host, Registry: reg, CapabilityRuntime: runtime, MCPConfigureSpec: func(spec *plugin.Spec) {
		spec.Package, _ = cfg.PluginPackageOwner(spec.Name)
	}})
	t.Cleanup(c.Close)
	runtime.ConfigureInventorySource(func() ([]config.PluginEntry, []plugin.Spec, map[string]bool, error) {
		current, err := config.LoadForRootReadOnly(workspace)
		if err != nil {
			return nil, nil, nil, err
		}
		var specs []plugin.Spec
		for _, entry := range current.Plugins {
			specs = append(specs, c.mcpSpec(entry))
		}
		return current.Plugins, specs, nil, nil
	})
	if _, err := c.RegisterMCPServerOnDemand(cfg.Plugins[0]); err != nil {
		t.Fatal(err)
	}
	connect, found := reg.Get("mcp__helper__connect")
	if !found {
		t.Fatal("managed placeholder missing")
	}
	for range 2 {
		if _, err := connect.Execute(t.Context(), json.RawMessage(`{}`)); err == nil {
			t.Fatal("missing managed executable connected")
		}
	}
	if failures := host.Failures(); len(failures) != 1 {
		t.Fatalf("managed startup failure missing: %+v", failures)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	entries, _, _, _, _ := runtime.CapabilityCatalogState()
	if len(entries) != 0 {
		t.Fatalf("manifest loss did not refresh inventory: %+v", entries)
	}
	if _, found := runtime.ServerIdentity("helper"); found {
		t.Fatal("manifest loss kept runtime identity")
	}
	if disconnected, err := c.RemoveMCPServer("helper"); err == nil || disconnected {
		t.Fatalf("managed stale removal = (%v, %v)", disconnected, err)
	}
	if _, found := reg.Get("mcp__helper__connect"); !found {
		t.Fatal("managed stale tools were removed independently")
	}
	if failures := host.Failures(); len(failures) != 1 {
		t.Fatalf("managed failure was cleared independently: %+v", failures)
	}
}
