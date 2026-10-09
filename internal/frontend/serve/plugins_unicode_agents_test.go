package serve

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

var unicodeAgentProviderSeq atomic.Uint64

func TestInstalledUnicodeAgentsMatchDisplayedInvocations(t *testing.T) {
	for _, format := range []struct{ kind, manifest, body string }{
		{"native", pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"unicode-agents","contributes":{"agents":["agents"]}}`},
		{"Claude", pluginpkg.ClaudeManifest, `{"name":"unicode-agents"}`},
	} {
		t.Run(format.kind, func(t *testing.T) {
			home, root, source := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Setenv("REASONIX_STATE_HOME", home)
			t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
			t.Chdir(root)
			kind := fmt.Sprintf("unicode-plugin-agents-%d", unicodeAgentProviderSeq.Add(1))
			rec := testutil.NewMock(kind)
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writePluginFile(t, filepath.Join(root, "reasonix.toml"), `
default_model = "fixture"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "fixture"
kind = "`+kind+`"
model = "x"
`)
			writePluginFile(t, filepath.Join(source, format.manifest), format.body)
			for _, stem := range []string{"审查", "点検", "cafe\u0301"} {
				writePluginFile(t, filepath.Join(source, "agents", stem+".md"), "---\ndescription: Unicode agent fixture\n---\nAGENT BODY")
			}
			if _, err := (config.Roots{}).ApproveWorkspacePrograms(root); err != nil {
				t.Fatal(err)
			}
			bc := NewBroadcaster()
			initial, err := boot.BuildRuntime(t.Context(), boot.Options{Sink: bc, WorkspaceRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			s := New(initial.Controller, bc, config.ServeConfig{})
			s.AdoptRuntime(initial)
			leases := control.NewSessionLeaseKeeper()
			s.SetSessionLeases(leases)
			t.Cleanup(leases.Release)
			t.Cleanup(func() { s.ctl().Close() })
			srv := httptest.NewServer(operatorHandler(s))
			defer srv.Close()
			args := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
			for _, endpoint := range []string{"/plugins/plan", "/plugins/install"} {
				resp := postJSON(t, srv.URL+endpoint, args)
				out := decodeInstallSource(t, resp)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK || out["ok"] != true || out["reloadError"] != nil {
					t.Fatalf("%s = %d: %v", endpoint, resp.StatusCode, out)
				}
				args["planId"] = out["planId"]
				if endpoint == "/plugins/plan" && len(getPlugins(t, srv.URL)) != 0 {
					t.Fatal("preview installed the package")
				}
			}
			plugins := getPlugins(t, srv.URL)
			if len(plugins) != 1 || !plugins[0].Enabled {
				t.Fatalf("installed packages=%+v", plugins)
			}
			if len(plugins[0].Agents) != 3 {
				t.Errorf("displayed agents=%+v, want three", plugins[0].Agents)
			}
			ctrl := s.ctl().(*control.Controller)
			for _, name := range []string{"café", "审查", "点検"} {
				invocation := "/unicode-agents:agent:" + name
				shown := false
				for _, agent := range plugins[0].Agents {
					shown = shown || agent.Name == name && agent.Invocation == invocation
				}
				if !shown {
					t.Errorf("inventory missing %q", invocation)
				}
				loaded := false
				for _, sk := range ctrl.Skills() {
					loaded = loaded || sk.SlashName() == strings.TrimPrefix(invocation, "/") && sk.RunAs == skill.RunSubagent && sk.Invocation == "manual"
				}
				input, found := ctrl.RunSkill(invocation)
				if !loaded || !found || !strings.Contains(input, "AGENT BODY") {
					t.Errorf("runtime profile %q: loaded=%t found=%t input=%q", invocation, loaded, found, input)
				}
			}
			if len(rec.Requests()) != 0 {
				t.Fatal("profile discovery started a provider request")
			}
		})
	}
}

func TestPluginCanonicalAgentCollisionMatchesInstalledRuntime(t *testing.T) {
	for _, format := range []struct{ kind, manifest, body, firstRoot, secondRoot string }{
		{"native", pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"collision-agents","contributes":{"agents":["second","first"]}}`, "first", "second"},
		{"Claude", pluginpkg.ClaudeManifest, `{"name":"collision-agents"}`, "agents/a-first", "agents"},
	} {
		for _, shape := range []struct{ label, first, second string }{
			{"composed first", "café.md", "cafe\u0301.md"},
			{"decomposed first", "cafe\u0301.md", "café.md"},
			{"directory first", "café/SKILL.md", "cafe\u0301.md"},
		} {
			t.Run(format.kind+"/"+shape.label, func(t *testing.T) {
				home, ctl, base := pluginHome(t)
				t.Setenv("REASONIX_STATE_HOME", home)
				t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
				t.Chdir(ctl.root)
				source := testenv.TempDir(t)
				writePluginFile(t, filepath.Join(source, format.manifest), format.body)
				writePluginFile(t, filepath.Join(source, format.firstRoot, shape.first), "---\ndescription: First fixture\n---\nFIRST")
				writePluginFile(t, filepath.Join(source, format.secondRoot, shape.second), "---\ndescription: Second fixture\n---\nSECOND")
				args := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
				resp := postJSON(t, base+"/plugins/plan", args)
				out := decodeInstallSource(t, resp)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK || out["ok"] != true {
					t.Fatalf("preview=%d: %v", resp.StatusCode, out)
				}
				if got := getPlugins(t, base); len(got) != 0 {
					t.Fatalf("preview installed a package: %+v", got)
				}
				diagnostic := fmt.Sprint(out)
				if !strings.Contains(diagnostic, "agent name") || !strings.Contains(diagnostic, "shadowed") {
					t.Fatalf("preview warning absent: %v", out)
				}
				args["planId"] = out["planId"]
				resp = postJSON(t, base+"/plugins/install", args)
				installed := decodeInstallSource(t, resp)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK || installed["status"] != "done" {
					t.Fatalf("install=%d: %v", resp.StatusCode, installed)
				}
				cfg, err := config.Load()
				if err != nil {
					t.Fatal(err)
				}
				store := skill.New(skill.Options{HomeDir: home, ReasonixHomeDir: home, CustomPaths: cfg.SkillCustomPaths(), PluginPaths: cfg.PluginPackageSkillOwners(), PluginAgentPaths: cfg.PluginPackageAgentOwners(), DisableBuiltins: true, Stderr: io.Discard})
				winner, ok := store.ReadSlash("collision-agents:agent:café")
				if !ok || winner.Body != "FIRST" || winner.RunAs != skill.RunSubagent || winner.Invocation != "manual" {
					t.Fatalf("actual runtime winner=%+v found=%t", winner, ok)
				}
				pkg, warnings, err := pluginpkg.ParseDir(pluginpkg.InstallRoot(home, "collision-agents"))
				if err != nil {
					t.Fatal(err)
				}
				inv := pkg.Inventory().Agents
				if len(inv) != 1 || inv[0].Path != winner.Path || inv[0].Name != winner.Name {
					t.Fatalf("inventory=%+v actual winner=%+v", inv, winner)
				}
				plugins := getPlugins(t, base)
				if len(plugins) != 1 || len(plugins[0].Agents) != 1 || plugins[0].Agents[0].Name != winner.Name || plugins[0].Agents[0].Description != winner.Description || plugins[0].Agents[0].Invocation != "/"+winner.SlashName() {
					t.Fatalf("HTTP inventory=%+v actual winner=%+v", plugins, winner)
				}
				if len(warnings) != 1 || !strings.Contains(warnings[0], pluginpkg.RelativeRoot(pkg.Root, winner.Path)) || !strings.Contains(warnings[0], "; "+format.secondRoot+"/") {
					t.Fatalf("winner/loser warnings=%v", warnings)
				}
				t.Logf("actual installed runtime selected %s: %s", pluginpkg.RelativeRoot(pkg.Root, winner.Path), winner.Body)
			})
		}
	}
}
