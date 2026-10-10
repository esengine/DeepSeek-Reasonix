package installsource

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestDuplicatePluginHintOffersApprovedReplacement(t *testing.T) {
	home := testenv.TempDir(t)
	source := testenv.TempDir(t)
	writeFile(t, filepath.Join(source, pluginpkg.NativeManifest), `{
  "apiVersion":"reasonix.io/plugin/v2","name":"recovery-kit","version":"1.0.0",
  "contributes":{"skills":["skills"]}
}`)
	const path = "skills/note/SKILL.md"
	const first = "---\nname: note\ndescription: Read a local note\n---\nOriginal note.\n"
	const updated = "---\nname: note\ndescription: Read a local note\n---\nUpdated note.\n"
	writeFile(t, filepath.Join(source, path), first)
	installer := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home, RequireApprovedPlan: true})
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	apply := func() response {
		t.Helper()
		args["apply"] = false
		delete(args, "planId")
		plan := execInstall(t, installer, args)
		if !plan.OK || plan.Status != "planned" || plan.Applied || plan.PlanID == "" {
			t.Fatalf("preview = %+v", plan)
		}
		args["apply"], args["planId"] = true, plan.PlanID
		return execInstall(t, installer, args)
	}
	if installed := apply(); !installed.OK || installed.Status != "done" {
		t.Fatalf("first install = %+v", installed)
	}
	target := filepath.Join(pluginpkg.InstallRoot(filepath.Join(home, ".reasonix"), "recovery-kit"), path)
	check := func(want string) {
		t.Helper()
		body, err := os.ReadFile(target)
		if err != nil || string(body) != want {
			t.Fatalf("installed note = %q, %v; want %q", body, err, want)
		}
	}
	writeFile(t, filepath.Join(source, path), updated)
	duplicate := apply()
	if duplicate.OK || duplicate.Status != "failed" || len(duplicate.Actions) != 1 || duplicate.Actions[0].Status != "failed" {
		t.Fatalf("duplicate = %+v", duplicate)
	}
	const want = "Choose another name, remove the existing entry, or retry MCP or plugin installs with replace=true."
	if got := duplicate.Actions[0].Next; got != want {
		t.Errorf("duplicate plugin next = %q, want %q", got, want)
	}
	check(first)
	args["replace"], args["apply"] = true, true
	delete(args, "planId")
	unticketed := execInstall(t, installer, args)
	if !unticketed.OK || unticketed.Status != "planned" || unticketed.Applied {
		t.Fatalf("unticketed replacement = %+v", unticketed)
	}
	check(first)
	if replaced := apply(); !replaced.OK || replaced.Status != "done" {
		t.Fatalf("approved replacement = %+v", replaced)
	}
	check(updated)
}
