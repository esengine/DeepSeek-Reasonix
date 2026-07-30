package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestMCPActivationStoreDefaultsAndOverrides(t *testing.T) {
	home := t.TempDir()
	store := NewMCPActivationStore(home)

	entry := PluginEntry{Name: "chrome-devtools", Source: MCPSourceUserConfig}
	enabled, err := store.IsEnabled(entry, "")
	if err != nil || !enabled {
		t.Fatalf("default user install should be enabled: enabled=%v err=%v", enabled, err)
	}

	disabled := false
	entry.AutoStart = &disabled
	enabled, err = store.IsEnabled(entry, "")
	if err != nil || enabled {
		t.Fatalf("auto_start=false without override should disable: enabled=%v err=%v", enabled, err)
	}

	if err := store.SetEnabled(MCPActivationOverride{
		Scope:   MCPActivationGlobal,
		Source:  string(MCPSourceUserConfig),
		Server:  "chrome-devtools",
		Enabled: true,
	}); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	enabled, err = store.IsEnabled(entry, "")
	if err != nil || !enabled {
		t.Fatalf("override should re-enable despite auto_start=false: enabled=%v err=%v", enabled, err)
	}

	path := MCPActivationPath(home)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat activation file: %v", err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Fatalf("activation file mode = %o, want 0600", perm)
	}

	// Atomic rewrite should keep a valid JSON document.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read activation: %v", err)
	}
	if len(raw) == 0 || raw[0] != '{' {
		t.Fatalf("activation JSON = %q", raw)
	}
	if filepath.Base(path) != "mcp-activation.json" {
		t.Fatalf("unexpected activation path base %q", path)
	}
}

func TestProjectMCPIsTrustedButActivationRemainsWorkspaceScoped(t *testing.T) {
	for _, source := range []MCPConfigSource{MCPSourceProjectConfig, MCPSourceProjectMCPJSON} {
		entry := PluginEntry{Name: "project-mcp", Source: source}
		if !source.UserAuthorized() || !source.ProjectScoped() {
			t.Fatalf("project source %q policy = authorized:%v scoped:%v, want trusted and workspace scoped",
				source, source.UserAuthorized(), source.ProjectScoped())
		}
		scopeA, workspaceA, gotSource, _ := ActivationIdentity(entry, "/workspace/a")
		scopeB, workspaceB, _, _ := ActivationIdentity(entry, "/workspace/b")
		if scopeA != MCPActivationWorkspace || scopeB != MCPActivationWorkspace || workspaceA == "" || workspaceA == workspaceB {
			t.Fatalf("project activation identities = (%q,%q,%q) and (%q,%q), want distinct workspace scopes",
				scopeA, workspaceA, gotSource, scopeB, workspaceB)
		}
	}
}

func TestMCPActivationStoreConcurrentIndependentWriters(t *testing.T) {
	home := t.TempDir()
	const writers = 24
	stores := make([]*MCPActivationStore, writers)
	for i := range stores {
		stores[i] = NewMCPActivationStore(home)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, store := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := store.SetEnabled(MCPActivationOverride{
				Scope:   MCPActivationGlobal,
				Server:  fmt.Sprintf("server-%02d", i),
				Enabled: true,
			}); err != nil {
				t.Errorf("SetEnabled(%d): %v", i, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	file, err := NewMCPActivationStore(home).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(file.Overrides) != writers {
		t.Fatalf("activation overrides = %d, want %d (lost update)", len(file.Overrides), writers)
	}
}

func TestEnabledPluginsHonorsActivation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	store := NewMCPActivationStore(home)
	if err := store.SetEnabled(MCPActivationOverride{
		Scope:   MCPActivationGlobal,
		Source:  string(MCPSourceUserConfig),
		Server:  "a",
		Enabled: false,
	}); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	cfg := &Config{Plugins: []PluginEntry{
		{Name: "a", Source: MCPSourceUserConfig},
		{Name: "b", Source: MCPSourceUserConfig},
	}}
	got := cfg.EnabledPlugins("", store)
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("EnabledPlugins = %+v, want [b]", got)
	}
}
