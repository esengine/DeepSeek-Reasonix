package installsource

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/ext/pluginpkg"
	"testing"
)

func TestRevisionCrashHelper(t *testing.T) {
	root := os.Getenv("REASONIX_REVISION_CRASH_HOME")
	if root == "" {
		t.Skip("subprocess helper")
	}
	tool := NewTool(Options{HomeDir: root, ProjectRoot: root})
	source := os.Getenv("REASONIX_REVISION_CRASH_SOURCE")
	phase := os.Getenv("REASONIX_REVISION_CRASH_PHASE")
	if phase == "before" {
		fileutil.CrashPoint = func(op, path string) {
			if op == "atomic-write" && path == pluginpkg.StatePath(tool.reasonixHome) {
				os.Exit(73)
			}
		}
	} else {
		fileutil.SetSyncParentDirForTest(func(string) error { os.Exit(73); return nil })
	}
	act := plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{Replace: true}, &act); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash point not reached")
}

func TestRevisionCrashRestartUsesOnlyCommittedRoot(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		t.Run(phase, func(t *testing.T) {
			tool, source := revisionPlugin(t)
			act := plannedPluginCopy(t, tool, source)
			if err := tool.applyInstallPluginPackage(t.Context(), request{}, &act); err != nil {
				t.Fatal(err)
			}
			old := act.Target
			writeFile(t, filepath.Join(source, "skills", "neutral", "SKILL.md"), "---\nname: neutral\ndescription: Neutral fixture\n---\nNEW")
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestRevisionCrashHelper$")
			cmd.Env = append(os.Environ(), "REASONIX_REVISION_CRASH_HOME="+tool.home, "REASONIX_REVISION_CRASH_SOURCE="+source, "REASONIX_REVISION_CRASH_PHASE="+phase)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("crash exit: %v, %s", err, out)
			}
			writeFile(t, filepath.Join(pluginpkg.PluginsDir(tool.reasonixHome), ".incomplete-orphan", "skills", "neutral", "SKILL.md"), "INCOMPLETE")
			installed, warnings := pluginpkg.LoadInstalled(tool.reasonixHome)
			if len(installed) != 1 || len(warnings) != 0 {
				t.Fatalf("restart trusted orphan or lost pointer: %+v, %v", installed, warnings)
			}
			root := installed[0].Package.Root
			body, err := os.ReadFile(filepath.Join(root, "skills", "neutral", "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			want := "---\nname: neutral\ndescription: Neutral fixture\n---\nAPPROVED"
			if phase == "after" {
				want = "---\nname: neutral\ndescription: Neutral fixture\n---\nNEW"
				if root == old {
					t.Fatal("new pointer not loaded")
				}
			} else if root != old {
				t.Fatal("prepublication exit changed pointer")
			}
			if string(body) != want {
				t.Fatalf("restart bytes %q, expected %q", body, want)
			}
		})
	}
}
