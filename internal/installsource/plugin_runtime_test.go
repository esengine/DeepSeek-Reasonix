package installsource

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// writeRuntimePlugin writes a Manifest v2 plugin package with a runtime
// declaration plus prompt and theme contributions.
func writeRuntimePlugin(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "reasonix-plugin.json"), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "rtsync",
  "version": "1.0.0",
  "contributes": {
    "prompts": ["prompts"],
    "themes": ["themes/*.reasonix-theme"]
  },
  "runtime": {
    "command": "${REASONIX_PLUGIN_ROOT}/bin/rtsync",
    "args": ["--serve"],
    "required": true,
    "intercepts": ["input.receive", "tool.before"],
    "replaces": ["system_prompt"],
    "capabilities": ["interceptors", "ui"]
  }
}`)
	writeFile(t, filepath.Join(root, "prompts", "plan.md"), "---\ndescription: plan\n---\nPlan $ARGUMENTS")
	writeFile(t, filepath.Join(root, "themes", "neon.reasonix-theme"), "theme bytes")
	writeFile(t, filepath.Join(root, "bin", "rtsync"), "#!/bin/sh\n")
}

func TestPluginRuntimePlanCarriesFullTrust(t *testing.T) {
	src := t.TempDir()
	writeRuntimePlugin(t, src)

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})

	resp := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "plugin",
		"apply":  false,
	})
	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	act := resp.Actions[0]
	if act.RiskLevel != RiskHigh {
		t.Fatalf("RiskLevel = %q, want high for a runtime package", act.RiskLevel)
	}
	fullTrust := false
	for _, reason := range act.RiskReasons {
		if strings.HasPrefix(reason, "FULL TRUST:") {
			fullTrust = true
			if !strings.Contains(reason, "${REASONIX_PLUGIN_ROOT}/bin/rtsync --serve") {
				t.Fatalf("FULL TRUST reason should describe the runtime command line: %q", reason)
			}
		}
	}
	if !fullTrust {
		t.Fatalf("RiskReasons = %v, want a FULL TRUST: entry", act.RiskReasons)
	}
	rt := act.Runtime
	if rt == nil {
		t.Fatal("action.Runtime is nil, want the runtime plan info")
	}
	if rt.Command != "${REASONIX_PLUGIN_ROOT}/bin/rtsync" || !rt.FullTrust {
		t.Fatalf("Runtime = %+v", rt)
	}
	if len(rt.Args) != 1 || rt.Args[0] != "--serve" {
		t.Fatalf("Runtime.Args = %v", rt.Args)
	}
	if len(rt.Intercepts) != 2 || len(rt.Replaces) != 1 || len(rt.Capabilities) != 2 {
		t.Fatalf("Runtime = %+v", rt)
	}
	if act.PromptCount != 1 || act.ThemeCount != 1 {
		t.Fatalf("PromptCount/ThemeCount = %d/%d, want 1/1", act.PromptCount, act.ThemeCount)
	}

	// The plan JSON must carry the runtime block so frontends can render it.
	raw, err := json.Marshal(act)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatal(err)
	}
	rtJSON, ok := encoded["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("plan JSON missing runtime block: %s", raw)
	}
	if rtJSON["fullTrust"] != true || rtJSON["command"] != "${REASONIX_PLUGIN_ROOT}/bin/rtsync" {
		t.Fatalf("runtime JSON = %v", rtJSON)
	}
}

func TestPluginLegacyPlanOmitsRuntimeFields(t *testing.T) {
	src := t.TempDir()
	// v2 without runtime: skills-only package has no Runtime plan fields.
	writeFile(t, filepath.Join(src, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"legacy","skills":["skills"]}`)
	writeFile(t, filepath.Join(src, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nS")

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})

	resp := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "plugin",
		"apply":  false,
	})
	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	act := resp.Actions[0]
	if act.Runtime != nil {
		t.Fatalf("skills-only package gained a runtime: %+v", act.Runtime)
	}
	if act.RiskLevel != RiskMedium {
		t.Fatalf("RiskLevel = %q, want medium for a plain legacy package", act.RiskLevel)
	}
	raw, err := json.Marshal(act)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"runtime"`, `"promptCount"`, `"themeCount"`} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("legacy plan JSON should omit %s: %s", key, raw)
		}
	}
}
