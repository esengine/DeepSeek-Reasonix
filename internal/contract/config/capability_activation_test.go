package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/mcplaunch"
)

func TestActivationStoreDefaultsAndOverrides(t *testing.T) {
	home := testenv.TempDir(t)
	store := NewActivationStore(home)

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

	if err := store.SetServerEnabled(entry, "", ActivationGlobal, true); err != nil {
		t.Fatalf("SetServerEnabled: %v", err)
	}
	enabled, err = store.IsEnabled(entry, "")
	if err != nil || !enabled {
		t.Fatalf("override should re-enable despite auto_start=false: enabled=%v err=%v", enabled, err)
	}

	path := ActivationPath(home)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat activation file: %v", err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Fatalf("activation file mode = %o, want 0600", perm)
	}
	if filepath.Base(path) != "capability-activation.json" {
		t.Fatalf("unexpected activation path base %q", path)
	}
}

// The point of the project layer: a globally installed server can be switched
// off in one project without the other projects noticing.
func TestProjectOverrideDoesNotLeakToAnotherProject(t *testing.T) {
	home := testenv.TempDir(t)
	store := NewActivationStore(home)
	entry := PluginEntry{Name: "playwright", Source: MCPSourceUserConfig}
	here, there := testenv.TempDir(t), testenv.TempDir(t)

	if err := store.SetServerEnabled(entry, here, ActivationProject, false); err != nil {
		t.Fatalf("SetServerEnabled: %v", err)
	}

	if on, err := store.IsEnabled(entry, here); err != nil || on {
		t.Fatalf("server in the overriding project: enabled=%v err=%v, want disabled", on, err)
	}
	if on, err := store.IsEnabled(entry, there); err != nil || !on {
		t.Fatalf("server in another project: enabled=%v err=%v, want still enabled", on, err)
	}
	if on, err := store.IsEnabled(entry, ""); err != nil || !on {
		t.Fatalf("server globally: enabled=%v err=%v, want still enabled", on, err)
	}
}

// Nine worktrees are one project. A switch flipped in the main tree has to
// apply in a tree cut from it, or per-project settings would vanish on branch.
func TestProjectOverrideAppliesAcrossLinkedWorktrees(t *testing.T) {
	home := testenv.TempDir(t)
	store := NewActivationStore(home)
	entry := PluginEntry{Name: "playwright", Source: MCPSourceUserConfig}

	base := testenv.TempDir(t)
	repo := filepath.Join(base, "repo")
	gitDir := filepath.Join(repo, ".git")
	mustMkdir(t, filepath.Join(gitDir, "worktrees", "studio"))
	tree := filepath.Join(base, "studio")
	mustMkdir(t, tree)
	mustWrite(t, filepath.Join(tree, ".git"),
		"gitdir: "+filepath.Join(gitDir, "worktrees", "studio")+"\n")

	if err := store.SetServerEnabled(entry, repo, ActivationProject, false); err != nil {
		t.Fatalf("SetServerEnabled: %v", err)
	}
	if on, err := store.IsEnabled(entry, tree); err != nil || on {
		t.Fatalf("worktree resolved enabled=%v err=%v, want the main tree's disable", on, err)
	}
}

// A repository-declared server may not take a global row: the same name in
// another repository is a different server entirely.
func TestRepositoryDeclaredServerStaysProjectScoped(t *testing.T) {
	for _, source := range []MCPConfigSource{MCPSourceProjectConfig, MCPSourceProjectMCPJSON} {
		entry := PluginEntry{Name: "project-mcp", Source: source}
		if !source.UserAuthorized() || !source.ProjectScoped() {
			t.Fatalf("project source %q policy = authorized:%v scoped:%v, want trusted and project scoped",
				source, source.UserAuthorized(), source.ProjectScoped())
		}
		a := ServerOverrideFor(entry, testenv.TempDir(t), ActivationGlobal)
		b := ServerOverrideFor(entry, testenv.TempDir(t), ActivationGlobal)
		if a.Scope != ActivationProject || b.Scope != ActivationProject {
			t.Fatalf("scopes = %q and %q, want both project even when global was asked for", a.Scope, b.Scope)
		}
		if a.Key == "" || a.Key == b.Key {
			t.Fatalf("keys = %q and %q, want distinct non-empty project keys", a.Key, b.Key)
		}
	}
}

// Two plugin packages may expose the same short server name; their rows must
// not collide.
func TestPluginPackageServersKeyByOwner(t *testing.T) {
	a := ServerOverrideFor(PluginEntry{Name: "search", Source: MCPSourcePluginPackage}, "", ActivationGlobal)
	b := ServerOverrideFor(PluginEntry{Name: "search", Source: MCPSourceUserConfig}, "", ActivationGlobal)
	if overrideKey(a) == overrideKey(b) {
		t.Fatalf("package and user rows share key %q", overrideKey(a))
	}
}

func TestSkillOverrideIsScopedPerProject(t *testing.T) {
	home := testenv.TempDir(t)
	store := NewActivationStore(home)
	here, there := testenv.TempDir(t), testenv.TempDir(t)

	if err := store.SetSkillEnabled("deploy", here, ActivationProject, false); err != nil {
		t.Fatalf("SetSkillEnabled: %v", err)
	}
	if on, err := store.SkillEnabled("deploy", here, true); err != nil || on {
		t.Fatalf("skill here: enabled=%v err=%v, want disabled", on, err)
	}
	if on, err := store.SkillEnabled("deploy", there, true); err != nil || !on {
		t.Fatalf("skill in another project: enabled=%v err=%v, want unaffected", on, err)
	}

	if err := store.ClearSkill("deploy", here, ActivationProject); err != nil {
		t.Fatalf("ClearSkill: %v", err)
	}
	if on, err := store.SkillEnabled("deploy", here, true); err != nil || !on {
		t.Fatalf("after clear: enabled=%v err=%v, want back to declared", on, err)
	}
}

func TestProjectLayerBeatsGlobalLayer(t *testing.T) {
	home := testenv.TempDir(t)
	store := NewActivationStore(home)
	root := testenv.TempDir(t)

	if err := store.SetSkillEnabled("review", root, ActivationGlobal, false); err != nil {
		t.Fatalf("global: %v", err)
	}
	if err := store.SetSkillEnabled("review", root, ActivationProject, true); err != nil {
		t.Fatalf("project: %v", err)
	}
	if on, err := store.SkillEnabled("review", root, true); err != nil || !on {
		t.Fatalf("resolved enabled=%v err=%v, want the project layer to win", on, err)
	}

	scope, found, err := store.SkillOverrideScope("review", root)
	if err != nil || !found || scope != ActivationProject {
		t.Fatalf("SkillOverrideScope = (%q,%v,%v), want (project,true,nil)", scope, found, err)
	}
}

// A pre-rename install keeps its switch: the v1 file is read once and matched
// by re-deriving the path fingerprint, since the stored digest cannot be
// reversed into the path it came from.
func TestLegacyMCPActivationFileStillResolves(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)

	legacy := map[string]any{
		"version": 1,
		"overrides": []map[string]any{{
			"scope": "global", "source": string(MCPSourceUserConfig),
			"server": "context7", "enabled": false,
		}, {
			"scope": "workspace", "workspace": mcplaunch.WorkspaceFingerprint(root),
			"source": string(MCPSourceProjectMCPJSON), "server": "codegraph", "enabled": false,
		}},
	}
	body, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	mustWrite(t, filepath.Join(home, "mcp-activation.json"), string(body))

	store := NewActivationStore(home)
	if on, err := store.IsEnabled(PluginEntry{Name: "context7", Source: MCPSourceUserConfig}, root); err != nil || on {
		t.Fatalf("legacy global row: enabled=%v err=%v, want disabled", on, err)
	}
	if on, err := store.IsEnabled(PluginEntry{Name: "codegraph", Source: MCPSourceProjectMCPJSON}, root); err != nil || on {
		t.Fatalf("legacy workspace row: enabled=%v err=%v, want disabled", on, err)
	}
}

// Writing must not disturb the legacy file, so a downgrade keeps working.
func TestWriteLeavesTheLegacyFileAlone(t *testing.T) {
	home := testenv.TempDir(t)
	legacyPath := filepath.Join(home, "mcp-activation.json")
	mustWrite(t, legacyPath, `{"version":1,"overrides":[]}`)

	store := NewActivationStore(home)
	if err := store.SetSkillEnabled("deploy", testenv.TempDir(t), ActivationGlobal, false); err != nil {
		t.Fatalf("SetSkillEnabled: %v", err)
	}
	body, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("read legacy: %v", err)
	}
	if string(body) != `{"version":1,"overrides":[]}` {
		t.Fatalf("legacy file was rewritten: %s", body)
	}
}

func TestProjectOverridesReportsWhatThisProjectChanged(t *testing.T) {
	home := testenv.TempDir(t)
	store := NewActivationStore(home)
	here, there := testenv.TempDir(t), testenv.TempDir(t)

	if err := store.SetSkillEnabled("deploy", here, ActivationProject, false); err != nil {
		t.Fatalf("skill: %v", err)
	}
	if err := store.SetServerEnabled(PluginEntry{Name: "playwright", Source: MCPSourceUserConfig},
		here, ActivationProject, false); err != nil {
		t.Fatalf("server: %v", err)
	}
	if err := store.SetSkillEnabled("dataviz", there, ActivationProject, false); err != nil {
		t.Fatalf("other project: %v", err)
	}
	if err := store.SetSkillEnabled("review", here, ActivationGlobal, false); err != nil {
		t.Fatalf("global: %v", err)
	}

	rows, err := store.ProjectOverrides(here)
	if err != nil {
		t.Fatalf("ProjectOverrides: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ProjectOverrides = %d rows (%+v), want 2", len(rows), rows)
	}
}

// With no project open there is nothing for "this project" to mean, and a row
// filed under an empty identity would never resolve — so the decision has to
// land on the global layer instead of disappearing.
func TestProjectScopeFallsBackToGlobalWithoutAWorkspace(t *testing.T) {
	home := testenv.TempDir(t)
	store := NewActivationStore(home)

	if err := store.SetSkillEnabled("audit", "", ActivationProject, false); err != nil {
		t.Fatalf("SetSkillEnabled: %v", err)
	}
	if on, err := store.SkillEnabled("audit", "", true); err != nil || on {
		t.Fatalf("skill with no workspace: enabled=%v err=%v, want the switch to have stuck", on, err)
	}

	entry := PluginEntry{Name: "context7", Source: MCPSourceUserConfig}
	if err := store.SetServerEnabled(entry, "", ActivationProject, false); err != nil {
		t.Fatalf("SetServerEnabled: %v", err)
	}
	if on, err := store.IsEnabled(entry, ""); err != nil || on {
		t.Fatalf("server with no workspace: enabled=%v err=%v, want the switch to have stuck", on, err)
	}
}

func TestActivationStoreConcurrentIndependentWriters(t *testing.T) {
	home := testenv.TempDir(t)
	const writers = 24
	stores := make([]*ActivationStore, writers)
	for i := range stores {
		stores[i] = NewActivationStore(home)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, store := range stores {
		wg.Go(func() {
			<-start
			if err := store.SetOverride(ActivationOverride{
				Kind:    CapabilityMCP,
				Scope:   ActivationGlobal,
				Name:    fmt.Sprintf("server-%02d", i),
				Enabled: true,
			}); err != nil {
				t.Errorf("SetOverride(%d): %v", i, err)
			}
		})
	}
	close(start)
	wg.Wait()

	file, err := NewActivationStore(home).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(file.Overrides) != writers {
		t.Fatalf("activation overrides = %d, want %d (lost update)", len(file.Overrides), writers)
	}
}

func TestEnabledPluginsHonorsActivation(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	store := NewActivationStore(home)
	if err := store.SetServerEnabled(PluginEntry{Name: "a", Source: MCPSourceUserConfig},
		"", ActivationGlobal, false); err != nil {
		t.Fatalf("SetServerEnabled: %v", err)
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

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
