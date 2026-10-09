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
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

var switchSkillProviderSeq atomic.Uint64

func TestModelSwitchPreservesLiveSkillDiscovery(t *testing.T) {
	home, root, source := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
	t.Chdir(root)
	kind := fmt.Sprintf("switch-skill-store-%d", switchSkillProviderSeq.Add(1))
	rec := testutil.NewMock(kind, testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"})
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writePluginFile(t, filepath.Join(root, "reasonix.toml"), `
default_model = "first"
[agent]
system_prompt = "BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[skills]
paths = ["custom-skills"]
excluded_paths = [".agents/skills"]
disabled_skills = ["disabled-review"]
max_depth = 1
[[providers]]
name = "first"
kind = "`+kind+`"
model = "a"
[[providers]]
name = "second"
kind = "`+kind+`"
model = "b"
`)
	writeSkill := func(path, name, body string) {
		t.Helper()
		writePluginFile(t, filepath.Join(root, path, "SKILL.md"), "---\nname: "+name+"\ndescription: Fixture "+name+"\n---\n"+body)
	}
	writeSkill("custom-skills/custom-review", "custom-review", "CUSTOM BODY")
	writeSkill(".reasonix/skills/project-review", "project-review", "PROJECT BODY")
	writeSkill(".reasonix/skills/disabled-review", "disabled-review", "DISABLED BODY")
	writeSkill(".reasonix/skills/group/deep-review", "deep-review", "DEEP BODY")
	writeSkill(".agents/skills/excluded-review", "excluded-review", "EXCLUDED BODY")
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"switch-kit","contributes":{"skills":["skills"],"agents":["agents"]}}`)
	writePluginFile(t, filepath.Join(source, "skills/greet/SKILL.md"), "---\nname: greet\ndescription: Plugin greeting\n---\nPLUGIN BODY")
	writePluginFile(t, filepath.Join(source, "agents/review.md"), "---\nname: package-review\ndescription: Plugin review\n---\nAGENT BODY")
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
	request := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
	for _, endpoint := range []string{"/plugins/plan", "/plugins/install"} {
		resp := postJSON(t, srv.URL+endpoint, request)
		out := decodeInstallSource(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || out["ok"] != true || out["reloadError"] != nil {
			t.Fatalf("%s = %d: %v", endpoint, resp.StatusCode, out)
		}
		if applied := out["applied"] == true; applied != (endpoint == "/plugins/install") {
			t.Fatalf("%s applied=%t", endpoint, applied)
		}
		request["planId"] = out["planId"]
	}
	if s.lastBuild == nil || s.lastBuild.Assembly == nil || s.lastBuild.Plan == nil || !s.lastBuild.Plan.IsNoOp() {
		t.Fatal("installed runtime did not produce an assembly eligible for switch reuse")
	}
	assertDiscovery := func(phase string) {
		t.Helper()
		ctrl := s.ctl().(*control.Controller)
		enabled := map[string]bool{}
		for _, sk := range ctrl.Skills() {
			enabled[sk.Name] = true
		}
		for name, want := range map[string]bool{"greet": true, "package-review": true, "custom-review": true, "project-review": true, "disabled-review": false, "deep-review": false, "excluded-review": false} {
			if enabled[name] != want {
				t.Errorf("%s: skill %s visible=%t, want %t", phase, name, enabled[name], want)
			}
		}
		all := map[string]bool{}
		for _, sk := range ctrl.AllSkills() {
			all[sk.Name] = true
		}
		if !all["disabled-review"] || !all["greet"] || !all["custom-review"] {
			t.Errorf("%s: management discovery lost configured skills: %v", phase, all)
		}
		if _, found := ctrl.RunSkill("/disabled-review"); found {
			t.Errorf("%s: config-disabled skill can be slash-invoked", phase)
		}
		if _, found := ctrl.RunSkill("/switch-kit:agent:package-review"); !found {
			t.Errorf("%s: qualified plugin agent missing", phase)
		}
	}
	assertDiscovery("installed")
	before := s.ctl().(*control.Controller)
	if err := before.Run(t.Context(), "before switch"); err != nil {
		t.Fatal(err)
	}
	resp := postJSON(t, srv.URL+"/model", map[string]any{"ref": "second/b"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || s.ctl().ModelRef() != "second/b" {
		t.Fatalf("model switch = %d, model=%s", resp.StatusCode, s.ctl().ModelRef())
	}
	assertDiscovery("switched")
	ctrl := s.ctl().(*control.Controller)
	if err := ctrl.Run(t.Context(), "after switch"); err != nil {
		t.Fatal(err)
	}
	reqs := rec.Requests()
	if len(reqs) != 2 {
		t.Fatalf("provider received %d requests, want before/after switch", len(reqs))
	}
	var prefixes []string
	for i, req := range reqs {
		for _, msg := range req.Messages {
			if msg.Role == provider.RoleSystem {
				prefixes = append(prefixes, msg.Content)
			}
			if msg.Role != provider.RoleUser || !strings.Contains(msg.Content, []string{"before switch", "after switch"}[i]) {
				continue
			}
			for _, name := range []string{"greet", "custom-review", "project-review"} {
				if !strings.Contains(msg.Content, "\n- "+name+" ") {
					t.Errorf("request %d: skill %s missing from current provider catalog", i, name)
				}
			}
			for _, name := range []string{"disabled-review", "deep-review", "excluded-review"} {
				if strings.Contains(msg.Content, "\n- "+name+" ") {
					t.Errorf("request %d: hidden skill %s reached provider catalog", i, name)
				}
			}
		}
	}
	if len(prefixes) != 2 || prefixes[0] != prefixes[1] {
		t.Fatal("model switch changed the shared system prefix")
	}
	for _, prefix := range prefixes {
		if strings.Contains(prefix, "PLUGIN BODY") || strings.Contains(prefix, "CUSTOM BODY") {
			t.Fatal("skill bodies entered the system prefix")
		}
	}
	input, found := ctrl.RunSkill("/switch-kit:greet hello")
	if !found || !strings.Contains(input, "PLUGIN BODY") {
		t.Errorf("switched plugin invocation = %q, found=%t", input, found)
	} else if err := ctrl.Run(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	writeSkill("custom-skills/custom-review", "custom-review", "UPDATED CUSTOM BODY")
	input, found = ctrl.RunSkill("/custom-review")
	if !found || !strings.Contains(input, "UPDATED CUSTOM BODY") {
		t.Errorf("switched custom invocation = %q, found=%t", input, found)
	} else if err := ctrl.Run(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SetSkillEnabled("custom-review", config.ActivationProject, false); err != nil {
		t.Error(err)
	}
	if _, found := ctrl.RunSkill("/custom-review"); found {
		t.Error("live activation switch did not hide the custom skill")
	}
	if err := ctrl.SetSkillEnabled("custom-review", config.ActivationProject, true); err != nil {
		t.Error(err)
	}
	if current, found := ctrl.RunSkill("/custom-review"); !found || !strings.Contains(current, "UPDATED CUSTOM BODY") {
		t.Errorf("re-enabled custom invocation = %q, found=%t", current, found)
	}
	reqs = rec.Requests()
	if len(reqs) != 4 {
		t.Fatalf("provider received %d requests, want two catalogs and two invocations", len(reqs))
	}
	for i, body := range []string{"PLUGIN BODY", "UPDATED CUSTOM BODY"} {
		found := false
		for _, msg := range reqs[i+2].Messages {
			found = found || msg.Role == provider.RoleUser && strings.Contains(msg.Content, body)
		}
		if !found {
			t.Errorf("invoked skill body %q did not reach provider", body)
		}
	}
}
