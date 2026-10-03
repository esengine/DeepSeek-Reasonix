package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestPluginPreviewRejectsCanonicalAgentCollisions(t *testing.T) {
	for _, format := range []struct{ kind, manifest, body string }{
		{"native", pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"collision-agents","contributes":{"agents":["agents"]}}`},
		{"Claude", pluginpkg.ClaudeManifest, `{"name":"collision-agents"}`},
	} {
		t.Run(format.kind, func(t *testing.T) {
			home, ctl, base := pluginHome(t)
			t.Setenv("REASONIX_STATE_HOME", home)
			t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
			t.Chdir(ctl.root)
			source := testenv.TempDir(t)
			writePluginFile(t, filepath.Join(source, format.manifest), format.body)
			writePluginFile(t, filepath.Join(source, "agents", "第一.md"), "---\nname: café\ndescription: First fixture\n---\nFIRST")
			writePluginFile(t, filepath.Join(source, "agents", "第二.md"), "---\nname: cafe\u0301\ndescription: Second fixture\n---\nSECOND")
			resp := postJSON(t, base+"/plugins/plan", map[string]any{"source": source, "kind": "plugin", "mode": "copy"})
			out := decodeInstallSource(t, resp)
			resp.Body.Close()
			if out["ok"] == true {
				t.Fatalf("ambiguous agent preview accepted: status=%d body=%v", resp.StatusCode, out)
			}
			if diagnostic := fmt.Sprint(out); !strings.Contains(diagnostic, `agent name "café"`) || !strings.Contains(diagnostic, "第一.md") || !strings.Contains(diagnostic, "第二.md") {
				t.Fatalf("agent collision preview lacks both paths: status=%d body=%v", resp.StatusCode, out)
			}
			if got := getPlugins(t, base); len(got) != 0 {
				t.Fatalf("refused preview installed a package: %+v", got)
			}
			if _, err := os.Stat(pluginpkg.InstallRoot(home, "collision-agents")); !os.IsNotExist(err) {
				t.Fatalf("refused preview wrote an install root: %v", err)
			}
		})
	}
}
