package serve

import (
	"fmt"
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
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

var unicodeSkillProviderSeq atomic.Uint64

func TestInstalledUnicodeSkillsMatchDisplayedInvocations(t *testing.T) {
	for _, format := range []struct {
		kind, manifest, body string
		flatOnly, collision  bool
	}{
		{"native", pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"unicode-kit","contributes":{"skills":["skills"]}}`, false, false},
		{"Claude", pluginpkg.ClaudeManifest, `{"name":"unicode-kit","skills":["skills"]}`, false, false},
		{"Codex", pluginpkg.CodexManifest, `{"name":"unicode-kit","skills":["skills"]}`, false, false},
		{"Claude flat only", pluginpkg.ClaudeManifest, `{"name":"unicode-kit","skills":["skills"]}`, true, false},
		{"Codex flat only", pluginpkg.CodexManifest, `{"name":"unicode-kit","skills":["skills"]}`, true, false},
		{"native collision", pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"unicode-kit","contributes":{"skills":["second","first"]}}`, false, true},
		{"Claude collision", pluginpkg.ClaudeManifest, `{"name":"unicode-kit","skills":["second","first"]}`, false, true},
		{"Codex collision", pluginpkg.CodexManifest, `{"name":"unicode-kit","skills":["second","first"]}`, false, true},
	} {
		t.Run(format.kind, func(t *testing.T) {
			home, root, source := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Setenv("REASONIX_STATE_HOME", home)
			t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
			t.Chdir(root)
			kind := fmt.Sprintf("unicode-plugin-skills-%d", unicodeSkillProviderSeq.Add(1))
			rec := testutil.NewMock(kind, testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"})
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writePluginFile(t, filepath.Join(root, "reasonix.toml"), `
default_model = "fixture"
[agent]
system_prompt = "UNICODE BASE"
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
			files := []struct{ path, frontName, body string }{
				{"skills/审查/SKILL.md", "审查", "CHINESE BODY"},
				{"skills/点検.md", "点検", "JAPANESE BODY"},
				{"skills/cafe\u0301/SKILL.md", "", "NORMALIZED BODY"},
			}
			if format.collision {
				files = []struct{ path, frontName, body string }{
					{"first/café/SKILL.md", "", "FIRST BODY"},
					{"second/cafe\u0301/SKILL.md", "", "SECOND BODY"},
				}
			}
			for _, file := range files {
				if format.flatOnly && file.path != "skills/点検.md" {
					continue
				}
				writePluginFile(t, filepath.Join(source, file.path), "---\nname: "+file.frontName+"\ndescription: Unicode skill fixture\n---\n"+file.body)
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
				if format.collision {
					warning := fmt.Sprint(out["warnings"])
					if !strings.Contains(warning, `skill name "café"`) || !strings.Contains(warning, "first/") || !strings.Contains(warning, "second/") {
						t.Fatalf("%s collision warning = %s", endpoint, warning)
					}
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
			cases := []struct{ name, body string }{{"café", "NORMALIZED BODY"}, {"审查", "CHINESE BODY"}, {"点検", "JAPANESE BODY"}}
			if format.flatOnly {
				cases = cases[2:]
			}
			if format.collision {
				cases = []struct{ name, body string }{{"café", "FIRST BODY"}}
			}
			if len(plugins[0].Skills) != len(cases) {
				t.Errorf("displayed skills=%+v, want %d", plugins[0].Skills, len(cases))
			}
			ctrl := s.ctl().(*control.Controller)
			for i, tc := range cases {
				invocation := "/unicode-kit:" + tc.name
				shown := false
				for _, sk := range plugins[0].Skills {
					shown = shown || sk.Name == tc.name && sk.Invocation == invocation
				}
				if !shown {
					t.Errorf("inventory missing %q", invocation)
				}
				input, found := ctrl.RunSkill(invocation)
				if !found || !strings.Contains(input, tc.body) || format.collision && strings.Contains(input, "SECOND BODY") {
					t.Fatalf("runtime invocation %q = %q, found=%t", invocation, input, found)
				}
				if err := ctrl.Run(t.Context(), input); err != nil {
					t.Fatal(err)
				}
				reqs := rec.Requests()
				if len(reqs) != i+1 {
					t.Fatalf("provider requests=%d, want %d", len(reqs), i+1)
				}
				bodyReached := false
				for _, msg := range reqs[i].Messages {
					bodyReached = bodyReached || msg.Role == provider.RoleUser && strings.Contains(msg.Content, tc.body)
					if msg.Role == provider.RoleSystem && strings.Contains(msg.Content, tc.body) {
						t.Error("skill body entered the system prefix")
					}
				}
				if !bodyReached {
					t.Errorf("%q did not reach provider", tc.body)
				}
			}
		})
	}
}
