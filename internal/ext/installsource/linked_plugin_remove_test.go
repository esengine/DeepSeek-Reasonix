package installsource

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestLinkedPluginRemoveAllowsFreshInstall(t *testing.T) {
	for _, removeSource := range []bool{false, true} {
		name := "source-present"
		if removeSource {
			name = "source-removed"
		}
		t.Run(name, func(t *testing.T) {
			project, err := filepath.EvalSymlinks(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			home, err := filepath.EvalSymlinks(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(project, "source")
			const manifest = `{"apiVersion":"reasonix.io/plugin/v2","name":"linked-example","version":"1.0.0","contributes":{"skills":["skills"]}}`
			const body = "---\nname: orientation\ndescription: Inspect selected inputs\n---\nRead selected files.\n"
			profile := filepath.Join(source, "skills", "orientation", "SKILL.md")
			writeSource := func() {
				writeFile(t, filepath.Join(source, "reasonix-plugin.json"), manifest)
				writeFile(t, profile, body)
			}
			writeSource()
			probe := filepath.Join(project, "symlink-probe")
			if err := os.Symlink(source, probe); err != nil {
				t.Skipf("directory symlink unavailable: %v", err)
			}
			if err := os.Remove(probe); err != nil {
				t.Fatal(err)
			}
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
			link := func() {
				t.Helper()
				args := map[string]any{"source": source, "kind": "plugin", "mode": "link", "scope": "global"}
				plan := execInstall(t, tl, args)
				if !plan.OK || plan.Applied || len(plan.Actions) != 1 || plan.PlanID == "" {
					t.Fatalf("preview = %+v", plan)
				}
				args["apply"], args["planId"] = true, plan.PlanID
				if installed := execInstall(t, tl, args); !installed.OK || installed.Status != "done" {
					t.Fatalf("approved link = %+v", installed)
				}
			}
			link()
			target := pluginpkg.InstallRoot(tl.reasonixHome, "linked-example")
			if got, err := os.Readlink(target); err != nil || got != source {
				t.Fatalf("managed link = %q, err=%v", got, err)
			}
			if removeSource {
				if err := os.RemoveAll(source); err != nil {
					t.Fatal(err)
				}
			}
			removed := execInstall(t, tl, map[string]any{"op": "uninstall", "name": "linked-example", "scope": "global"})
			if !removed.OK || removed.Status != "done" {
				t.Fatalf("remove = %+v", removed)
			}
			if _, found, err := pluginpkg.FindInstalled(tl.reasonixHome, "linked-example"); err != nil || found {
				t.Fatalf("registration remains: found=%t, err=%v", found, err)
			}
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Errorf("managed link remains after removal: %v", err)
			}
			if removeSource {
				if _, err := os.Stat(source); !os.IsNotExist(err) {
					t.Fatalf("remove recreated the external source: %v", err)
				}
				writeSource()
			} else if got, err := os.ReadFile(profile); err != nil || string(got) != body {
				t.Fatalf("external source changed: %q, err=%v", got, err)
			}
			link()
			if got, err := os.ReadFile(filepath.Join(target, "skills", "orientation", "SKILL.md")); err != nil || string(got) != body {
				t.Fatalf("fresh installation = %q, err=%v", got, err)
			}
		})
	}
}

func TestLinkedPluginRemovePreservesReplacedTarget(t *testing.T) {
	for _, replacement := range []string{"file", "link", "missing"} {
		t.Run(replacement, func(t *testing.T) {
			project, err := filepath.EvalSymlinks(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			home, err := filepath.EvalSymlinks(testenv.TempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(project, "source")
			writeFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"linked-example","version":"1.0.0","contributes":{}}`)
			probe := filepath.Join(project, "symlink-probe")
			if err := os.Symlink(source, probe); err != nil {
				t.Skipf("directory symlink unavailable: %v", err)
			}
			if err := os.Remove(probe); err != nil {
				t.Fatal(err)
			}
			tl := NewTool(Options{ProjectRoot: project, HomeDir: home, RequireApprovedPlan: true})
			args := map[string]any{"source": source, "kind": "plugin", "mode": "link", "scope": "global"}
			plan := execInstall(t, tl, args)
			args["apply"], args["planId"] = true, plan.PlanID
			installed := execInstall(t, tl, args)
			if !installed.OK {
				t.Fatalf("link = %+v", installed)
			}
			target := pluginpkg.InstallRoot(tl.reasonixHome, "linked-example")
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(project, "manual-files")
			const manual = "Keep these manually added files."
			switch replacement {
			case "file":
				writeFile(t, target, manual)
			case "link":
				writeFile(t, filepath.Join(other, "notes.txt"), manual)
				if err := os.Symlink(other, target); err != nil {
					t.Fatal(err)
				}
			}
			removed := execInstall(t, tl, map[string]any{"op": "uninstall", "name": "linked-example", "scope": "global"})
			if !removed.OK || removed.Status != "done" {
				t.Fatalf("remove = %+v", removed)
			}
			if _, found, err := pluginpkg.FindInstalled(tl.reasonixHome, "linked-example"); err != nil || found {
				t.Fatalf("registration remains: found=%t, err=%v", found, err)
			}
			switch replacement {
			case "file":
				if got, err := os.ReadFile(target); err != nil || string(got) != manual {
					t.Fatalf("manual file changed: %q, err=%v", got, err)
				}
			case "link":
				if got, err := os.Readlink(target); err != nil || got != other {
					t.Fatalf("replacement link changed: %q, err=%v", got, err)
				}
				if got, err := os.ReadFile(filepath.Join(other, "notes.txt")); err != nil || string(got) != manual {
					t.Fatalf("replacement source changed: %q, err=%v", got, err)
				}
			case "missing":
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatalf("missing target recreated: %v", err)
				}
			}
			if _, err := os.Stat(filepath.Join(source, "reasonix-plugin.json")); err != nil {
				t.Fatalf("external source removed: %v", err)
			}
		})
	}
}
