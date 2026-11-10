package boot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/state/agentpreset"
)

func TestLiveCompositionDoesNotCaptureUnusedSkillAssets(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	writeFile(t, home, "skills/templates/SKILL.md", "---\nname: templates\ndescription: template library\n---\nRead templates when needed.")
	// An unused link cannot participate in assembling a capability grant.
	if err := os.Symlink(testenv.TempDir(t), filepath.Join(home, "skills/templates/unused")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	d := agentcomposition.Builtins()[0]
	d.ID = "user:live"
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
	res, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, AgentComposition: d.ID, resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Controller.Close()
	b := res.Controller.CompositionBinding()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var format map[string]any
	if err := json.Unmarshal(data, &format); err != nil {
		t.Fatal(err)
	}
	if format["formatVersion"] != float64(2) || format["resourceMode"] != "live" || len(b.Resources) != 0 {
		t.Fatalf("binding retained resources or lacks live format: %s", data)
	}
	if b.Resolved.Skills.Mode != agentcomposition.Inherit {
		t.Fatal("dynamic inheritance was converted into a fixed list")
	}
	writeFile(t, home, "skills/added/SKILL.md", "---\nname: added\ndescription: added later\n---\nNEW")
	found := false
	for _, sk := range res.Controller.Skills() {
		found = found || sk.Name == "added"
	}
	if !found {
		t.Fatal("inherited skill added after selection is missing")
	}
}

func TestLiveCompositionExplicitAutoStartDoesNotBlockSelection(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	defer close(release)
	home, root := compositionTestWorkspace(t, "openai")
	writeFile(t, home, "config.toml", "[[plugins]]\nname='background'\ntype='http'\nurl='"+server.URL+"'\nauto_start=true\n")
	t.Setenv("REASONIX_HOME", home)
	cfg, err := config.RootsForHome(home).LoadForRootReadOnly(root)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].AutoStart == nil || !*cfg.Plugins[0].AutoStart || cfg.Plugins[0].Source != config.MCPSourceUserConfig {
		t.Fatalf("automatic connection fixture was not loaded: %v", err)
	}
	d := agentcomposition.Builtins()[0]
	d.ID = "user:background"
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
	res, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, AgentComposition: d.ID, resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Controller.Close()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatalf("explicit automatic connection did not start: failures=%+v source=%s", res.Controller.Host().Failures(), cfg.Plugins[0].Source)
	}
}

func TestLiveCompositionLeavesUncachedMCPIdle(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	home, root := compositionTestWorkspace(t, "openai")
	d := agentcomposition.Builtins()[0]
	d.ID = "user:offline"
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
	res, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, AgentComposition: d.ID, resolvedShell: pinnedEffectShell(),
		ExtraPlugins: []plugin.Spec{{Name: "offline", Type: "http", URL: server.URL, Authorized: true}}})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Controller.Close()
	proxy, ok := res.Assembly.Registry.Get("use_capability")
	if !ok {
		t.Fatal("stable capability entry is missing")
	}
	for _, args := range []string{`{"action":"list"}`, `{"action":"search","query":"offline"}`, `{"action":"inspect","capability_id":"mcp-server:offline"}`} {
		if _, err := proxy.Execute(t.Context(), json.RawMessage(args)); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("selection/catalog caused %d MCP requests", calls.Load())
	}
	if len(res.Controller.CompositionBinding().Resources) != 0 {
		t.Fatal("MCP resources captured")
	}
}

func TestLiveCompositionSkillWhitelistAndRevocation(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	writeFile(t, home, "skills/allowed.md", "---\nname: allowed\ndescription: allowed\n---\nOLD")
	writeFile(t, home, "skills/denied.md", "---\nname: denied\ndescription: denied\n---\nDENIED")
	d := agentcomposition.Builtins()[0]
	d.ID = "user:limited"
	d.Skills = agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{{ID: "allowed"}}}
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
	res, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, AgentComposition: d.ID, resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Controller.Close()
	skills := res.Controller.Skills()
	if len(skills) != 1 || skills[0].Name != "allowed" {
		t.Fatalf("whitelist not applied: %+v", skills)
	}
	writeFile(t, home, "skills/allowed.md", "---\nname: allowed\ndescription: allowed\n---\nNEW")
	if got := res.Controller.Skills(); len(got) != 1 || got[0].Body != "NEW" {
		t.Fatalf("live update missing: %+v", got)
	}
	if err := config.DefaultActivationStore().SetSkillEnabled("allowed", root, config.ActivationProject, false); err != nil {
		t.Fatal(err)
	}
	if len(res.Controller.Skills()) != 0 {
		t.Fatal("revoked skill remains visible")
	}
	if err := res.Controller.CompositionExecutionError(); err != nil {
		t.Fatalf("revoked optional skill blocked unrelated conversation: %v", err)
	}
}
